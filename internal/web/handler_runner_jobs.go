package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"kestrix/internal/redact"
)

// runnerLongPollInterval bounds how long GET /api/runner-jobs/next blocks
// waiting for a pending job before answering 204 - see tasks.md 3.2. It
// polls the store at runnerLongPollTick, a cheap interval given Phase 1's
// scale (two runners, one project - design.md Risks). Package variables,
// not constants, so a unit test isn't forced to wait the real interval -
// the same pattern service.CancelWaitBuffer uses.
var (
	runnerLongPollInterval = 20 * time.Second
	runnerLongPollTick     = 500 * time.Millisecond
)

// runnerIDContextKey is the context key requireRunnerAuth stores an
// authenticated runner's ID under.
type runnerIDContextKey struct{}

func runnerIDFromContext(ctx context.Context) (int, bool) {
	id, ok := ctx.Value(runnerIDContextKey{}).(int)
	return id, ok
}

// runnerPollLimiter bounds how many concurrent GET /api/runner-jobs/next
// long-polls one authenticated runner token may hold open at once
// (design.md D4) - a misbehaving or compromised token then has a
// documented ceiling, not none.
type runnerPollLimiter struct {
	mu    sync.Mutex
	inUse map[int]int
}

func newRunnerPollLimiter() *runnerPollLimiter {
	return &runnerPollLimiter{inUse: make(map[int]int)}
}

// acquire reports whether runnerID is under max concurrent polls and, if
// so, reserves one slot. Call release when the poll returns.
func (l *runnerPollLimiter) acquire(runnerID, max int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inUse[runnerID] >= max {
		return false
	}
	l.inUse[runnerID]++
	return true
}

func (l *runnerPollLimiter) release(runnerID int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.inUse[runnerID]--
	if l.inUse[runnerID] <= 0 {
		delete(l.inUse, runnerID)
	}
}

// requireRunnerAuth authenticates the runner-job route group with a bearer
// token resolved through RunnerStore.AuthenticateRunner - a separate
// credential space from a user session or an /api token (access-control
// spec's "Runner token does not authenticate /api"): it never calls
// RequireAuth, never resolves an account or role, and a user session
// presented here is rejected the same as any other unrecognized value.
func (s *Server) requireRunnerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) || s.runnerStore == nil {
			writeError(w, http.StatusUnauthorized, missingCredentialMessage)
			return
		}
		token := header[len(prefix):]
		runnerID, ok, err := s.runnerStore.AuthenticateRunner(token)
		if err != nil || !ok {
			writeError(w, http.StatusUnauthorized, missingCredentialMessage)
			return
		}
		ctx := context.WithValue(r.Context(), runnerIDContextKey{}, runnerID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// handleRunnerJobNext is a bounded long-poll: it claims the oldest pending
// job for the authenticated runner, blocking up to runnerLongPollInterval
// before answering 204 when none arrives - never an error (tasks.md 3.2).
func (s *Server) handleRunnerJobNext(w http.ResponseWriter, r *http.Request) {
	runnerID, ok := runnerIDFromContext(r.Context())
	if !ok || s.remoteJobStore == nil {
		writeError(w, http.StatusInternalServerError, "runner jobs not configured")
		return
	}

	if s.runnerPollLimiter != nil {
		max := s.runnerJobMaxConcurrentPolls()
		if !s.runnerPollLimiter.acquire(runnerID, max) {
			writeError(w, http.StatusTooManyRequests, "too many concurrent polls for this runner")
			return
		}
		defer s.runnerPollLimiter.release(runnerID)
	}

	deadline := time.Now().Add(runnerLongPollInterval)
	ticker := time.NewTicker(runnerLongPollTick)
	defer ticker.Stop()
	for {
		job, err := s.remoteJobStore.ClaimNextJob(runnerID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "claim job")
			return
		}
		if job != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(job)
			return
		}
		if time.Now().After(deadline) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

// runnerTraceRequest is the body PATCH /api/runner-jobs/{id}/trace accepts.
type runnerTraceRequest struct {
	Epoch  int    `json:"epoch"`
	Offset *int   `json:"offset"`
	Chunk  string `json:"chunk"`
}

// handleRunnerJobTrace implements PATCH /api/runner-jobs/{id}/trace
// (tasks.md 3.3): offset-based, redacted before storage, rejected as a
// no-op when the epoch no longer matches or the job is not this runner's own.
func (s *Server) handleRunnerJobTrace(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}
	runnerID, ok := runnerIDFromContext(r.Context())
	if !ok || s.remoteJobStore == nil {
		writeError(w, http.StatusInternalServerError, "runner jobs not configured")
		return
	}
	var req runnerTraceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Offset == nil {
		writeError(w, http.StatusBadRequest, "offset is required")
		return
	}
	secrets, err := s.jobSecrets(jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load job secrets")
		return
	}
	chunk := redact.Values(req.Chunk, secrets)
	maxOutputBytes := 0
	if s.runService != nil {
		maxOutputBytes = s.runService.MaxOutputBytes
	}
	traced, err := s.remoteJobStore.AppendJobTrace(jobID, req.Epoch, *req.Offset, chunk, maxOutputBytes, &runnerID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !traced {
		writeError(w, http.StatusConflict, "stale epoch, job not owned by this runner, or job already terminal")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// jobSecrets returns every value text a runner sends back for jobID must
// have redacted before it is stored: this server's own secrets plus the
// resolved runtime secrets of the job's run (remote-runner-fixes design.md
// D1) - knownSecrets alone never covers a pipeline's secret_ref values.
func (s *Server) jobSecrets(jobID int) ([]string, error) {
	values, err := s.remoteJobStore.RemoteJobSecretValues(jobID)
	if err != nil {
		return nil, err
	}
	return append(s.knownSecrets(), values...), nil
}

// maxRunnerTaskErrorBytes bounds the error a completion report may attach
// to a task result: it is runner-supplied text, stored per task.
const maxRunnerTaskErrorBytes = 4096

// runnerCompleteRequest is the body PUT /api/runner-jobs/{id}/complete
// accepts. Error is optional; an older runner never sends it.
type runnerCompleteRequest struct {
	Epoch  int    `json:"epoch"`
	Status string `json:"status"`
	Output string `json:"output"`
	Error  string `json:"error"`
}

// handleRunnerJobComplete implements PUT /api/runner-jobs/{id}/complete
// (tasks.md 3.4): a thin adapter into RunService.CompleteRemoteJob, which
// owns the epoch/ownership/terminal-run fencing and the dependency-resumption
// step.
func (s *Server) handleRunnerJobComplete(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}
	runnerID, ok := runnerIDFromContext(r.Context())
	if !ok || s.runService == nil || s.remoteJobStore == nil {
		writeError(w, http.StatusInternalServerError, "run service not configured")
		return
	}
	var req runnerCompleteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	secrets, err := s.jobSecrets(jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load job secrets")
		return
	}
	output := redact.Values(req.Output, secrets)
	// Redact before cutting, so the cut can never leave part of a secret
	// that no longer matches its whole value.
	taskError := redact.Values(req.Error, secrets)
	if len(taskError) > maxRunnerTaskErrorBytes {
		cut := maxRunnerTaskErrorBytes
		for cut > 0 && !utf8.RuneStart(taskError[cut]) {
			cut-- // never split a multi-byte character
		}
		taskError = taskError[:cut]
	}
	completed, err := s.runService.CompleteRemoteJob(jobID, req.Epoch, req.Status, output, taskError, &runnerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !completed {
		writeError(w, http.StatusConflict, "stale epoch, job not owned by this runner, or job already terminal")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// runnerCreateRequest is the body POST /api/runners accepts.
type runnerCreateRequest struct {
	Name string `json:"name"`
}

// handleCreateRunner implements POST /api/runners (tasks.md 3.5):
// admin-only, returns the bearer token once.
func (s *Server) handleCreateRunner(w http.ResponseWriter, r *http.Request) {
	var req runnerCreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if s.runnerStore == nil {
		writeError(w, http.StatusInternalServerError, "runners not configured")
		return
	}
	id, token, err := s.runnerStore.CreateRunner(req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "name": req.Name, "token": token})
}

// handleRevokeRunner implements DELETE /api/runners/{id} (tasks.md 3.5):
// admin-only.
func (s *Server) handleRevokeRunner(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runner id")
		return
	}
	if s.runnerStore == nil {
		writeError(w, http.StatusInternalServerError, "runners not configured")
		return
	}
	if err := s.runnerStore.RevokeRunner(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "revoked"})
}
