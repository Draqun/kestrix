package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"

	"kestrix/internal/domain"
	"kestrix/internal/model"
	"kestrix/internal/variables"
)

// ProjectService handles project CRUD with validation.
type ProjectService struct {
	store domain.ProjectStore
}

// ErrValidation identifies invalid project configuration supplied by a client.
var ErrValidation = errors.New("project validation failed")

func validateProject(cfg *domain.ProjectConfig) error {
	if strings.TrimSpace(cfg.Name) == "" {
		return fmt.Errorf("%w: project name is required", ErrValidation)
	}
	url := strings.TrimSpace(cfg.RepoURL)
	ref := strings.TrimSpace(cfg.RepoRef)
	if url == "" {
		return fmt.Errorf("%w: repo_url is required", ErrValidation)
	}
	if ref == "" {
		return fmt.Errorf("%w: repo_ref is required", ErrValidation)
	}
	parsed, parseErr := urlpkg(url)
	if parseErr != nil && url != "" {
		return fmt.Errorf("%w: repo_url is invalid", ErrValidation)
	}
	if parsed != nil && parsed.User != nil {
		_, hasPassword := parsed.User.Password()
		if parsed.Scheme != "ssh" || hasPassword {
			return fmt.Errorf("%w: repo_url must not contain embedded credentials", ErrValidation)
		}
	}
	return nil
}

func urlpkg(raw string) (*url.URL, error) {
	return url.Parse(raw)
}

// NewProjectService creates a ProjectService backed by the given store.
func NewProjectService(store domain.ProjectStore) *ProjectService {
	return &ProjectService{store: store}
}

// Create validates and persists a new project.
func (s *ProjectService) Create(cfg *domain.ProjectConfig) error {
	return s.CreateValidated(cfg, nil)
}

// CreateValidated validates the complete remote source before persistence.
func (s *ProjectService) CreateValidated(cfg *domain.ProjectConfig, validateSource func(*domain.ProjectConfig) error) error {
	if err := validateProject(cfg); err != nil {
		return err
	}
	if cfg.DefaultBranch == "" {
		cfg.DefaultBranch = "main"
	}
	if cfg.PipelineFile == "" {
		cfg.PipelineFile = "pipeline.tf"
	}
	if cfg.PipelineDir == "" {
		cfg.PipelineDir = "ci"
	}
	if validateSource != nil {
		if err := validateSource(cfg); err != nil {
			return fmt.Errorf("%w: validate project source: %v", ErrValidation, err)
		}
	}
	return s.store.Upsert(cfg)
}

// Update validates and updates an existing project.
func (s *ProjectService) Update(name string, cfg *domain.ProjectConfig) error {
	return s.UpdateValidated(name, cfg, nil)
}

// UpdateValidated replaces a project only after the candidate source and
// pipeline definition have been validated successfully.
func (s *ProjectService) UpdateValidated(name string, cfg *domain.ProjectConfig, validateSource func(*domain.ProjectConfig) error) error {
	existing, err := s.store.Get(name)
	if err != nil {
		return fmt.Errorf("check project: %w", err)
	}
	if existing == nil {
		return fmt.Errorf("project %q not found", name)
	}
	cfg.Name = name
	if cfg.PipelineFile == "" {
		cfg.PipelineFile = "pipeline.tf"
	}
	if cfg.DefaultBranch == "" {
		cfg.DefaultBranch = "main"
	}
	if cfg.CreatedAt == "" {
		cfg.CreatedAt = existing.CreatedAt
	}
	if cfg.PipelineDir == "" {
		cfg.PipelineDir = "ci"
	}
	if err := validateProject(cfg); err != nil {
		return err
	}
	if validateSource != nil {
		if err := validateSource(cfg); err != nil {
			return fmt.Errorf("%w: validate project source: %v", ErrValidation, err)
		}
	}
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
	runStore   domain.RunStore
	pipeline   domain.PipelineRunner
	taskRunner domain.TaskRunner
}

// NewRunService creates a RunService.
func NewRunService(runStore domain.RunStore, pipeline domain.PipelineRunner, taskRunner domain.TaskRunner) *RunService {
	return &RunService{runStore: runStore, pipeline: pipeline, taskRunner: taskRunner}
}

// Start creates a run and executes the pipeline in the background.
// Returns the run ID immediately; execution happens asynchronously.
func (s *RunService) Start(ctx context.Context, projectName string, proj *model.Project, changed []string, opts domain.RunnerOptions) (int, error) {
	if s.pipeline == nil {
		return 0, fmt.Errorf("pipeline runner not configured")
	}
	runID, err := s.createRun(projectName, changed, opts.IncludeManual, proj)
	if err != nil {
		return 0, err
	}
	output := &bytes.Buffer{}
	s.prepareRunExecution(runID, output, &opts)

	go s.executeRun(ctx, runID, proj, changed, opts, output)
	return runID, nil
}

func (s *RunService) createRun(projectName string, changed []string, includeManual bool, proj *model.Project) (int, error) {
	if proj == nil {
		return 0, fmt.Errorf("project not loaded")
	}
	if s.runStore == nil {
		return 0, fmt.Errorf("run store is not configured")
	}
	runID, err := s.runStore.CreateRun(projectName, changed, includeManual, proj)
	if err != nil {
		return 0, fmt.Errorf("create run: %w", err)
	}
	for _, t := range proj.Tasks {
		_ = s.runStore.AppendTaskResult(runID, domain.TaskResult{Name: t.Name, Status: "pending"})
	}
	return runID, nil
}

func (s *RunService) prepareRunExecution(runID int, output *bytes.Buffer, opts *domain.RunnerOptions) {
	previousSourcesAcquired := opts.OnSourcesAcquired
	opts.Stdout = output
	opts.Stderr = output
	opts.OnTaskStart = func(name string) {
		if s.runStore != nil {
			_ = s.runStore.AppendTaskResult(runID, domain.TaskResult{Name: name, Status: "running"})
		}
	}
	opts.OnTaskComplete = func(tr domain.TaskResult) {
		if s.runStore != nil {
			_ = s.runStore.AppendTaskResult(runID, tr)
			_ = s.runStore.FlushRunOutput(runID, output.String())
		}
	}
	opts.OnSourcesAcquired = func(sources []domain.AcquiredSource) error {
		allSources := append([]domain.AcquiredSource(nil), opts.InitialSources...)
		allSources = append(allSources, sources...)
		if sourceStore, ok := s.runStore.(domain.RunSourceStore); ok {
			if err := sourceStore.UpdateRunSources(runID, allSources); err != nil {
				return fmt.Errorf("persist run sources: %w", err)
			}
		}
		if opts.SourceSessions != nil {
			if err := opts.SourceSessions.Pin(context.Background(), runID, allSources); err != nil {
				_ = opts.SourceSessions.Release(context.Background(), runID)
				return fmt.Errorf("pin run sources: %w", err)
			}
		}
		if previousSourcesAcquired != nil {
			return previousSourcesAcquired(allSources)
		}
		return nil
	}
}

func (s *RunService) executeRun(ctx context.Context, runID int, proj *model.Project, changed []string, opts domain.RunnerOptions, output *bytes.Buffer) {
	defer func() {
		if r := recover(); r != nil && s.runStore != nil {
			_ = s.runStore.FinishRun(runID, "failed", output.String(), fmt.Sprintf("panic: %v", r), nil)
			if opts.SourceSessions != nil {
				_ = opts.SourceSessions.Release(context.Background(), runID)
			}
		}
	}()

	changedSet := make(map[string]bool, len(changed))
	for _, c := range changed {
		changedSet[c] = true
	}
	taskResults, err := s.pipeline.Run(ctx, *proj, changedSet, opts)
	s.finishRun(runID, output.String(), taskResults, err, opts.SourceSessions)
}

func (s *RunService) finishRun(runID int, output string, taskResults []domain.TaskResult, runErr error, sourceSessions domain.SourceSessionManager) {
	status, errMsg := runStatus(taskResults, runErr)
	if s.runStore != nil {
		_ = s.runStore.FinishRun(runID, status, output, errMsg, taskResults)
	}
	if sourceSessions != nil && !hasExecutableRunState(taskResults) {
		_ = sourceSessions.Release(context.Background(), runID)
	}
}

func runStatus(taskResults []domain.TaskResult, runErr error) (string, string) {
	status := "passed"
	errMsg := ""
	if runErr != nil {
		status = "failed"
		errMsg = runErr.Error()
	}
	for _, tr := range taskResults {
		if tr.Status != "failed" {
			continue
		}
		status = "failed"
		if errMsg == "" {
			errMsg = tr.Error
		}
		break
	}
	if hasExecutableRunState(taskResults) {
		status = "running"
	}
	return status, errMsg
}

func hasExecutableRunState(taskResults []domain.TaskResult) bool {
	for _, tr := range taskResults {
		if tr.Status == "manual" || tr.Status == "pending" || tr.Status == "running" {
			return true
		}
	}
	return false
}

// RunTask executes a single task and appends the result to an existing run.
func (s *RunService) RunTask(ctx context.Context, runID int, projectName string, proj *model.Project, taskName string, opts domain.RunnerOptions) (domain.TaskResult, error) {
	_ = projectName
	if proj == nil {
		return domain.TaskResult{}, fmt.Errorf("project not loaded")
	}
	if s.taskRunner == nil {
		return domain.TaskResult{}, fmt.Errorf("task runner not configured")
	}
	if opts.WorkspaceCleanup != nil {
		defer opts.WorkspaceCleanup()
	}
	if s.runStore != nil {
		_ = s.runStore.ApplyTaskResult(runID, domain.TaskResult{Name: taskName, Status: "running"}, "")
	}
	if !opts.SourcesPrepared {
		previousSourcesAcquired := opts.OnSourcesAcquired
		opts.OnSourcesAcquired = func(sources []domain.AcquiredSource) error {
			allSources := append([]domain.AcquiredSource(nil), opts.InitialSources...)
			allSources = append(allSources, sources...)
			if sourceStore, ok := s.runStore.(domain.RunSourceStore); ok {
				if err := sourceStore.UpdateRunSources(runID, allSources); err != nil {
					return fmt.Errorf("persist run sources: %w", err)
				}
			}
			if opts.SourceSessions != nil {
				if err := opts.SourceSessions.Pin(ctx, runID, allSources); err != nil {
					_ = opts.SourceSessions.Release(context.Background(), runID)
					return fmt.Errorf("pin run sources: %w", err)
				}
			}
			if previousSourcesAcquired != nil {
				return previousSourcesAcquired(allSources)
			}
			return nil
		}
	}

	tr, err := s.taskRunner.RunTask(ctx, *proj, taskName, opts)
	if err != nil && tr.Status == "" {
		tr.Status = "failed"
		tr.Error = err.Error()
	}
	if s.runStore != nil {
		_ = s.runStore.ApplyTaskResult(runID, tr, tr.Output)
	}
	if opts.SourceSessions != nil && s.runStore != nil {
		if run, getErr := s.runStore.GetRun(runID); getErr == nil && run != nil && !hasExecutableRunState(run.TaskResults) {
			_ = opts.SourceSessions.Release(context.Background(), runID)
		}
	}
	if err != nil {
		return tr, fmt.Errorf("task %q failed: %w", taskName, err)
	}
	return tr, nil
}

func taskResultsByName(run *domain.Run) map[string]domain.TaskResult {
	results := make(map[string]domain.TaskResult)
	if run == nil {
		return results
	}
	for _, tr := range run.TaskResults {
		results[tr.Name] = tr
	}
	return results
}

func manualTaskContext(run *domain.Run, proj *model.Project, taskName string) (bool, []string) {
	manualOK := false
	dependsOn := []string{}
	if run != nil && run.PipelineDef != nil {
		for _, t := range run.PipelineDef.Tasks {
			if t.Name == taskName {
				manualOK = t.Manual
				dependsOn = t.DependsOn
				break
			}
		}
	}
	if proj != nil {
		for i := range proj.Tasks {
			if proj.Tasks[i].Name == taskName {
				if proj.Tasks[i].Manual {
					manualOK = true
				}
				if len(dependsOn) == 0 {
					dependsOn = proj.Tasks[i].DependsOn
				}
				break
			}
		}
	}
	return manualOK, dependsOn
}

func isWaitingForApproval(tr domain.TaskResult) bool {
	return tr.Status == "manual" || (tr.Status == "skipped" && tr.Error != "dependency failed" && tr.Error != "dependency canceled")
}

// ApproveManualTask validates a waiting manual task and executes it.
func (s *RunService) ApproveManualTask(ctx context.Context, runID int, projectName string, proj *model.Project, taskName string, opts domain.RunnerOptions) (domain.TaskResult, error) {
	if s.runStore == nil {
		return domain.TaskResult{}, fmt.Errorf("runs not supported")
	}
	run, err := s.runStore.GetRun(runID)
	if err != nil {
		return domain.TaskResult{}, fmt.Errorf("get run: %w", err)
	}
	if run == nil || run.ProjectName != projectName {
		return domain.TaskResult{}, fmt.Errorf("run not found")
	}

	latest := taskResultsByName(run)
	cur, ok := latest[taskName]
	if !ok {
		return domain.TaskResult{}, fmt.Errorf("task not in run")
	}
	if !isWaitingForApproval(cur) {
		return domain.TaskResult{}, fmt.Errorf("task %q is not waiting for manual approval (status=%s)", taskName, cur.Status)
	}

	manualOK, dependsOn := manualTaskContext(run, proj, taskName)
	if !manualOK {
		return domain.TaskResult{}, fmt.Errorf("task is not manual")
	}
	for _, dep := range dependsOn {
		d, exists := latest[dep]
		if !exists || d.Status != "passed" {
			return domain.TaskResult{}, fmt.Errorf("dependency %q has not passed", dep)
		}
	}

	return s.RunTask(ctx, runID, projectName, proj, taskName, opts)
}

// WebhookService handles webhook event processing.
type WebhookService struct {
	logSink domain.WebhookLogSink
}

// NewWebhookService creates a WebhookService.
func NewWebhookService(logSink domain.WebhookLogSink) *WebhookService {
	return &WebhookService{logSink: logSink}
}

// Record stores a processed webhook event in history. A storage failure is
// logged and swallowed: webhook delivery must not fail because history is
// unavailable, or GitLab would retry a push that Kestrix already handled.
func (s *WebhookService) Record(event domain.WebhookEvent) {
	if s == nil || s.logSink == nil {
		return
	}
	if err := s.logSink.Record(event); err != nil {
		log.Printf("webhook history: record event repo=%s status=%s: %v", event.Repo, event.Status, err)
	}
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
