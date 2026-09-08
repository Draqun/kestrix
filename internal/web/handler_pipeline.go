package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"kestrix/internal/dag"
	"kestrix/internal/parser"
)

var readPipelineFile = os.ReadFile

// errPipelineDirOutsideRoot is returned by boundPipelineDir both for a
// requested directory that resolves outside the configured pipeline root
// and for one that does not exist at all - the two answer identically so
// the check cannot be used to probe which paths exist on the server; see
// design.md D5.
var errPipelineDirOutsideRoot = errors.New("pipeline directory is not within the configured pipeline root")

// boundPipelineDir resolves requested - following any symbolic links - and
// requires the result to be root or a descendant of it. An empty requested
// keeps meaning root itself, unresolved, matching every call site's
// existing default when no dir is supplied. See design.md D5.
func boundPipelineDir(root, requested string) (string, error) {
	if requested == "" {
		return root, nil
	}
	resolvedRoot, err := resolvePipelinePath(root)
	if err != nil {
		// The configured root itself is unusable: refuse rather than fall
		// back to an unbounded requested path.
		return "", errPipelineDirOutsideRoot
	}
	resolvedRequested, err := resolvePipelinePath(requested)
	if err != nil {
		return "", errPipelineDirOutsideRoot
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedRequested)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errPipelineDirOutsideRoot
	}
	return resolvedRequested, nil
}

// resolvePipelinePath is boundPipelineDir's clock-free filesystem seam:
// absolute path plus symlink resolution, both of which fail for a path that
// does not exist.
var resolvePipelinePath = func(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// handleGetPipeline godoc
// @Summary Parse and return pipeline definition
// @Tags pipeline
// @Produce json
// @Param dir query string false "Pipeline directory"
// @Success 200 {object} model.Project
// @Router /api/pipeline [get]
func (s *Server) handleGetPipeline(w http.ResponseWriter, r *http.Request) {
	dir, err := boundPipelineDir(s.cfg.PipelineDir, r.URL.Query().Get("dir"))
	if err != nil {
		writeError(w, http.StatusForbidden, "pipeline directory is not accessible")
		return
	}

	proj, err := parser.ParseDir(dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(proj)
}

// handleGetPipelineRaw godoc
// @Summary Return raw pipeline HCL source
// @Tags pipeline
// @Produce text/plain
// @Param dir query string false "Pipeline directory"
// @Success 200 {string} string
// @Router /api/pipeline/raw [get]
func (s *Server) handleGetPipelineRaw(w http.ResponseWriter, r *http.Request) {
	dir, err := boundPipelineDir(s.cfg.PipelineDir, r.URL.Query().Get("dir"))
	if err != nil {
		writeError(w, http.StatusForbidden, "pipeline directory is not accessible")
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var tfFile string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".tf") {
			tfFile = filepath.Join(dir, e.Name())
			break
		}
	}
	if tfFile == "" {
		writeError(w, http.StatusNotFound, "no .tf file found")
		return
	}

	data, err := readPipelineFile(tfFile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}

// handleResolve godoc
// @Summary Resolve trigger expressions against changed repos
// @Tags pipeline
// @Accept json
// @Produce json
// @Param request body object true "Changed repos"
// @Success 200 {object} map[string]interface{}
// @Router /api/resolve [post]
func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Changed []string `json:"changed"`
		Dir     string   `json:"dir"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	dir, err := boundPipelineDir(s.cfg.PipelineDir, req.Dir)
	if err != nil {
		writeError(w, http.StatusForbidden, "pipeline directory is not accessible")
		return
	}

	proj, err := parser.ParseDir(dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	changedSet := make(map[string]bool)
	for _, c := range req.Changed {
		changedSet[c] = true
	}

	e := dag.New(proj)
	nodes, err := e.Resolve(changedSet)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var triggered []string
	for _, n := range nodes {
		if n.Triggered {
			triggered = append(triggered, n.Task.Name)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"project":   proj.Name,
		"changed":   req.Changed,
		"triggered": triggered,
		"all_tasks": len(proj.Tasks),
	})
}

// handleGenerate godoc
// @Summary Generate gitlab-ci.yml from pipeline definition
// @Tags pipeline
// @Accept json
// @Produce json
// @Param request body object true "Generation options"
// @Success 200 {object} map[string]interface{}
// @Router /api/generate [post]
func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir     string   `json:"dir"`
		Changed []string `json:"changed"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	dir, err := boundPipelineDir(s.cfg.PipelineDir, req.Dir)
	if err != nil {
		writeError(w, http.StatusForbidden, "pipeline directory is not accessible")
		return
	}

	proj, err := parser.ParseDir(dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	e := dag.New(proj)
	changedSet := make(map[string]bool)
	for _, c := range req.Changed {
		changedSet[c] = true
	}

	nodes, err := e.Resolve(changedSet)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var triggered []dag.Node
	for _, n := range nodes {
		if n.Triggered {
			triggered = append(triggered, n)
		}
	}

	yaml := GenerateGitLabCI(proj, triggered)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"yaml":       yaml,
		"task_count": len(triggered),
	})
}

// handleValidate godoc
// @Summary Validate pipeline definition
// @Tags pipeline
// @Produce json
// @Param dir query string false "Pipeline directory"
// @Success 200 {object} map[string]interface{}
// @Router /api/validate [get]
func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	dir, err := boundPipelineDir(s.cfg.PipelineDir, r.URL.Query().Get("dir"))
	if err != nil {
		writeError(w, http.StatusForbidden, "pipeline directory is not accessible")
		return
	}

	proj, err := parser.ParseDir(dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	e := dag.New(proj)
	if err := e.Validate(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"valid":  false,
			"errors": []string{err.Error()},
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"valid":   true,
		"project": proj.Name,
		"repos":   len(proj.Repos),
		"tasks":   len(proj.Tasks),
	})
}
