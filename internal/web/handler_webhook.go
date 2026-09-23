package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"kestrix/internal/dag"
	"kestrix/internal/dispatch"
	"kestrix/internal/domain"
	"kestrix/internal/model"
	"kestrix/internal/parser"
	"kestrix/internal/redact"
	"kestrix/internal/source"
)

func (s *Server) logWebhook(event domain.WebhookEvent) {
	if s.webhookService == nil {
		return
	}
	s.webhookService.Record(event)
}

// writeWebhookErrorResponse answers the failure path that reports the event it
// just recorded. logEntry is a domain.WebhookEvent because it is the history
// row; encoding that struct directly would put the persistence type's field
// names on the wire (received_at, triggered_tasks, and an always-zero id) and
// silently change the response contract of POST /webhook, so the response
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

// webhookMatch pairs a registered project with the source of it that the
// delivery's repository matched.
type webhookMatch struct {
	cfg    *domain.ProjectConfig
	source domain.ProjectSource
	// keys are the delivery's repository match keys; they name the pushed
	// repository inside the freshly parsed pipeline, whatever the ci_repo is
	// called there now.
	keys []string
}

// webhookResult is one project's outcome within a delivery. A delivery that
// matches several projects yields one entry each; the single-match case is a
// list of one and additionally mirrors its fields at the top level of the
// response for callers that predate fan-out.
type webhookResult struct {
	Project     string   `json:"project"`
	SourceName  string   `json:"source_name,omitempty"`
	Status      string   `json:"status"`
	Changed     []string `json:"changed"`
	Tasks       []string `json:"tasks"`
	Count       int      `json:"count"`
	Dispatched  bool     `json:"dispatched"`
	PipelineID  int      `json:"pipeline_id,omitempty"`
	PipelineURL string   `json:"pipeline_url,omitempty"`
	Error       string   `json:"error,omitempty"`

	dispatchAttempted bool
	failureCode       int
}

func newWebhookEntry(event *PushEvent) domain.WebhookEvent {
	return domain.WebhookEvent{
		ReceivedAt:     time.Now().UTC(),
		Repo:           event.Repo,
		Branch:         event.Branch,
		Revision:       event.CheckoutSHA,
		Commits:        event.CommitCount,
		TriggeredTasks: []string{},
		Status:         domain.WebhookStatusError,
	}
}

// handleWebhook godoc
// @Summary Receive GitLab webhook
// @Tags webhooks
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /webhook [post]
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	defer r.Body.Close()

	// ─── Route and authenticate before anything in the body is trusted ───
	// The project ID and repository URLs are probed from the raw body, the
	// matching projects are resolved, and the delivery is verified against
	// each matched project's own secret (or the global one) before the
	// payload is parsed. A project whose secret does not verify is skipped
	// without a history row; a delivery no matched project and no global
	// secret accepts answers 401. See design.md D6.
	probe := probeWebhook(body)
	keys := sourceMatchKeys(collectRepoURLs(probe))
	matches, ignored, err := s.matchProjects(probe.ID, keys)
	if err != nil {
		slog.Warn("webhook: store lookup error", "err", redact.Err(err, s.knownSecrets()))
		writeError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	verified := make([]webhookMatch, 0, len(matches))
	for _, m := range matches {
		if verifyDelivery(s.cfg.Signature.Method, r.Header, body, m.cfg.WebhookSecret, s.cfg.Signature.Secret) {
			verified = append(verified, m)
		}
	}
	if len(matches) > 0 && len(verified) == 0 {
		if s.metrics != nil {
			s.metrics.WebhookDelivery("rejected")
		}
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if len(matches) == 0 && !verifyDelivery(s.cfg.Signature.Method, r.Header, body, "", s.cfg.Signature.Secret) {
		if s.metrics != nil {
			s.metrics.WebhookDelivery("rejected")
		}
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	matches = verified

	event, err := ParseGitLabPushEvent(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	logEntry := newWebhookEntry(event)

	if ignored {
		// Log the match keys, not the raw URLs: a key never carries userinfo.
		slog.Info("webhook: unknown project", "gitlab_project_id", event.GitLabProjectID, "source_keys", keys)
		logEntry.Status = domain.WebhookStatusIgnored
		logEntry.Error = "unknown project"
		s.logWebhook(logEntry)
		if s.metrics != nil {
			s.metrics.WebhookDelivery("skipped")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ignored", "reason": "unknown project",
		})
		return
	}

	var results []webhookResult
	if len(matches) == 0 {
		// Single pipeline directory, no registered project. The parsed
		// pipeline still names the project the event is about; recording it
		// keeps the history Project column usable in this deployment too.
		proj, err := parser.ParseDir(s.cfg.PipelineDir)
		if err != nil {
			slog.Warn("webhook: parse error", "err", redact.Err(err, s.knownSecrets()))
			logEntry.Error = err.Error()
			s.logWebhook(logEntry)
			writeWebhookErrorResponse(w, logEntry)
			return
		}
		logEntry.ProjectName = proj.Name
		changed := DefaultClassifier().MatchRepos(event.FilesChanged, proj.Repos)
		results = []webhookResult{s.resolveAndDispatch(event, proj, changed, s.dispatchTarget(nil), logEntry)}
	}
	for _, m := range matches {
		results = append(results, s.resolveOne(r.Context(), event, m))
	}
	writeWebhookResponse(w, results)
}

// matchProjects finds every registered project the delivery belongs to: by
// repository URL against derived sources first, then by GitLab project ID as
// a fallback for registrations without a routable URL. ignored reports that a
// registered-project lookup was possible but found nothing; no matches and
// not ignored means the receiver runs against its single pipeline directory.
func (s *Server) matchProjects(gitlabProjectID int, keys []string) (matches []webhookMatch, ignored bool, err error) {
	if s.projectStore == nil {
		return nil, false, nil
	}
	if len(keys) > 0 {
		projects, err := s.projectStore.FindBySourceKey(keys)
		if err != nil {
			return nil, false, err
		}
		for _, p := range projects {
			matches = append(matches, webhookMatch{cfg: p, source: matchedSource(p, keys), keys: keys})
		}
		if len(matches) > 0 {
			return matches, false, nil
		}
	}
	if gitlabProjectID > 0 {
		p, err := s.projectStore.FindByGitLabID(gitlabProjectID)
		if err != nil {
			return nil, false, err
		}
		if p == nil {
			return nil, true, nil
		}
		return []webhookMatch{{cfg: p, source: primarySource(p)}}, false, nil
	}
	return nil, false, nil
}

// sourceMatchKeys reduces the payload's URLs to distinct match keys. A URL
// that yields no key (embedded credentials, malformed) is dropped here, so it
// is neither matched, stored, nor logged.
func sourceMatchKeys(urls []string) []string {
	var keys []string
	seen := make(map[string]bool)
	for _, u := range urls {
		key, err := source.SourceMatchKey(u)
		if err != nil || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys
}

func primarySource(p *domain.ProjectConfig) domain.ProjectSource {
	return domain.ProjectSource{Name: p.Name, URL: p.RepoURL, Primary: true}
}

// matchedSource returns the source of p whose key is among keys. Sources are
// stored primary first, so a repository that is both is reported as primary.
func matchedSource(p *domain.ProjectConfig, keys []string) domain.ProjectSource {
	wanted := make(map[string]bool, len(keys))
	for _, k := range keys {
		wanted[k] = true
	}
	for _, src := range p.Sources {
		if key, err := source.SourceMatchKey(src.URL); err == nil && wanted[key] {
			return src
		}
	}
	return primarySource(p)
}

// dispatchTarget is the GitLab project a resolved pipeline is dispatched to:
// the registered project's own ID, or the global dispatch target when the
// registration (or the single-directory mode) has none.
func (s *Server) dispatchTarget(cfg *domain.ProjectConfig) int {
	if cfg != nil && cfg.GitLabProjectID > 0 {
		return cfg.GitLabProjectID
	}
	var id int
	fmt.Sscanf(s.cfg.Dispatch.ProjectID, "%d", &id)
	return id
}

// resolveOne processes a delivery for one matched project: loads its pipeline,
// computes the changed set, resolves the DAG, dispatches, and records one
// history row. A failure becomes this entry's error and never stops the other
// matches.
//
// The pipeline is always the primary repository's file. A push to the primary
// loads it at the event SHA and matches the pushed files against every
// ci_repo. A push to a secondary source cannot be checked out there, so the
// pipeline is loaded at the configured repo_ref, and the changed set is that
// one ci_repo when the push carries a code change - never a path match of the
// secondary repository's files against the primary's subpath blocks.
//
// Sources are re-derived only from a load of the configured ref, which on this
// path means a secondary delivery. A primary delivery loads its own immutable
// SHA: normally the ref's head, but deliveries can arrive out of order or be
// retried, and deriving from an older commit would resurrect a ci_repo that a
// later commit removed - a correctly signed replay could then quietly restore
// routing. The definition route and create/update cover the remaining refresh
// points, and both load the configured ref.
func (s *Server) resolveOne(ctx context.Context, event *PushEvent, m webhookMatch) webhookResult {
	entry := newWebhookEntry(event)
	entry.ProjectName = m.cfg.Name
	entry.SourceName = m.source.Name
	fail := func(code int, msg string) webhookResult {
		entry.Error = msg
		s.logWebhook(entry)
		return webhookResult{Project: m.cfg.Name, SourceName: m.source.Name, Status: "error", Error: msg,
			Changed: []string{}, Tasks: []string{}, failureCode: code}
	}

	revision := ""
	if m.source.Primary {
		if event.CheckoutSHA == "" {
			return fail(http.StatusBadRequest, "push event has no immutable checkout SHA")
		}
		revision = event.CheckoutSHA
	}
	proj, session, err := s.loadProjectPipeline(ctx, m.cfg, revision, "webhook")
	if err != nil {
		return fail(http.StatusUnprocessableEntity, err.Error())
	}
	defer session.Cleanup()

	if !m.source.Primary {
		s.refreshSources(m.cfg, proj)
	}
	classifier := DefaultClassifier()
	var changed []string
	if m.source.Primary {
		changed = classifier.MatchRepos(event.FilesChanged, proj.Repos)
	} else {
		// The pushed ci_repo's name comes from the pipeline just loaded, so
		// the history row, the response, and KESTRIX_SOURCE_NAME agree with
		// the changed set even when the block was renamed in Git.
		entry.SourceName = pushedRepoName(proj, m)
		if classifier.HasCodeChanges(event.FilesChanged) {
			changed = []string{entry.SourceName}
		}
	}
	return s.resolveAndDispatch(event, proj, changed, s.dispatchTarget(m.cfg), entry)
}

// pushedRepoName finds the ci_repo in the just-parsed pipeline whose URL is
// the pushed repository, so a ci_repo renamed in Git since the sources were
// last derived is still marked changed under the name the DAG knows. The
// stored source name is the fallback when the pipeline no longer declares it.
func pushedRepoName(proj model.Project, m webhookMatch) string {
	wanted := make(map[string]bool, len(m.keys))
	for _, k := range m.keys {
		wanted[k] = true
	}
	for _, repo := range proj.Repos {
		if repo.URL == "" {
			continue
		}
		if key, err := source.SourceMatchKey(repo.URL); err == nil && wanted[key] {
			return repo.Name
		}
	}
	return m.source.Name
}

// resolveAndDispatch evaluates the DAG against changed, dispatches when
// configured, records the history row, and returns the result entry.
func (s *Server) resolveAndDispatch(event *PushEvent, proj model.Project, changed []string, dispatchProjectID int, entry domain.WebhookEvent) webhookResult {
	if changed == nil {
		changed = []string{}
	}
	changedSet := make(map[string]bool, len(changed))
	for _, c := range changed {
		changedSet[c] = true
	}
	nodes, _ := dag.New(proj).Resolve(changedSet)
	tasks := []string{}
	for _, n := range nodes {
		if n.Triggered {
			tasks = append(tasks, n.Task.Name)
		}
	}
	entry.TriggeredTasks = tasks
	entry.Status = domain.WebhookStatusTriggered
	outcome := "accepted"
	if len(tasks) == 0 {
		entry.Status = domain.WebhookStatusSkipped
		outcome = "skipped"
	}
	if s.metrics != nil {
		s.metrics.WebhookDelivery(outcome)
	}
	slog.Info("webhook processed", "repo", event.Repo, "branch", event.Branch,
		"commits", event.CommitCount, "files", len(changed), "tasks", tasks)

	// entry.ProjectName is the registration name for a registered project and
	// the HCL name in single-directory mode, so results pair with history rows.
	res := webhookResult{Project: entry.ProjectName, SourceName: entry.SourceName, Status: "ok",
		Changed: changed, Tasks: tasks, Count: len(tasks)}

	// ─── Dispatch to GitLab (if configured) ──────────────────
	if s.cfg.Dispatch.Enabled && dispatchProjectID > 0 && len(tasks) > 0 {
		res.dispatchAttempted = true
		client := &dispatch.Client{
			BaseURL: s.cfg.Dispatch.GitLabURL,
			Token:   s.cfg.Dispatch.GitLabToken,
		}
		vars := map[string]string{
			"KESTRIX_TRIGGERED_TASKS": strings.Join(tasks, ","),
			"KESTRIX_CHANGED_REPOS":   strings.Join(changed, ","),
			"KESTRIX_DISPATCH_TIME":   time.Now().UTC().Format(time.RFC3339),
			"KESTRIX_SOURCE_NAME":     entry.SourceName,
			"KESTRIX_SOURCE_REVISION": event.CheckoutSHA,
		}
		ref := s.cfg.Branch
		if ref == "" {
			ref = event.Branch
		}
		resp, err := client.Dispatch(dispatch.DispatchRequest{
			ProjectID: dispatchProjectID,
			Ref:       ref,
			Variables: vars,
		})
		if err != nil {
			slog.Warn("webhook: dispatch error", "err", redact.Err(err, s.knownSecrets()))
			entry.Status = domain.WebhookStatusError
			entry.Error = err.Error()
			res.Error = err.Error()
		} else {
			slog.Info("webhook: dispatched pipeline", "pipeline_id", resp.PipelineID, "pipeline_url", resp.WebURL)
			entry.DispatchedID = resp.PipelineID
			entry.DispatchedURL = resp.WebURL
			res.Dispatched = true
			res.PipelineID = resp.PipelineID
			res.PipelineURL = resp.WebURL
		}
	}

	s.logWebhook(entry)
	return res
}

// writeWebhookResponse answers with {status, results}. status is ok when at
// least one project resolved and error when every matched project failed; a
// single result also mirrors the pre-fan-out top-level fields so existing
// callers keep reading the same document. When every match failed, the HTTP
// code is 422 if any project failed to load its pipeline, otherwise 400 (the
// push itself was unusable, e.g. no immutable SHA).
func writeWebhookResponse(w http.ResponseWriter, results []webhookResult) {
	status := "error"
	code := http.StatusOK
	for _, r := range results {
		if r.Status == "ok" {
			status = "ok"
		}
	}
	response := map[string]interface{}{"status": status, "results": results}
	if status == "error" {
		code = http.StatusBadRequest
		for _, r := range results {
			if r.failureCode == http.StatusUnprocessableEntity {
				code = r.failureCode
			}
		}
	}
	if len(results) == 1 {
		r := results[0]
		response["project"] = r.Project
		if r.Status == "ok" {
			response["changed"] = r.Changed
			response["tasks"] = r.Tasks
			response["count"] = r.Count
			response["dispatched"] = r.Dispatched
			if r.dispatchAttempted {
				var id, url interface{}
				if r.Dispatched {
					id, url = r.PipelineID, r.PipelineURL
				}
				response["pipeline_id"] = id
				response["pipeline_url"] = url
			}
		} else {
			response["error"] = r.Error
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(response)
}
