package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"kestrix/internal/domain"
	"kestrix/internal/service"
)

var defaultSSEHeartbeatInterval = 15 * time.Second

// handleRunEvents godoc
// @Summary Stream pipeline run events via SSE
// @Description Streams status, task, trace, and complete events as they happen (service.StatusPayload, service.TaskPayload, service.TracePayload, service.CompletePayload), with a ": ping" comment heartbeat every 15s and the stream closing once the run reaches a terminal status.
// @Tags runs
// @Produce text/event-stream
// @Param name path string true "Project name"
// @Param runID path int true "Run ID"
// @Success 200 {string} string "text/event-stream"
// @Failure 400 {object} map[string]string "invalid run ID"
// @Failure 404 {object} map[string]string "run not found, or project name does not match"
// @Failure 500 {object} map[string]string "streaming unsupported by the response writer"
// @Router /api/projects/{name}/runs/{runID}/events [get]
func (s *Server) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	runID, err := strconv.Atoi(chi.URLParam(r, "runID"))
	if err != nil || runID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid run ID")
		return
	}

	if s.runStore == nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}

	run, err := s.runStore.GetRun(runID)
	if err != nil || run == nil || run.ProjectName != chi.URLParam(r, "name") {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	// http.Server's WriteTimeout (120s by default) would otherwise fire on
	// this handler's very first write past that mark and silently fail every
	// later heartbeat and event write without ever closing the connection -
	// the client never sees an error, so useRunEvents never falls back to
	// polling. Clearing the deadline is safe here because the heartbeat
	// ticker below is this handler's own liveness mechanism (see REVIEW.md
	// R1).
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	hub := s.EventHub()
	events := hub.Subscribe(runID)
	defer hub.Unsubscribe(runID, events)

	// Re-check after subscribe: if the run is already terminal, emit complete and close cleanly.
	run, err = s.runStore.GetRun(runID)
	if err == nil && run != nil && domain.IsTerminalRunStatus(run.Status) {
		writeSSEEvent(w, flusher, service.NewCompleteEvent(runID, run.Status))
		return
	}

	heartbeatInterval := s.sseHeartbeatInterval
	if heartbeatInterval <= 0 {
		heartbeatInterval = defaultSSEHeartbeatInterval
	}
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			// A write or flush failure here means the connection is dead -
			// the client already saw it close, so returning promptly costs
			// nothing and avoids spinning the ticker over a socket nobody
			// is reading from (see REVIEW.md R1).
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev, ok := <-events:
			if !ok {
				return
			}
			if !writeSSEEvent(w, flusher, ev) {
				return
			}
			if ev.Type == service.EventTypeComplete {
				return
			}
		}
	}
}

// writeSSEEvent writes one SSE frame for ev and flushes it, reporting
// whether the write succeeded - the single place that formats "event:
// type\ndata: json\n\n", shared by the immediate-terminal shortcut above
// and the main event loop (see REVIEW.md R12).
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, ev service.RunEvent) bool {
	data, _ := json.Marshal(ev.Data)
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data); err != nil {
		return false
	}
	flusher.Flush()
	return true
}
