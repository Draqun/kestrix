package web

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"kestrix/internal/dag"
	"kestrix/internal/dispatch"
	"kestrix/internal/domain"
	"kestrix/internal/model"
	"kestrix/internal/parser"
)

func (s *Server) logWebhook(event domain.WebhookEvent) {
	if s.webhookService == nil {
		return
	}
	s.webhookService.Record(event)
}

// writeWebhookErrorResponse answers the two failure paths that report the event
// they just recorded. logEntry is a domain.WebhookEvent because it is the
// history row; encoding that struct directly would put the persistence type's
// field names on the wire (received_at, triggered_tasks, and an always-zero id)
// and silently change the response contract of POST /webhook, so the response
// document is built explicitly.
func writeWebhookErrorResponse(w http.ResponseWriter, entry domain.WebhookEvent) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"timestamp": entry.ReceivedAt.Format(time.RFC3339),
		"repo":      entry.Repo,
		"branch":    entry.Branch,
		"revision":  entry.Revision,
		"commits":   entry.Commits,
		"tasks":     entry.TriggeredTasks,
		"status":    entry.Status,
		"error":     entry.Error,
	})
}

// handleWebhook godoc
// @Summary Receive GitLab webhook
// @Tags webhooks
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /webhook [post]
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Signature.Enabled && s.cfg.Signature.Secret != "" {
		token := r.Header.Get("X-Gitlab-Token")
		if token != s.cfg.Signature.Secret {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	defer r.Body.Close()

	event, err := ParseGitLabPushEvent(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	logEntry := domain.WebhookEvent{
		ReceivedAt:     time.Now().UTC(),
		Repo:           event.Repo,
		Branch:         event.Branch,
		Revision:       event.CheckoutSHA,
		Commits:        event.CommitCount,
		TriggeredTasks: []string{},
		Status:         domain.WebhookStatusError,
	}

	// ─── Look up project by GitLab project ID ────────────────
	var proj model.Project
	var projCfg *domain.ProjectConfig
	cleanup := func() {}
	if event.GitLabProjectID > 0 && s.projectStore != nil {
		projCfg, err = s.projectStore.FindByGitLabID(event.GitLabProjectID)
		if err != nil {
			log.Printf("webhook: store lookup error: %v", err)
			logEntry.Status = domain.WebhookStatusError
			logEntry.Error = err.Error()
			s.logWebhook(logEntry)
			writeWebhookErrorResponse(w, logEntry)
			return
		}
		if projCfg == nil {
			log.Printf("webhook: unknown project gitlab_id=%d", event.GitLabProjectID)
			logEntry.Status = domain.WebhookStatusIgnored
			logEntry.Error = "unknown project"
			s.logWebhook(logEntry)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "ignored", "reason": "unknown project",
			})
			return
		}
		logEntry.ProjectName = projCfg.Name
		if event.CheckoutSHA == "" {
			logEntry.Status = domain.WebhookStatusError
			logEntry.Error = "push event has no immutable checkout SHA"
			s.logWebhook(logEntry)
			writeError(w, http.StatusBadRequest, "push event has no immutable checkout SHA")
			return
		}
		loaded, session, loadErr := s.loadProjectPipeline(r.Context(), projCfg, event.CheckoutSHA, "webhook")
		if loadErr != nil {
			logEntry.Status = domain.WebhookStatusError
			logEntry.Error = loadErr.Error()
			s.logWebhook(logEntry)
			writeError(w, http.StatusUnprocessableEntity, loadErr.Error())
			return
		}
		proj = loaded
		cleanup = session.Cleanup
	} else {
		proj, err = parser.ParseDir(s.cfg.PipelineDir)
		if err != nil {
			log.Printf("webhook: parse error: %v", err)
			logEntry.Status = domain.WebhookStatusError
			logEntry.Error = err.Error()
			s.logWebhook(logEntry)
			writeWebhookErrorResponse(w, logEntry)
			return
		}
		// This mode has no registered project, but the parsed pipeline names the
		// project the event is about. Recording it keeps the history Project
		// column and the project filter usable here too, instead of leaving
		// every row in a single-pipeline deployment blank.
		logEntry.ProjectName = proj.Name
	}
	defer cleanup()

	classifier := DefaultClassifier()
	changedRepos := classifier.MatchRepos(event.FilesChanged, proj.Repos)

	changedSet := make(map[string]bool)
	for _, c := range changedRepos {
		changedSet[c] = true
	}

	e := dag.New(proj)
	nodes, _ := e.Resolve(changedSet)

	var triggeredTasks []string
	for _, n := range nodes {
		if n.Triggered {
			triggeredTasks = append(triggeredTasks, n.Task.Name)
		}
	}

	if triggeredTasks == nil {
		triggeredTasks = []string{}
	}
	logEntry.TriggeredTasks = triggeredTasks
	logEntry.Status = domain.WebhookStatusTriggered
	if len(triggeredTasks) == 0 {
		logEntry.Status = domain.WebhookStatusSkipped
	}

	log.Printf("webhook: repo=%s branch=%s commits=%d files=%d tasks=%v",
		event.Repo, event.Branch, event.CommitCount, len(changedRepos), triggeredTasks)

	// ─── Dispatch to GitLab (if configured) ──────────────────
	var dispatchResult map[string]interface{}
	dispatchID := s.cfg.Dispatch.ProjectID
	if event.GitLabProjectID > 0 && s.projectStore != nil {
		if projCfg != nil && projCfg.GitLabProjectID > 0 {
			dispatchID = fmt.Sprintf("%d", projCfg.GitLabProjectID)
		}
	}
	if s.cfg.Dispatch.Enabled && dispatchID != "" && len(triggeredTasks) > 0 {
		var projectID int
		fmt.Sscanf(dispatchID, "%d", &projectID)
		if projectID > 0 {
			client := &dispatch.Client{
				BaseURL: s.cfg.Dispatch.GitLabURL,
				Token:   s.cfg.Dispatch.GitLabToken,
			}
			vars := map[string]string{
				"KESTRIX_TRIGGERED_TASKS": strings.Join(triggeredTasks, ","),
				"KESTRIX_CHANGED_REPOS":   strings.Join(changedRepos, ","),
				"KESTRIX_DISPATCH_TIME":   time.Now().UTC().Format(time.RFC3339),
			}
			ref := s.cfg.Branch
			if ref == "" {
				ref = event.Branch
			}

			resp, err := client.Dispatch(dispatch.DispatchRequest{
				ProjectID: projectID,
				Ref:       ref,
				Variables: vars,
			})
			if err != nil {
				log.Printf("webhook: dispatch error: %v", err)
				logEntry.Status = domain.WebhookStatusError
				logEntry.Error = err.Error()
				dispatchResult = map[string]interface{}{"dispatched": false, "error": err.Error()}
			} else {
				log.Printf("webhook: dispatched pipeline %d (%s)", resp.PipelineID, resp.WebURL)
				logEntry.DispatchedID = resp.PipelineID
				logEntry.DispatchedURL = resp.WebURL
				dispatchResult = map[string]interface{}{
					"dispatched":   true,
					"pipeline_id":  resp.PipelineID,
					"pipeline_url": resp.WebURL,
				}
			}
		}
	}

	s.logWebhook(logEntry)

	response := map[string]interface{}{
		"status":  "ok",
		"project": proj.Name,
		"changed": changedRepos,
		"tasks":   triggeredTasks,
		"count":   len(triggeredTasks),
	}
	if dispatchResult != nil {
		response["dispatched"] = dispatchResult["dispatched"]
		response["pipeline_url"] = dispatchResult["pipeline_url"]
		response["pipeline_id"] = dispatchResult["pipeline_id"]
	} else {
		response["dispatched"] = false
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
