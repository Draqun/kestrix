package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"kestrix/internal/credentials"
	"kestrix/internal/domain"
	"kestrix/internal/model"
	"kestrix/internal/variables"
)

// defaultCancelGrace is used by Cancel when CancelGrace is unset (zero),
// mirroring the runner package's own default; see design.md D4/D5.
const defaultCancelGrace = 10 * time.Second

// CancelWaitBuffer is added to CancelGrace when Cancel waits for an active
// run's executeRun goroutine to return; see design.md D5. A package
// variable so unit tests aren't forced to wait a real 5 seconds to reach
// the timeout path.
var CancelWaitBuffer = 5 * time.Second

// ErrRunCancelTimedOut identifies a Cancel call whose run did not terminate
// within its grace period; callers use errors.Is to map this distinctly
// (for example to an HTTP 504) without exposing the underlying message,
// which may name internal timing but never a host path or secret.
var ErrRunCancelTimedOut = errors.New("run did not terminate in time")

// ProjectService handles project CRUD with validation.
type ProjectService struct {
	store domain.ProjectStore
	// credentialLookup resolves a repo_credential_ref's metadata so
	// CreateValidated/UpdateValidated can reject a `secret` credential as a
	// repository credential (see runtime-secrets design.md D4). Nil (the
	// default) performs no type check, so callers that never wire a
	// credential store keep their prior behavior.
	credentialLookup func(ref string) (domain.CredentialMetadata, error)
}

// SetCredentialLookup configures how CreateValidated and UpdateValidated
// look up a repo_credential_ref's type. A domain.CredentialStore's
// GetCredentialMetadata method satisfies this signature directly.
func (s *ProjectService) SetCredentialLookup(lookup func(ref string) (domain.CredentialMetadata, error)) {
	s.credentialLookup = lookup
}

// checkRepoCredential rejects a repo_credential_ref that names a `secret`
// credential - see runtime-secrets design.md D4 and repository-credentials
// spec.md "Secret rejected for source acquisition". A lookup error (unknown
// ref, store unavailable) is left for source acquisition to report later,
// as before this check existed; only a confirmed `secret` type is rejected
// here.
func (s *ProjectService) checkRepoCredential(cfg *domain.ProjectConfig) error {
	ref := strings.TrimSpace(cfg.RepoCredentialRef)
	if ref == "" || s.credentialLookup == nil {
		return nil
	}
	meta, err := s.credentialLookup(ref)
	if err != nil {
		return nil
	}
	if meta.Type == credentials.TypeSecret {
		return fmt.Errorf("%w: repo_credential_ref %q is a secret credential and cannot be used for Git access", ErrValidation, ref)
	}
	return nil
}

// ErrValidation identifies invalid project configuration supplied by a client.
var ErrValidation = errors.New("project validation failed")

func validateProject(cfg *domain.ProjectConfig) error {
	if strings.TrimSpace(cfg.Name) == "" {
		return fmt.Errorf("%w: project name is required", ErrValidation)
	}
	raw := strings.TrimSpace(cfg.RepoURL)
	ref := strings.TrimSpace(cfg.RepoRef)
	if raw == "" {
		return fmt.Errorf("%w: repo_url is required", ErrValidation)
	}
	if ref == "" {
		return fmt.Errorf("%w: repo_ref is required", ErrValidation)
	}
	return validateRepoURL(raw)
}

// validateRepoURL accepts only an https:// URL, an ssh:// URL, or an
// scp-like [user@]host:path address, and rejects a host that is a literal
// loopback, link-local, or unspecified address. See
// webhook-ingress-hardening design.md D6. The checks are syntactic only -
// no DNS resolution and no network access.
func validateRepoURL(raw string) error {
	if host, _, ok := splitSCPLikeAddress(raw); ok {
		if isDisallowedRepoHost(host) {
			return fmt.Errorf("%w: repo_url host is not allowed", ErrValidation)
		}
		return nil
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: repo_url is invalid", ErrValidation)
	}
	switch parsed.Scheme {
	case "https", "ssh":
	default:
		return fmt.Errorf("%w: repo_url scheme is not allowed", ErrValidation)
	}
	if parsed.User != nil {
		_, hasPassword := parsed.User.Password()
		if parsed.Scheme != "ssh" || hasPassword {
			return fmt.Errorf("%w: repo_url must not contain embedded credentials", ErrValidation)
		}
	}
	if isDisallowedRepoHost(parsed.Hostname()) {
		return fmt.Errorf("%w: repo_url host is not allowed", ErrValidation)
	}
	return nil
}

// splitSCPLikeAddress recognizes the scp-like "[user@]host:path" form: no
// "://" anywhere, and the first ":" precedes any "/". A double colon (as in
// the "ext::" transport helper) is rejected by requiring a non-empty path
// that does not itself start with ":".
func splitSCPLikeAddress(raw string) (host, path string, ok bool) {
	if strings.Contains(raw, "://") {
		return "", "", false
	}
	firstColon := strings.Index(raw, ":")
	if firstColon < 0 {
		return "", "", false
	}
	if firstSlash := strings.Index(raw, "/"); firstSlash >= 0 && firstColon > firstSlash {
		return "", "", false
	}
	rest := raw
	if at := strings.Index(raw[:firstColon], "@"); at >= 0 {
		rest = raw[at+1:]
	}
	// rest still contains raw's first colon (any "@" found above precedes
	// it), so this always finds one.
	colonIdx := strings.Index(rest, ":")
	host = rest[:colonIdx]
	path = rest[colonIdx+1:]
	if host == "" || path == "" || strings.HasPrefix(path, ":") {
		return "", "", false
	}
	return host, path, true
}

// isDisallowedRepoHost reports whether host is a literal loopback,
// link-local, or unspecified address, or the name "localhost". A hostname
// that is none of those is allowed without resolving it - see D6.
func isDisallowedRepoHost(host string) bool {
	h := strings.Trim(host, "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(h)
	if err != nil {
		return false
	}
	return addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsUnspecified()
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
	if err := s.checkRepoCredential(cfg); err != nil {
		return err
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
	// An update whose webhook_secret is absent or empty keeps the stored
	// secret unchanged; rotation requires a non-empty value. See
	// webhook-ingress-hardening design.md D4.
	if cfg.WebhookSecret == "" {
		cfg.WebhookSecret = existing.WebhookSecret
	}
	if err := validateProject(cfg); err != nil {
		return err
	}
	if err := s.checkRepoCredential(cfg); err != nil {
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

	// CancelGrace bounds how long Cancel waits, beyond an extra 5s, for an
	// active run's executeRun goroutine to return after its context is
	// cancelled. Zero selects defaultCancelGrace; see design.md D5.
	CancelGrace time.Duration

	mu     sync.Mutex
	active map[int]context.CancelFunc
	done   map[int]chan struct{}
}

// NewRunService creates a RunService.
func NewRunService(runStore domain.RunStore, pipeline domain.PipelineRunner, taskRunner domain.TaskRunner) *RunService {
	return &RunService{
		runStore:   runStore,
		pipeline:   pipeline,
		taskRunner: taskRunner,
		active:     make(map[int]context.CancelFunc),
		done:       make(map[int]chan struct{}),
	}
}

// Start creates a run and executes the pipeline in the background.
// Returns the run ID immediately; execution happens asynchronously.
func (s *RunService) Start(ctx context.Context, projectName string, proj *model.Project, changed []string, opts domain.RunnerOptions) (int, error) {
	if s.pipeline == nil {
		return 0, fmt.Errorf("pipeline runner not configured")
	}
	runID, err := s.createRun(projectName, changed, opts.IncludeManual, proj, opts.Actor)
	if err != nil {
		return 0, err
	}
	log.Printf("run: started actor=%q project=%q run=%d", opts.Actor, projectName, runID)
	output := &bytes.Buffer{}
	redactor := s.prepareRunExecution(runID, output, &opts)

	runCtx, cancel := context.WithCancel(ctx)
	doneCh := make(chan struct{})
	s.mu.Lock()
	s.active[runID] = cancel
	s.done[runID] = doneCh
	s.mu.Unlock()

	go func() {
		defer close(doneCh)
		defer cancel()
		defer func() {
			s.mu.Lock()
			delete(s.active, runID)
			delete(s.done, runID)
			s.mu.Unlock()
		}()
		s.executeRun(runCtx, runID, proj, changed, opts, output, redactor)
	}()
	return runID, nil
}

// Cancel stops an active run's executing task before its status is recorded
// as canceled: it cancels the run's context, waits up to CancelGrace plus 5s
// for its executeRun goroutine to return, then marks the run canceled in the
// store. If the wait times out, the run is left running and an error is
// returned instead - a run reported canceled while its process keeps
// executing would be worse than one correctly still reported running; see
// design.md D5. A run with no active entry (already finished, waiting for
// manual approval, or from before a restart) is canceled in the store
// directly, with no termination attempted.
func (s *RunService) Cancel(ctx context.Context, runID int, actor string) error {
	s.mu.Lock()
	cancel, hasCancel := s.active[runID]
	doneCh, hasDone := s.done[runID]
	s.mu.Unlock()

	if !hasCancel || !hasDone {
		if s.runStore == nil {
			return fmt.Errorf("runs not supported")
		}
		if err := s.runStore.CancelRun(runID, actor); err != nil {
			return err
		}
		log.Printf("run: canceled actor=%q run=%d", actor, runID)
		return nil
	}

	cancel()
	grace := s.CancelGrace
	if grace <= 0 {
		grace = defaultCancelGrace
	}
	timeout := grace + CancelWaitBuffer
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-doneCh:
		// An active entry only exists once Start has registered it, which
		// requires runStore to create the run in the first place - so
		// runStore is guaranteed non-nil here.
		if err := s.runStore.CancelRun(runID, actor); err != nil {
			return err
		}
		log.Printf("run: canceled actor=%q run=%d", actor, runID)
		return nil
	case <-timer.C:
		return fmt.Errorf("run %d did not terminate within %s: %w", runID, timeout, ErrRunCancelTimedOut)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *RunService) createRun(projectName string, changed []string, includeManual bool, proj *model.Project, startedBy string) (int, error) {
	if proj == nil {
		return 0, fmt.Errorf("project not loaded")
	}
	if s.runStore == nil {
		return 0, fmt.Errorf("run store is not configured")
	}
	runID, err := s.runStore.CreateRun(projectName, changed, includeManual, proj, startedBy)
	if err != nil {
		return 0, fmt.Errorf("create run: %w", err)
	}
	for _, t := range proj.Tasks {
		_ = s.runStore.AppendTaskResult(runID, domain.TaskResult{Name: t.Name, Status: "pending"})
	}
	return runID, nil
}

// prepareRunExecution wires runID's lifecycle callbacks onto opts and wraps
// its output in a redactingWriter built from opts.ResolvedSecretValues, so
// opts.Stdout/Stderr, FlushRunOutput, and FinishRun all see redacted text
// (see runtime-secrets design.md D5). It returns that redactor so the
// caller can Flush it before the run's final output is persisted.
func (s *RunService) prepareRunExecution(runID int, output *bytes.Buffer, opts *domain.RunnerOptions) *redactingWriter {
	previousSourcesAcquired := opts.OnSourcesAcquired
	redactor := newRedactingWriter(output, opts.ResolvedSecretValues)
	opts.Stdout = redactor
	opts.Stderr = redactor
	opts.OnTaskStart = func(name string) {
		if s.runStore != nil {
			_ = s.runStore.AppendTaskResult(runID, domain.TaskResult{Name: name, Status: "running"})
		}
	}
	opts.OnTaskComplete = func(tr domain.TaskResult) {
		tr.Output = redactValues(tr.Output, opts.ResolvedSecretValues)
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
	return redactor
}

func (s *RunService) executeRun(ctx context.Context, runID int, proj *model.Project, changed []string, opts domain.RunnerOptions, output *bytes.Buffer, redactor *redactingWriter) {
	defer func() {
		if r := recover(); r != nil && s.runStore != nil {
			_ = redactor.Flush()
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
	// Flush before reading output.String() so a secret value sitting in the
	// redactor's cross-write buffer at run end is redacted before it is
	// persisted - see design.md D5.
	_ = redactor.Flush()
	// FinishRun persists taskResults wholesale, overwriting whatever
	// AppendTaskResult wrote incrementally via OnTaskComplete during the
	// run, so this slice must be redacted independently rather than relying
	// on that earlier pass.
	taskResults = redactTaskResults(taskResults, opts.ResolvedSecretValues)
	s.finishRun(runID, output.String(), taskResults, err, opts.SourceSessions)
}

// redactTaskResults returns a copy of results with each Output field
// redacted (see design.md D5); results itself is left unmodified.
func redactTaskResults(results []domain.TaskResult, values []string) []domain.TaskResult {
	if len(results) == 0 {
		return results
	}
	out := make([]domain.TaskResult, len(results))
	for i, tr := range results {
		tr.Output = redactValues(tr.Output, values)
		out[i] = tr
	}
	return out
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
	canceled := false
	for _, tr := range taskResults {
		switch tr.Status {
		case "canceled":
			canceled = true
		case "failed":
			status = "failed"
			if errMsg == "" {
				errMsg = tr.Error
			}
		}
	}
	if canceled {
		// A task terminated by our own cancellation is not a pipeline
		// failure; see design.md D5 and the "Cancel a running local task"
		// scenario - the run SHALL be canceled even when the task that was
		// killed reports its own result as canceled rather than failed.
		status = "canceled"
		errMsg = "canceled"
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
	var approvedBy string
	var approvedAt *time.Time
	if opts.Actor != "" {
		approvedBy = opts.Actor
		t := time.Now()
		approvedAt = &t
	}
	if s.runStore != nil {
		_ = s.runStore.ApplyTaskResult(runID, domain.TaskResult{Name: taskName, Status: "running", ApprovedBy: approvedBy, ApprovedAt: approvedAt}, "")
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
	if approvedBy != "" {
		tr.ApprovedBy = approvedBy
		tr.ApprovedAt = approvedAt
	}
	// Redact before persisting, whether this task ran directly or through
	// ApproveManualTask - see design.md D5.
	tr.Output = redactValues(tr.Output, opts.ResolvedSecretValues)
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

	log.Printf("run: approved actor=%q project=%q run=%d task=%q", opts.Actor, projectName, runID, taskName)
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

// Errors BuildVarStore returns for an unresolvable secret variable; see
// design.md D6. Each is wrapped as `variable "<key>": <this>` and never
// carries the value, the credential name, or the wrapped resolver error.
var (
	ErrSecretNotFound   = errors.New("secret credential not found")
	ErrSecretRevoked    = errors.New("secret credential is revoked")
	ErrSecretWrongType  = errors.New("reference is not a secret credential")
	ErrSecretUnresolved = errors.New("secret credential could not be resolved")
)

// BuildVarStore constructs a variables.Store from a project's declared
// variables, resolving every secret_ref through resolver. It returns the
// resolved secret values (for the output redactor, see D5) and a resolution
// error - see design.md D3 - that MUST abort the run or task before it
// starts: a missing, revoked, wrong-type, or otherwise unresolvable
// reference fails closed rather than injecting an empty or stale value.
func BuildVarStore(ctx context.Context, proj *model.Project, resolver domain.CredentialResolver) (*variables.Store, []string, error) {
	vs := variables.New()
	var resolvedValues []string
	if proj != nil {
		for _, v := range proj.Variables {
			val := v.Value
			if val == "" {
				val = v.Default
			}
			if v.SecretRef != "" {
				secretVal, err := resolveSecretValue(ctx, resolver, v.SecretRef)
				if err != nil {
					return nil, nil, fmt.Errorf("variable %q: %w", v.Key, err)
				}
				val = secretVal
				resolvedValues = append(resolvedValues, val)
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
	return vs, resolvedValues, nil
}

// resolveSecretValue resolves ref through resolver and requires it to name a
// live `secret` credential. Every failure is one of the four D6 error shapes;
// the underlying resolver error is logged with the reference only, never
// returned to the caller.
func resolveSecretValue(ctx context.Context, resolver domain.CredentialResolver, ref string) (string, error) {
	if resolver == nil {
		log.Printf("secret resolution: no credential resolver configured, ref=%q", ref)
		return "", ErrSecretUnresolved
	}
	cred, err := resolver.Resolve(ctx, ref)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrCredentialNotFound):
			return "", ErrSecretNotFound
		case errors.Is(err, domain.ErrCredentialRevoked):
			return "", ErrSecretRevoked
		default:
			log.Printf("secret resolution: ref=%q: %v", ref, err)
			return "", ErrSecretUnresolved
		}
	}
	if cred.Type != credentials.TypeSecret {
		return "", ErrSecretWrongType
	}
	return cred.Value, nil
}
