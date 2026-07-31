package web

import (
	"encoding/json"
	"fmt"
	"net/http"

	"kestrix/internal/domain"
	"kestrix/internal/version"
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
// @Tags system
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/health [get]
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.webhookMu.RLock()
	n := len(s.webhookLog)
	s.webhookMu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"service":   "kestrix",
		"pipelines": n,
		"webhooks":  n,
		"repos":     0,
	})
}

// handleGetLogs godoc
// @Summary Get webhook logs
// @Tags webhooks
// @Produce json
// @Success 200 {array} map[string]interface{}
// @Router /api/logs [get]
func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	s.webhookMu.RLock()
	cp := make([]map[string]interface{}, len(s.webhookLog))
	copy(cp, s.webhookLog)
	s.webhookMu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cp)
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
  <li><a href="/health">/health</a> — Health check</li>
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
