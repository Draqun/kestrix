package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gitlab.com/lunaris-bot/metaci/internal/domain"
	"gitlab.com/lunaris-bot/metaci/internal/model"
	"gitlab.com/lunaris-bot/metaci/internal/variables"
)

// ProjectService handles project CRUD with validation.
type ProjectService struct {
	store domain.ProjectStore
}

// NewProjectService creates a ProjectService backed by the given store.
func NewProjectService(store domain.ProjectStore) *ProjectService {
	return &ProjectService{store: store}
}

// Create validates and persists a new project.
func (s *ProjectService) Create(cfg *domain.ProjectConfig) error {
	if strings.TrimSpace(cfg.Name) == "" {
		return errors.New("project name is required")
	}
	if strings.TrimSpace(cfg.RepoPath) == "" && strings.TrimSpace(cfg.RepoURL) == "" {
		return errors.New("repo_path or repo_url is required")
	}
	if cfg.DefaultBranch == "" {
		cfg.DefaultBranch = "main"
	}
	if cfg.PipelineFile == "" {
		cfg.PipelineFile = "pipeline.tf"
	}
	return s.store.Upsert(cfg)
}

// Update validates and updates an existing project.
func (s *ProjectService) Update(name string, cfg *domain.ProjectConfig) error {
	existing, err := s.store.Get(name)
	if err != nil {
		return fmt.Errorf("check project: %w", err)
	}
	if existing == nil {
		return fmt.Errorf("project %q not found", name)
	}
	cfg.Name = name
	return s.store.Upsert(cfg)
}

// Delete removes a project by name.
func (s *ProjectService) Delete(name string) error {
	return s.store.Delete(name)
}

// Get returns a project by name.
func (s *ProjectService) Get(name string) (*domain.ProjectConfig, error) {
	return s.store.Get(name)
}

// List returns all project names.
func (s *ProjectService) List() ([]string, error) {
	return s.store.List()
}

// Load returns all projects.
func (s *ProjectService) Load() (map[string]*domain.ProjectConfig, error) {
	return s.store.Load()
}

// FindByGitLabID returns a project by its GitLab project ID.
func (s *ProjectService) FindByGitLabID(id int) (*domain.ProjectConfig, error) {
	return s.store.FindByGitLabID(id)
}

// RunService handles run lifecycle with background execution.
type RunService struct {
	runStore domain.RunStore
	runner   domain.PipelineRunner
}

// NewRunService creates a RunService.
func NewRunService(runStore domain.RunStore, runner domain.PipelineRunner) *RunService {
	return &RunService{runStore: runStore, runner: runner}
}

// Start creates a run and executes the pipeline in the background.
// Returns the run ID immediately; execution happens asynchronously.
func (s *RunService) Start(ctx context.Context, projectName string, proj *model.Project, changed []string, includeManual bool) (int, error) {
	runID, err := s.runStore.CreateRun(projectName, changed, includeManual, proj)
	if err != nil {
		return 0, fmt.Errorf("create run: %w", err)
	}

	// Pre-populate all tasks as pending.
	for _, t := range proj.Tasks {
		_ = s.runStore.AppendTaskResult(runID, domain.TaskResult{
			Name:   t.Name,
			Status: "pending",
		})
	}

	// Execute in background.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				_ = s.runStore.FinishRun(runID, "failed", "", fmt.Sprintf("panic: %v", r), nil)
			}
		}()

		changedSet := make(map[string]bool)
		for _, c := range changed {
			changedSet[c] = true
		}

		taskResults, err := s.runner.Run(ctx, *proj, changedSet, domain.RunnerOptions{
			IncludeManual: includeManual,
		})

		status := "passed"
		errMsg := ""
		if err != nil {
			status = "failed"
			errMsg = err.Error()
		}
		for _, tr := range taskResults {
			if tr.Status == "failed" {
				status = "failed"
				if errMsg == "" {
					errMsg = tr.Error
				}
				break
			}
		}
		_ = s.runStore.FinishRun(runID, status, "", errMsg, taskResults)
	}()

	return runID, nil
}

// WebhookService handles webhook event processing.
type WebhookService struct {
	logSink domain.WebhookLogSink
}

// NewWebhookService creates a WebhookService.
func NewWebhookService(logSink domain.WebhookLogSink) *WebhookService {
	return &WebhookService{logSink: logSink}
}

// Process parses and logs a webhook event.
func (s *WebhookService) Process(event map[string]interface{}) error {
	s.logSink.Append(event)
	return nil
}

// BuildVarStore constructs a variables.Store from project config, model, and env.
func BuildVarStore(proj *model.Project, cfg *domain.ProjectConfig) *variables.Store {
	vs := variables.New()
	if proj != nil {
		for _, v := range proj.Variables {
			val := v.Value
			if val == "" {
				val = v.Default
			}
			opts := []variables.Option{}
			if v.Protected {
				opts = append(opts, variables.WithProtected(true))
			}
			if v.Masked {
				opts = append(opts, variables.WithMasked(true))
			}
			switch v.Scope {
			case "repo":
				vs.SetRepo(v.Repo, v.Key, val, opts...)
			case "job":
				vs.SetJob(v.Repo, v.Job, v.Key, val, opts...)
			default:
				vs.SetProject(v.Key, val, opts...)
			}
		}
	}
	return vs
}
