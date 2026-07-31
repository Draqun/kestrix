package web

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"kestrix/internal/dag"
	"kestrix/internal/dispatch"
	"kestrix/internal/parser"
	"kestrix/internal/service"
)

func (s *Server) logWebhook(entry map[string]interface{}) {
	if s.webhookService == nil {
		s.webhookService = service.NewWebhookService(s)
	}
	_ = s.webhookService.Process(entry)
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
	logEntry := map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"repo":      event.Repo,
		"branch":    event.Branch,
		"commits":   event.CommitCount,
		"tasks":     []string{},
		"status":    "received",
	}

	// ─── Look up project by GitLab project ID ────────────────
	var pipelineDir string
	if event.GitLabProjectID > 0 && s.projectStore != nil {
		projCfg, err := s.projectStore.FindByGitLabID(event.GitLabProjectID)
		if err != nil {
			log.Printf("webhook: store lookup error: %v", err)
			logEntry["status"] = "error"
			logEntry["error"] = err.Error()
			s.logWebhook(logEntry)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(logEntry)
			return
		}
		if projCfg == nil {
			log.Printf("webhook: unknown project gitlab_id=%d", event.GitLabProjectID)
			logEntry["status"] = "ignored"
			logEntry["error"] = "unknown project"
			s.logWebhook(logEntry)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "ignored", "reason": "unknown project",
			})
			return
		}
		pipelineDir = filepath.Join(projCfg.RepoPath, projCfg.PipelineDir)
	} else {
		pipelineDir = s.cfg.PipelineDir
	}

	proj, err := parser.ParseDir(pipelineDir)
	if err != nil {
		log.Printf("webhook: parse error: %v", err)
		logEntry["status"] = "error"
		logEntry["error"] = err.Error()
		s.logWebhook(logEntry)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(logEntry)
		return
	}

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

	logEntry["tasks"] = triggeredTasks
	logEntry["status"] = "triggered"
	if len(triggeredTasks) == 0 {
		logEntry["status"] = "skipped"
	}

	log.Printf("webhook: repo=%s branch=%s commits=%d files=%d tasks=%v",
		event.Repo, event.Branch, event.CommitCount, len(changedRepos), triggeredTasks)

	// ─── Dispatch to GitLab (if configured) ──────────────────
	var dispatchResult map[string]interface{}
	dispatchID := s.cfg.Dispatch.ProjectID
	if event.GitLabProjectID > 0 && s.projectStore != nil {
		if projCfg, _ := s.projectStore.FindByGitLabID(event.GitLabProjectID); projCfg != nil && projCfg.GitLabProjectID > 0 {
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
				logEntry["dispatch"] = map[string]interface{}{
					"dispatched": false,
					"error":      err.Error(),
				}
			} else {
				log.Printf("webhook: dispatched pipeline %d (%s)", resp.PipelineID, resp.WebURL)
				logEntry["dispatch"] = map[string]interface{}{
					"dispatched":   true,
					"pipeline_id":  resp.PipelineID,
					"pipeline_url": resp.WebURL,
				}
				dispatchResult = logEntry["dispatch"].(map[string]interface{})
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
