package domain

import (
	"context"
	"io"
	"time"
)

// SourceAcquirer obtains a repository in an isolated workspace before task
// execution starts. Implementations may use a local checkout or Git CLI.
type SourceAcquirer interface {
	Acquire(ctx context.Context, repo Repo, opts AcquisitionOptions) (AcquiredSource, error)
}

// SourceSessionManager refreshes a remote source through the shared Git object
// cache and returns an isolated checkout owned by one operation.
type SourceSessionManager interface {
	Open(ctx context.Context, request SourceRequest) (SourceSession, error)
	Pin(ctx context.Context, runID int, sources []AcquiredSource) error
	Release(ctx context.Context, runID int) error
	Maintain(ctx context.Context) error
}

// SourceRequest identifies one remote source without carrying secret values.
// Revision, when set, is an exact historical commit to materialize while
// RequestedRef remains the user-facing branch/tag/commit expression.
type SourceRequest struct {
	RepoName      string
	URL           string
	RequestedRef  string
	Revision      string
	CredentialRef string
	Purpose       string
	Primary       bool
}

// SourceSession owns one private checkout. Cleanup is idempotent.
type SourceSession struct {
	Source  AcquiredSource
	Cleanup func()
}

// CredentialResolver resolves a named credential without exposing its value
// to pipeline definitions or persisted run metadata.
type CredentialResolver interface {
	Resolve(ctx context.Context, ref string) (Credential, error)
}

// Credential contains process-scoped authentication material. Implementations
// must never serialize or log this value.
type Credential struct {
	Type       string
	Username   string
	Password   string
	Token      string
	PrivateKey string
	KnownHosts string
}

// CredentialInput is accepted only by secret-bearing create/rotate operations.
// It must never be serialized in a response or persisted in plaintext.
type CredentialInput struct {
	Type       string `json:"type"`
	Name       string `json:"name"`
	Provider   string `json:"provider,omitempty"`
	Host       string `json:"host,omitempty"`
	Username   string `json:"username,omitempty"`
	Password   string `json:"password,omitempty"`
	Token      string `json:"token,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
	KnownHosts string `json:"known_hosts,omitempty"`
}

// CredentialMetadata is safe to return to callers.
type CredentialMetadata struct {
	Ref       string    `json:"ref"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Provider  string    `json:"provider,omitempty"`
	Host      string    `json:"host,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Revoked   bool      `json:"revoked"`
}

// CredentialStore owns secret lifecycle and runtime resolution.
type CredentialStore interface {
	CreateCredential(input CredentialInput) (CredentialMetadata, error)
	RotateCredential(ref string, input CredentialInput) (CredentialMetadata, error)
	GetCredentialMetadata(ref string) (CredentialMetadata, error)
	ListCredentialMetadata() ([]CredentialMetadata, error)
	RevokeCredential(ref string) error
	ResolveCredential(ctx context.Context, ref string) (Credential, error)
}

// AcquisitionOptions controls one source acquisition.
type AcquisitionOptions struct {
	Workspace    string
	BaseDir      string
	Timeout      time.Duration
	Retries      int
	RetainOnFail bool
	Resolver     CredentialResolver
}

// AcquiredSource is the non-secret result of source acquisition.
type AcquiredSource struct {
	RepoName         string `json:"repo_name"`
	URL              string `json:"url,omitempty"`
	Path             string `json:"-"`
	RequestedRef     string `json:"requested_ref,omitempty"`
	ResolvedRevision string `json:"resolved_revision,omitempty"`
	CredentialRef    string `json:"credential_ref,omitempty"`
	Primary          bool   `json:"primary,omitempty"`
}

// ProjectStore is the port for PostgreSQL-backed project configuration
// persistence.
type ProjectStore interface {
	Load() (map[string]*ProjectConfig, error)
	Save(projects map[string]*ProjectConfig) error
	Get(name string) (*ProjectConfig, error)
	FindByGitLabID(id int) (*ProjectConfig, error)
	Upsert(cfg *ProjectConfig) error
	Delete(name string) error
	List() ([]string, error)
}

// RunStore is the port for run lifecycle persistence.
// Only PostgreSQL implements this.
type RunStore interface {
	CreateRun(projectName string, changed []string, includeManual bool, snap *Project) (int, error)
	FinishRun(id int, status, output, errMsg string, taskResults []TaskResult) error
	AppendTaskResult(runID int, tr TaskResult) error
	FlushRunOutput(runID int, output string) error
	CancelRun(id int) error
	ApplyTaskResult(id int, tr TaskResult, outputExtra string) error
	GetRun(id int) (*Run, error)
	CountRuns(projectName string) (int, error)
	ListRuns(projectName string, limit, offset int) ([]Run, error)
	ListAllRuns(limit int) ([]Run, error)
	CountRunsByStatus() (map[string]int, error)
}

// RunSourceStore persists non-secret source metadata after acquisition.
// It is optional so lightweight test stores and alternative backends remain
// compatible with the core run lifecycle port.
type RunSourceStore interface {
	UpdateRunSources(runID int, sources []AcquiredSource) error
}

// PipelineRunner is the port for pipeline execution.
// Implemented by runner.PipelineExecutor.
type PipelineRunner interface {
	Run(ctx context.Context, project Project, changed map[string]bool, opts RunnerOptions) ([]TaskResult, error)
}

// TaskRunner is the port for executing a single manual task.
// Implemented by runner.PipelineExecutor.
type TaskRunner interface {
	RunTask(ctx context.Context, project Project, taskName string, opts RunnerOptions) (TaskResult, error)
}

// VariableResolver supplies the variables visible to a task.
type VariableResolver interface {
	Get(repo, job, env string) map[string]string
}

// RunnerOptions is the domain-level configuration for a pipeline run.
// It mirrors the runner.Options struct but lives in domain so the
// service layer can construct it without importing runner.
type RunnerOptions struct {
	WorkDir               string
	IncludeManual         bool
	Variables             VariableResolver
	Stdout                io.Writer
	Stderr                io.Writer
	OnTaskStart           func(name string)
	OnTaskComplete        func(TaskResult)
	OnSourcesAcquired     func([]AcquiredSource) error
	InitialSources        []AcquiredSource
	SourceSessions        SourceSessionManager
	SourceAcquirer        SourceAcquirer
	CredentialResolver    CredentialResolver
	SourceTimeout         time.Duration
	SourceRetries         int
	RetainSourceWorkspace bool
	WorkspaceCleanup      func()
	RepoPaths             map[string]string
	SourcesPrepared       bool
}

// WebhookEvent is a single processed webhook delivery, recorded in webhook
// history. Status is one of "triggered", "skipped", "ignored", or "error".
type WebhookEvent struct {
	ID             int       `json:"id"`
	ReceivedAt     time.Time `json:"received_at"`
	ProjectName    string    `json:"project_name,omitempty"`
	Repo           string    `json:"repo"`
	Branch         string    `json:"branch"`
	Revision       string    `json:"revision,omitempty"`
	Commits        int       `json:"commits"`
	TriggeredTasks []string  `json:"triggered_tasks"`
	Status         string    `json:"status"`
	Error          string    `json:"error,omitempty"`
	DispatchedID   int       `json:"dispatched_pipeline_id,omitempty"`
	DispatchedURL  string    `json:"dispatched_pipeline_url,omitempty"`
}

// Webhook event statuses.
const (
	WebhookStatusTriggered = "triggered"
	WebhookStatusSkipped   = "skipped"
	WebhookStatusIgnored   = "ignored"
	WebhookStatusError     = "error"
)

// WebhookEventFilter narrows a webhook history query. Zero-valued fields are
// not applied; the fields that are set combine conjunctively.
type WebhookEventFilter struct {
	ProjectName string
	Status      string
	Since       *time.Time
	Until       *time.Time
}

// WebhookLogSink is the write port for webhook history. Implementations must
// not fail webhook delivery: recording errors are reported, never propagated
// to the caller of the webhook endpoint.
type WebhookLogSink interface {
	Record(event WebhookEvent) error
}

// WebhookLogStore is the read port for webhook history.
type WebhookLogStore interface {
	ListWebhookEvents(filter WebhookEventFilter, limit, offset int) ([]WebhookEvent, error)
	CountWebhookEvents(filter WebhookEventFilter) (int, error)
}

// ProjectConfig is the configuration of a registered project.
// It lives in domain so both ProjectStore and ProjectService can use it
// without importing store.
type ProjectConfig struct {
	Name              string `json:"name"`
	RepoURL           string `json:"repo_url"`
	RepoRef           string `json:"repo_ref"`
	RepoCredentialRef string `json:"repo_credential_ref,omitempty"`
	PipelineDir       string `json:"pipeline_dir,omitempty"`
	PipelineFile      string `json:"pipeline_file"`
	GitLabProjectID   int    `json:"gitlab_project_id,omitempty"`
	DefaultBranch     string `json:"default_branch,omitempty"`
	WebhookSecret     string `json:"webhook_secret,omitempty"`
	CreatedAt         string `json:"created_at"`
}

// Run is a stored run record.
type Run struct {
	ID            int              `json:"id"`
	ProjectName   string           `json:"project_name"`
	Status        string           `json:"status"`
	Changed       []string         `json:"changed"`
	IncludeManual bool             `json:"include_manual"`
	PipelineDef   *Project         `json:"pipeline_def,omitempty"`
	Output        string           `json:"output"`
	Error         string           `json:"error,omitempty"`
	TaskResults   []TaskResult     `json:"task_results,omitempty"`
	Sources       []AcquiredSource `json:"sources,omitempty"`
	StartedAt     time.Time        `json:"started_at"`
	FinishedAt    *time.Time       `json:"finished_at,omitempty"`
}
