package domain

import (
	"context"
	"errors"
	"io"
	"time"
)

// Errors a CredentialStore's ResolveCredential and GetCredentialMetadata may
// return for a missing or revoked reference. Adapters wrap these with
// fmt.Errorf("%w: ...", ...) so callers - including the service layer's
// secret-resolution error messages (runtime-secrets design.md D6) - can
// match them with errors.Is without depending on adapter-specific error
// types.
var (
	ErrCredentialNotFound = errors.New("credential not found")
	ErrCredentialRevoked  = errors.New("credential is revoked")
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
	// Value is the opaque payload of a `secret` credential (see
	// runtime-secrets). It is unused by every Git credential type.
	Value string `json:"value,omitempty"`
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
	// Value is the opaque payload of a `secret` credential (see
	// runtime-secrets). It is unused by every Git credential type.
	Value string `json:"value,omitempty"`
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
	// Upsert writes the project row and keeps its primary source row in step
	// with repo_url, for callers that have no derived set to hand over
	// (seeding, tests). Create and update use UpsertWithSources instead.
	Upsert(cfg *ProjectConfig) error
	// UpsertWithSources writes the project row and the project's complete
	// derived source set in one transaction, so a rejected set leaves the
	// registration untouched rather than half applied behind an error
	// response.
	UpsertWithSources(cfg *ProjectConfig, sources []ProjectSource) error
	Delete(name string) error
	List() ([]string, error)
	// ReplaceSources swaps a project's derived source set in full; see
	// ProjectSource. Sources are a projection of the project row and its
	// parsed pipeline, never edited one at a time.
	ReplaceSources(name string, sources []ProjectSource) error
	// FindBySourceKey returns every project, ordered by name and with Sources
	// populated, that has a source whose match key is in keys.
	FindBySourceKey(keys []string) ([]*ProjectConfig, error)
}

// ProjectSource is one repository a project listens to: its primary
// repository or a remote ci_repo declared by its pipeline. Name is the
// ci_repo name (the project name for the primary). It carries no credential.
type ProjectSource struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Primary bool   `json:"primary"`
}

// RunStore is the port for run lifecycle persistence.
// Only PostgreSQL implements this.
type RunStore interface {
	CreateRun(projectName string, changed []string, includeManual bool, snap *Project, startedBy string) (int, error)
	FinishRun(id int, status, output, errMsg string, taskResults []TaskResult) error
	AppendTaskResult(runID int, tr TaskResult) error
	FlushRunOutput(runID int, output string) error
	CancelRun(id int, canceledBy string) error
	ApplyTaskResult(id int, tr TaskResult, outputExtra string) error
	// SetRunQueued marks a non-terminal run "queued" while it waits for a
	// concurrency slot; see run-concurrency-and-retention design.md D1/D2. A
	// run already terminal (passed/failed/canceled) is left untouched.
	SetRunQueued(id int) error
	// StartQueuedRun transitions a "queued" run to "running" once a slot is
	// acquired. ok is false when the row was not "queued" (already canceled
	// concurrently, or never queued), telling the caller not to execute it.
	StartQueuedRun(id int) (ok bool, err error)
	// FailInterruptedRuns marks every run left "running" or "queued" by a
	// previous process as failed, so a restart cannot leave ghosts in run
	// lists, counters, or guards - a "queued" run's place in the in-memory
	// scheduler does not survive a restart either, see
	// run-concurrency-and-retention design.md Migration Plan. A run whose
	// only non-terminal task result is "manual" is left alone. Returns the
	// number of runs failed.
	FailInterruptedRuns(reason string) (int, error)
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
	// Actor is the account that requested this run, task run, or approval,
	// "user:<name>". Set by the handler from authenticated claims, never
	// from client input - see run-attribution D2, D5. The runner ignores it.
	Actor string
	// ResolvedSecretValues holds every runtime-secret value BuildVarStore
	// resolved for this run or task, in resolution order. The service layer
	// uses it to redact these values from output before persisting or
	// returning it - see runtime-secrets design.md D5. The runner ignores it.
	ResolvedSecretValues []string
}

// WebhookEvent is a single processed webhook delivery, recorded in webhook
// history. Status is one of "triggered", "skipped", "ignored", or "error".
type WebhookEvent struct {
	ID             int       `json:"id"`
	ReceivedAt     time.Time `json:"received_at"`
	ProjectName    string    `json:"project_name,omitempty"`
	SourceName     string    `json:"source_name,omitempty"`
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

// UserStore persists user accounts and their password hashes. GetUser and
// ListUsers never expose a password hash; only GetUserCredential does, and it
// exists solely for authentication.
//
// UpdateUserRole and UpdateUserPassword each bump the account's token
// version as part of the same write, so a token issued before the change
// stops verifying; see TokenIssuer and Claims. BumpTokenVersion exists for
// callers that need to force that without changing the role or password.
type UserStore interface {
	CreateUser(name string, role Role, passwordHash string) (User, error)
	// CreateFirstUser creates name/role/passwordHash as an account only
	// while no account exists at all, atomically with the emptiness check;
	// see first-run-setup design.md D1. It returns ErrSetupDone once any
	// account exists, and never creates, modifies, or deletes anything in
	// that case.
	CreateFirstUser(name string, role Role, passwordHash string) (User, error)
	GetUser(name string) (User, error)
	GetUserCredential(name string) (UserCredential, error)
	ListUsers() ([]User, error)
	// HasAnyUser reports whether at least one account exists, without
	// fetching any row - see first-run-setup's "Setup status is
	// discoverable without credentials" requirement, answered by an
	// unauthenticated, unthrottled route on every UI mount.
	HasAnyUser() (bool, error)
	UpdateUserRole(name string, role Role) error
	UpdateUserPassword(name string, passwordHash string) error
	DeleteUser(name string) error
	BumpTokenVersion(name string) error
}

// GrantStore persists per-project role grants. ResolveGrant reports whether
// a grant exists at all, distinct from returning the zero Role.
type GrantStore interface {
	SetProjectGrant(userName, projectName string, role Role) error
	RemoveProjectGrant(userName, projectName string) error
	ListGrantsForUser(userName string) ([]ProjectGrant, error)
	ListGrantsForProject(projectName string) ([]ProjectGrant, error)
	ResolveGrant(userName, projectName string) (role Role, found bool, err error)
}

// PasswordHasher hashes and verifies passwords. Implementations must use a
// memory-hard algorithm, a per-call random salt, and must not be sensitive to
// timing across the success and failure paths.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(hash, password string) (bool, error)
}

// IdentityCache bounds how long RequireAuth's per-request identity lookup
// stays fresh; see design.md D4. Get reports a cache miss for both an
// absent entry and an expired one - callers cannot tell the two apart, and
// do not need to. Set stores the current entry with the cache's own TTL and
// eviction policy; a zero-TTL cache implementation MAY treat Set as a no-op
// and Get as always missing, disabling caching entirely. Invalidate drops
// any entry for name immediately, used by writes that change what the
// entry would report (role, password, or token-version changes; account
// deletion) so a stale privilege cannot outlive the write that revoked it.
type IdentityCache interface {
	Get(name string) (entry IdentityCacheEntry, ok bool)
	Set(name string, entry IdentityCacheEntry)
	Invalidate(name string)
}

// TokenIssuer issues and verifies signed authentication tokens. Verify must
// reject a token whose signature does not check out, whose signing algorithm
// is not the expected one, or whose expiry has passed.
type TokenIssuer interface {
	Issue(user User) (token string, expiresAt time.Time, err error)
	Verify(token string) (Claims, error)
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
	// WebhookSecret is write-only: it is accepted on create and update but
	// SHALL NOT appear in any response - see webhook-ingress-hardening D4.
	// Responses carry HasWebhookSecret instead; callers encoding a
	// ProjectConfig for the wire MUST go through the web package's
	// publicProject helper first.
	WebhookSecret string `json:"webhook_secret,omitempty"`
	// HasWebhookSecret reports whether a webhook secret is stored, without
	// disclosing it. Only publicProject sets this on an outgoing copy; the
	// stored struct itself does not populate it.
	HasWebhookSecret bool   `json:"has_webhook_secret"`
	CreatedAt        string `json:"created_at"`
	// Sources is derived and read-only; a client-supplied value is ignored.
	Sources []ProjectSource `json:"sources,omitempty"`
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
	// StartedBy is the actor that started the run, "user:<name>". Empty for
	// a run recorded before run-attribution existed.
	StartedBy string `json:"started_by"`
	// CanceledBy is the actor that canceled the run. Empty unless the run
	// was canceled while running or queued - see run-attribution D3 and
	// run-concurrency-and-retention design.md D2.
	CanceledBy string `json:"canceled_by,omitempty"`
}
