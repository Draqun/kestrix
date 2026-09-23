package web

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/redact"
	"kestrix/internal/version"
	"kestrix/internal/web/pagination"
)

// handleDashboard returns aggregate metrics for the overview dashboard.
// @Summary Dashboard metrics
// @Tags system
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/dashboard [get]
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	projects, _ := s.projectStore.List()
	projectCount := len(projects)

	resp := map[string]interface{}{
		"project_count": projectCount,
		"run_stats":     map[string]int{},
		"recent_runs":   []domain.Run{},
		"pg_available":  false,
	}

	stats, err := s.runStore.CountRunsByStatus()
	if err == nil {
		resp["run_stats"] = stats
	}
	runs, err := s.runStore.ListAllRuns(10)
	if err == nil {
		if runs == nil {
			runs = []domain.Run{}
		}
		resp["recent_runs"] = runs
	}
	resp["pg_available"] = true

	json.NewEncoder(w).Encode(resp)
}

// handleHealth godoc
// @Summary Health check
// @Description Returns 503 when a backing store query fails; the body then
// @Description carries "status": "degraded" and omits the failed counter.
// @Tags system
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 503 {object} map[string]interface{}
// @Router /api/health [get]
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := map[string]interface{}{
		"status":  "ok",
		"service": "kestrix",
		"repos":   0,
	}
	// A counter is omitted rather than reported as zero when its store fails,
	// so a health check cannot stay green while its backing store is down.
	if s.webhookLogStore != nil {
		n, err := s.webhookLogStore.CountWebhookEvents(domain.WebhookEventFilter{})
		if err != nil {
			slog.Warn("health: count webhook events failed", "err", redact.Err(err, s.knownSecrets()))
			resp["status"] = "degraded"
		} else {
			resp["webhooks"] = n
		}
	} else {
		resp["webhooks"] = 0
	}
	if s.projectStore != nil {
		projects, err := s.projectStore.List()
		if err != nil {
			slog.Warn("health: list projects failed", "err", redact.Err(err, s.knownSecrets()))
			resp["status"] = "degraded"
		} else {
			resp["pipelines"] = len(projects)
		}
	} else {
		resp["pipelines"] = 0
	}
	w.Header().Set("Content-Type", "application/json")
	// The consumers that gate orchestration read the HTTP status and nothing
	// else: the Compose healthchecks run `wget --spider` and deploy.sh runs
	// `curl -sf`. A degraded body behind a 200 would leave both green.
	if resp["status"] == "degraded" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(resp)
}

// parseWebhookFilter builds a history filter from the request query. It
// returns an error message and HTTP status when a value cannot be parsed.
func parseWebhookFilter(q url.Values) (domain.WebhookEventFilter, string, int) {
	filter := domain.WebhookEventFilter{
		ProjectName: q.Get("project"),
		Status:      q.Get("status"),
	}
	if raw := q.Get("status"); raw != "" {
		switch raw {
		case domain.WebhookStatusTriggered, domain.WebhookStatusSkipped,
			domain.WebhookStatusIgnored, domain.WebhookStatusError:
		default:
			return filter, "invalid status filter", http.StatusBadRequest
		}
	}
	for _, spec := range []struct {
		name   string
		target **time.Time
	}{
		{"since", &filter.Since},
		{"until", &filter.Until},
	} {
		raw := q.Get(spec.name)
		if raw == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return filter, "invalid " + spec.name + " timestamp, expected RFC3339", http.StatusBadRequest
		}
		value := parsed
		*spec.target = &value
	}
	return filter, "", 0
}

// handleGetLogs godoc
// @Summary List webhook history
// @Description Returns recorded webhook events newest first, in the standard paginated envelope.
// @Tags webhooks
// @Produce json
// @Param limit query int false "Maximum events to return"
// @Param offset query int false "Number of events to skip"
// @Param project query string false "Filter by registered project name"
// @Param status query string false "Filter by status" Enums(triggered, skipped, ignored, error)
// @Param since query string false "Only events received at or after this RFC3339 timestamp"
// @Param until query string false "Only events received at or before this RFC3339 timestamp"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Router /api/logs [get]
func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	limit, offset, errStr, status := pagination.Parse(r.URL.Query())
	if errStr != "" {
		http.Error(w, errStr, status)
		return
	}
	filter, filterErr, filterStatus := parseWebhookFilter(r.URL.Query())
	if filterErr != "" {
		writeError(w, filterStatus, filterErr)
		return
	}

	events := []domain.WebhookEvent{}
	total := 0
	if s.webhookLogStore != nil {
		count, err := s.webhookLogStore.CountWebhookEvents(filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "read webhook history: "+err.Error())
			return
		}
		total = count
		listed, err := s.webhookLogStore.ListWebhookEvents(filter, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "read webhook history: "+err.Error())
			return
		}
		if listed != nil {
			events = listed
		}
	}

	next, prev := pagination.Links(offset, limit, total, "/api/logs")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"items":      events,
		"pagination": pagination.Meta{Total: total, Limit: limit, Offset: offset, Next: next, Prev: prev},
	})
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>Kestrix</title></head>
<body style="font-family:monospace;background:#0a1929;color:#e0e0e0;padding:40px">
<h1>🚀 Kestrix — CI/CD Control Plane</h1>
<p>Version %s — API endpoints:</p>
<ul>
  <li><a href="/docs/">/docs/</a> — Swagger UI</li>
  <li><a href="/api/health">/api/health</a> — Health check</li>
  <li><a href="/api/pipeline">/api/pipeline</a> — Get pipeline definition</li>
  <li><a href="/api/validate">/api/validate</a> — Validate pipeline</li>
  <li><a href="/api/logs">/api/logs</a> — Webhook logs</li>
</ul>
<p>Frontend: <code>cd ui && npm run dev</code> (port 3000)</p>
</body>
</html>`, version.Version)
}

// handleVersion returns the server version
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"version": version.Version})
}
