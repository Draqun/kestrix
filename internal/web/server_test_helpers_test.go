package web

import (
	"encoding/json"
	"os"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/runner"
	"kestrix/internal/service"
)

// NewServer provides dependency-injected wiring for unit tests. Production
// startup must use NewServerE, which requires PostgreSQL.
func NewServer(cfg Config) *Server {
	projectStore := &mocks.MockProjectStore{}
	projectStore.Projects = make(map[string]*domain.ProjectConfig)
	if cfg.StorePath != "" {
		if data, err := os.ReadFile(cfg.StorePath); err == nil {
			var projects map[string]*domain.ProjectConfig
			if json.Unmarshal(data, &projects) == nil {
				projectStore.Projects = projects
			}
		}
	}
	runStore := &mocks.MockRunStore{CreateRunID: 1}
	pipelineRunner := &runner.PipelineExecutor{Mode: "local", GenerateYAML: GenerateGitLabCI}
	return NewServerWithDeps(
		cfg,
		projectStore,
		runStore,
		service.NewProjectService(projectStore),
		pipelineRunner,
		pipelineRunner,
		service.NewRunService(runStore, pipelineRunner, pipelineRunner),
		nil,
	)
}
