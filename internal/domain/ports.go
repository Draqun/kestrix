package domain

import (
	"context"
	"errors"
	"io"
	"slices"
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
	// CreateRun records a new run. rerunTask names the task of a single-task
	// re-run (whose changed is empty); it is "" for every other run.
	CreateRun(projectName string, changed []string, rerunTask string, includeManual bool, snap *Project, startedBy string) (int, error)
	FinishRun(id int, status, output, errMsg string, taskResults []TaskResult) error
	// FinishRunIfDone finishes a run whose recorded results leave nothing in
	// flight, and only then: while any task is still pending or running the
	// run keeps its "running" status. It is a no-op for a run that is already
	// terminal, so a cancel that won the race is never undone. A run whose
	// results imply a terminal status takes that status, its error message
	// and finished_at, and its history is pruned as FinishRun's is.
	// remote-run-waits-for-dependents design.md D2.
	//
	// A terminal run is never re-derived, by this method or by
	// ApplyTaskResult - see ApplyTaskResult.
	FinishRunIfDone(runID int) error
	// AppendTaskResult records a task result and leaves the run's status,
	// error and finished_at untouched - unlike ApplyTaskResult, it does not
	// derive the run's status. A caller that records a result for a task
	// that was never run (a skipped dependent) uses it, and decides the
	// run's outcome itself, with FinishRunIfDone.
	AppendTaskResult(runID int, tr TaskResult) error
	FlushRunOutput(runID int, output string) error
	// CancelRun marks a running/queued run canceled. changed reports whether
	// this call performed the transition (false when the run was already
	// terminal) - see REVIEW.md R2, the caller uses it to record the
	// canceled metric exactly once under concurrent Cancel calls.
	CancelRun(id int, canceledBy string) (changed bool, err error)
	// ApplyTaskResult appends a task result and recomputes overall run
	// status, returned as status - the caller uses it to detect and record
	// a terminal outcome (passed/failed/canceled) exactly once, without a
	// separate racy GetRun check - see REVIEW.md R1.
	//
	// It follows the same rule FinishRunIfDone does: a terminal run is
	// never re-derived. A result that arrives for a finished run is
	// recorded, but it does not move the run back to running and does not
	// overwrite a cancel that won the race. A cancel is the one status a
	// finished run still takes - it is a deliberate act, not a derivation.
	// See REVIEW.md R3/R10.
	ApplyTaskResult(id int, tr TaskResult, outputExtra string) (status string, err error)
	// SetCanceledBy records which actor canceled a run, for the path where a
	// running task's own ApplyTaskResult call - not CancelRun - performs the
	// running/queued → canceled transition (RunTask/ApproveManualTask losing
	// the cancel race; see REVIEW.md R1/R9). A no-op if canceled_by is
	// already set, so it never overwrites CancelRun's own write.
	SetCanceledBy(id int, canceledBy string) error
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

// Remote job and job-task statuses; see remote-runner-execution spec.md.
const (
	RemoteJobStatusPending  = "pending"
	RemoteJobStatusClaimed  = "claimed"
	RemoteJobStatusRunning  = "running"
	RemoteJobStatusPassed   = "passed"
	RemoteJobStatusFailed   = "failed"
	RemoteJobStatusCanceled = "canceled"
)

// IsTerminalJobStatus reports whether a remote job status is terminal.
func IsTerminalJobStatus(status string) bool {
	return status == RemoteJobStatusPassed ||
		status == RemoteJobStatusFailed ||
		status == RemoteJobStatusCanceled
}

// RemoteJobPayload is what a runner receives from GET /api/runner-jobs/next:
// the task to execute, its fully-resolved variables (flattened - no
// control-plane variable is ever included, per task-execution-isolation),
// and a tar.gz archive of the source checkout Kestrix already acquired
// server-side - see design.md D6/D7. It names no execution engine or image
// (design.md D3).
type RemoteJobPayload struct {
	Task Task `json:"task"`
	// Repos carries the project's declared repositories - Name, Path, and
	// Kind only (never URL/CredentialRef, which name a live source the
	// runner never touches: D6 already shipped the checkout as Archive).
	// The runner needs this to resolve task.Execute.Repo's working
	// directory within the extracted archive the same way internal/runner's
	// repoDir does in-process.
	Repos []Repo `json:"repos"`
	// DefaultImage is the project's own default, for a docker-mode runner
	// resolving an image when the task names none - the same fallback
	// RunOne applies in-process. It is data already public in the pipeline
	// definition, not an engine selector Kestrix is choosing on the
	// runner's behalf (design.md D3): the runner's own configuration alone
	// decides whether it runs docker or local at all.
	DefaultImage string            `json:"default_image,omitempty"`
	Variables    map[string]string `json:"variables,omitempty"`
	// SecretValues are the resolved runtime-secret values among Variables,
	// so the runner can redact its own streamed output across chunk
	// boundaries (remote-runner-fixes design.md D1). The runner already
	// holds every value as a variable; this only says which ones to hide.
	// Variables and SecretValues are stored encrypted; the store's payload
	// column uses its own type that has neither field (D2).
	SecretValues []string `json:"secret_values,omitempty"`
	Archive      []byte   `json:"archive"`
	// Artifacts carries the archives of every passed ancestor of the task,
	// ordered topologically with ties broken by declaration order (remote-task-artifacts design D4).
	Artifacts [][]byte `json:"artifacts,omitempty"`
}

// RemoteJob is one task dispatched to a remote runner instead of executed
// in-process; see remote-runner-execution spec.md.
type RemoteJob struct {
	ID         int              `json:"id"`
	RunID      int              `json:"run_id"`
	TaskName   string           `json:"task_name"`
	Status     string           `json:"status"`
	RunnerID   *int             `json:"runner_id,omitempty"`
	Payload    RemoteJobPayload `json:"payload"`
	ClaimEpoch int              `json:"claim_epoch"`
	ClaimedAt  *time.Time       `json:"claimed_at,omitempty"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// ErrRemoteJobExists is what a RemoteJobStore's CreateRemoteJob returns
// when the task already has an active job - the run's own index rejects the
// second dispatch. It is not a failure: the task is already dispatched, and
// the caller must not record it as failed.
var ErrRemoteJobExists = errors.New("remote job already exists")

// RemoteJobStore is the port for dispatching, claiming, and completing
// remote-runner jobs; see remote-runner-execution spec.md and design.md D1/D9.
type RemoteJobStore interface {
	// CreateRemoteJob dispatches one task of runID as a pending job. It
	// returns ErrRemoteJobExists when the task already has an active job.
	CreateRemoteJob(runID int, taskName string, payload RemoteJobPayload) (int, error)
	// ClaimNextJob atomically claims the oldest pending job whose required
	// tags are all among runnerID's tags (an untagged job matches every
	// runner; remote-runner-management) via SELECT ... FOR UPDATE SKIP
	// LOCKED, incrementing claim_epoch. A job no polling runner matches is
	// skipped, never blocking newer ones. Returns a nil job and no error
	// when nothing matching is pending.
	ClaimNextJob(runnerID int) (*RemoteJob, error)
	// GetRemoteJob returns a job by ID, or nil if it does not exist.
	GetRemoteJob(jobID int) (*RemoteJob, error)
	// AppendJobTrace appends chunk to the job's run's output (a database-side
	// append, never FlushRunOutput's whole-column overwrite - D9), bounded by
	// maxOutputBytes the same way the in-process boundedWriter is. The same
	// chunk is also appended to the job's own output, bounded against that
	// output and not the run's, so a sibling that used up the aggregate's
	// budget does not shorten this task's output; a new claim of the job
	// starts that output, and its trace stream, from empty. offset is
	// the byte position, in the job's own trace stream, chunk begins at: it
	// must equal the job's current stored trace_offset to be applied. A
	// lower offset is a runner's own safe retry (for example after a
	// dropped HTTP response) - a no-op reported as ok=true, since the
	// content was already accepted. A higher offset is a gap - a real
	// protocol violation - reported as a non-nil error rather than silently
	// misplacing content. ok is false, with no error, only when epoch no
	// longer matches the job's stored claim_epoch (D1), the job is
	// already terminal, or callerRunnerID is non-nil and does not match the
	// job's stored runner_id - a caller may only trace a job it holds the
	// current claim on, not any job by numeric ID (the epoch check alone is
	// not enough: a never-claimed job's claim_epoch defaults to 0, which a
	// caller could simply guess). callerRunnerID is nil only for
	// system-initiated calls that are not attributed to an authenticated
	// runner; every HTTP-driven call passes the authenticated runner's ID.
	AppendJobTrace(jobID, epoch, offset int, chunk string, maxOutputBytes int, callerRunnerID *int) (ok bool, err error)
	// CompleteJob records a job's terminal report. The epoch check, the
	// ownership check, and the parent run's terminal-status check happen in
	// the same transaction as the task-result write (task 1.2/3.4): ok is
	// false when epoch is stale, the job is already terminal, callerRunnerID
	// is non-nil and does not match the job's stored runner_id (see
	// AppendJobTrace), or the run went terminal - by something else
	// entirely, such as FailInterruptedRuns - between the epoch check and
	// this call, since that run's dependency graph must never be revived.
	// When the job is fenced out this way but its run is already terminal,
	// its trailing output is still appended (D9's append shape) even though
	// ok is false and no task result is recorded. callerRunnerID is nil only
	// for system-initiated calls (ReclaimStaleJobs's own fail-a-stale-job
	// path); every HTTP-driven call passes the authenticated runner's ID.
	// output is bounded by maxOutputBytes the same way AppendJobTrace's
	// chunk is - a non-positive value means unbounded. The recorded task
	// result's Output is the trace accumulated for the job's current claim
	// (see AppendJobTrace) followed by output (on its own line when the
	// trace ended mid-line), bounded against the job's own budget; the job's
	// copy of the trace is then cleared, and the run's aggregate output is
	// built separately. taskError, when
	// non-empty, is stored as the task result's Error (remote-runner-fixes
	// design.md D4).
	//
	// A completion never finishes the run: it records the result, the output
	// and the job and leaves the run's status, error and finished_at alone.
	// The caller decides the run's status, once, through
	// RunStore.FinishRunIfDone - so a run's terminal status is written in one
	// place - remote-run-waits-for-dependents design.md D1. The one
	// exception is silent-runner recovery, which is not a port call:
	// ReclaimStaleJobs fails a stale job and, in the same transaction, ends
	// the run when the run's results leave nothing in flight (see its doc).
	CompleteJob(jobID, epoch int, status, output, taskError string, callerRunnerID *int, maxOutputBytes int) (ok bool, err error)
	// PendingOrClaimedJobIDsForRun returns the IDs of runID's jobs not yet
	// terminal, for CancelRun's remote-mode carve-out (task 4.3).
	PendingOrClaimedJobIDsForRun(runID int) ([]int, error)
	// CancelJobsForRun marks runID's non-terminal jobs unclaimable: a
	// "pending" job is marked "canceled" outright; a "claimed"/"running" job
	// is left for its runner to finish (no stop signal exists - D8) but its
	// eventual completion report will find the run already terminal and be
	// fenced by CompleteJob as documented above.
	CancelJobsForRun(runID int) error
	// ReclaimStaleJobs returns every "claimed"/"running" job last updated
	// before the cutoff to "pending" (bumping claim_epoch, D5), except a job
	// whose run is already terminal, which is instead finalized in place as
	// "failed" with reason - a stale job of a terminal run is neither
	// reclaimed nor left claimable. A job returned to "pending" by an
	// earlier reclaim that no runner claimed before the cutoff counts as
	// another unanswered attempt. A job whose reclaim count (never its
	// claim_epoch, which also moves on every claim) already reached
	// maxReclaims is finalized "failed" with reason instead of being
	// returned to "pending" again; a never-claimed job is never touched -
	// remote-runner-fixes design.md D3. Returns the counts reclaimed and
	// finalized-failed.
	ReclaimStaleJobs(olderThan time.Time, maxReclaims int, reason string) (reclaimed, finalizedFailed int, err error)
	// DeleteTerminalRemoteSecrets deletes the encrypted variables and secret
	// values of every terminal job and run (remote-runner-fixes design.md
	// D2). One sweep covers every way a job or run can end.
	DeleteTerminalRemoteSecrets() error
	// PutRemoteArtifact stores an artifact archive produced by taskName in runID.
	// A repeated upload replaces the existing archive (remote-task-artifacts).
	PutRemoteArtifact(runID int, taskName string, archive []byte) error
	// ListRemoteArtifactsByTasks retrieves stored artifact archives for the given tasks in runID (remote-task-artifacts).
	ListRemoteArtifactsByTasks(runID int, taskNames []string) (map[string][]byte, error)
	// DeleteTerminalRemoteArtifacts deletes stored artifacts of terminal runs (remote-task-artifacts).
	DeleteTerminalRemoteArtifacts() error
	// OutstandingJobRunIDsForProject returns the IDs of projectName's runs
	// that have a job in pending/claimed/running status - the store-backed
	// half of ActiveRunIDsForProject's remote-mode carve-out (design.md
	// D10), unioned with the in-memory active set there. A query error is
	// returned rather than swallowed so the caller can fail closed (refuse
	// a project delete) instead of failing open.
	OutstandingJobRunIDsForProject(projectName string) ([]int, error)
	// SetRemoteRunSnapshot persists the one source archive and every
	// pipeline task's resolved variables a remote-mode run's initial
	// dispatch computed - the data tasks.md 3.6's resumption walker needs
	// to dispatch a dependent task later without re-acquiring source or
	// re-resolving secrets (design.md D6/D7). Called once, at run creation.
	SetRemoteRunSnapshot(runID int, archive []byte, variables map[string]map[string]string, secretValues []string) error
	// GetRemoteRunSnapshot returns what SetRemoteRunSnapshot stored, or a
	// nil archive and empty map if none was ever set (a non-remote run, a
	// single-task re-run, or a run with no triggered task at all - see
	// startRemote, which stores one whenever any task is triggered, even when
	// every triggered task is manual and nothing is dispatched at Start).
	// resumeRemoteRun resumes only runs that have one.
	GetRemoteRunSnapshot(runID int) (archive []byte, variables map[string]map[string]string, secretValues []string, err error)
	// RemoteJobSecretValues returns the resolved runtime-secret values of a
	// job's run, for redacting what its runner sends back (trace and
	// completion) - remote-runner-fixes design.md D1. Nil once the job's
	// values have been deleted, or when the job does not exist.
	RemoteJobSecretValues(jobID int) ([]string, error)
}

// RunnerStore is the port for registering and authenticating remote
// runners; see remote-runner-execution spec.md and design.md D4.
type RunnerStore interface {
	// CreateRunner registers a new runner and returns its ID and a
	// newly-generated bearer token. The token is returned once, in plain
	// text, and never again - only its hash is stored.
	// tags are assigned here, by the administrator, and only here - a
	// runner never declares its own (remote-runner-management design.md).
	CreateRunner(name string, tags []string) (id int, token string, err error)
	// AuthenticateRunner resolves token to a runner ID in constant time and
	// records the request as the runner's last-seen time. ok is false for
	// an unknown or revoked token, which records nothing.
	AuthenticateRunner(token string) (runnerID int, ok bool, err error)
	// RevokeRunner marks a runner revoked; a revoked runner's token no
	// longer authenticates.
	RevokeRunner(id int) error
	// ListRunners returns every unrevoked runner, oldest first.
	ListRunners() ([]Runner, error)
}

// Runner is a registered runner as ListRunners reports it. It never carries
// the token or its hash.
type Runner struct {
	ID         int        `json:"id"`
	Name       string     `json:"name"`
	Tags       []string   `json:"tags"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	// Busy is true while the runner holds a claimed or running job.
	Busy bool `json:"-"`
}

// Runner liveness states, as RunnerStatus derives them.
const (
	RunnerStatusBusy      = "busy"
	RunnerStatusOnline    = "online"
	RunnerStatusOffline   = "offline"
	RunnerStatusNeverSeen = "never seen"
)

// RunnerStatus derives r's liveness at now: busy while it holds a job
// (a quiet task sends nothing until it completes, so staleness says
// nothing then), never seen before its first request, otherwise online or
// offline against offlineAfter (remote-runner-management design.md).
func RunnerStatus(r Runner, now time.Time, offlineAfter time.Duration) string {
	switch {
	case r.Busy:
		return RunnerStatusBusy
	case r.LastSeenAt == nil:
		return RunnerStatusNeverSeen
	case now.Sub(*r.LastSeenAt) <= offlineAfter:
		return RunnerStatusOnline
	default:
		return RunnerStatusOffline
	}
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
	// Mode is the project's own execution backend ("local", "docker", or
	// "remote"), set by the handler from the project's
	// stored domain.ProjectConfig.Mode - see remote-runner-registration
	// design.md D2. RunService branches on it directly (D10); a non-remote
	// PipelineRunner/TaskRunner implementation uses it to select its own
	// per-call backend instead of one instance-wide mode.
	Mode string
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

// modes are the execution backends a project's Mode or the instance's
// runner.mode may name. Unexported so no importer can change what both accept.
var modes = []string{"local", "docker", "remote"}

// IsValidMode reports whether mode names a supported execution backend. The
// empty mode is valid too: it defers to a default - the instance mode for a
// project, "local" for the instance.
func IsValidMode(mode string) bool {
	return mode == "" || slices.Contains(modes, mode)
}

// Modes returns the supported execution backends, for messages that list
// them. The empty mode is not listed.
func Modes() []string {
	return slices.Clone(modes)
}

// ResolveMode folds an empty project (or caller) mode through the
// instance default, and an empty instance default to "local" - the one
// fallback chain every mode-aware call site (RunService.CheckLocalAllowed,
// Server.CountLocalModeProjects, modeSelectingExecutor.executorFor) must
// agree on, so the rule is written once instead of three times. mode wins
// when set; instanceDefault wins when mode is empty; "local" is the final
// fallback, matching DefaultConfig's own runner.mode default.
func ResolveMode(mode, instanceDefault string) string {
	if mode != "" {
		return mode
	}
	if instanceDefault != "" {
		return instanceDefault
	}
	return "local"
}

// LocalExecutionDisabledMessage explains why Kestrix refuses `local`
// execution without runner.allow_local: a `local` task shares this
// process's master key and JWT signing key with whatever script it runs.
// Startup validation, project validation, and the stored-project run guard
// all return this same text, so the explanation is identical wherever
// `local` is refused - see safe-production-execution design.md D1.
func LocalExecutionDisabledMessage() string {
	return "local execution can read this instance's master key and JWT signing key; use runner mode remote with a registered runner, or set KESTRIX_RUNNER_ALLOW_LOCAL=true (runner.allow_local) only on a single-user machine"
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
	// Mode selects this project's execution backend: "local", "docker", or
	// "remote" - see remote-runner-registration
	// design.md D2. Empty on input defaults to the instance's configured
	// mode (ProjectService.DefaultMode); always populated on output.
	Mode            string `json:"mode,omitempty"`
	GitLabProjectID int    `json:"gitlab_project_id,omitempty"`
	DefaultBranch   string `json:"default_branch,omitempty"`
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
	// RerunTask is the task a single-task re-run was started for; empty for
	// every other run, including runs stored before the field existed.
	RerunTask string `json:"rerun_task"`
}
