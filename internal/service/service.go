package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"kestrix/internal/credentials"
	"kestrix/internal/domain"
	"kestrix/internal/metrics"
	"kestrix/internal/model"
	"kestrix/internal/redact"
	"kestrix/internal/source"
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
	// allowLocal mirrors runner.allow_local: false (the default) rejects a
	// project explicitly registered with mode `local` - see
	// safe-production-execution design.md D1.
	allowLocal bool
}

// SetAllowLocal configures whether CreateValidated/UpdateValidated accept a
// project registered with mode `local`. See runner.allow_local.
func (s *ProjectService) SetAllowLocal(allow bool) {
	s.allowLocal = allow
}

// SetCredentialLookup configures how CreateValidated and UpdateValidated
// look up a repo_credential_ref's type. A domain.CredentialStore's
// GetCredentialMetadata method satisfies this signature directly.
func (s *ProjectService) SetCredentialLookup(lookup func(ref string) (domain.CredentialMetadata, error)) {
	s.credentialLookup = lookup
}

// credentialRefIsSecret reports whether ref names a `secret`-typed
// credential, and returns ref trimmed for callers to reuse in error
// messages. A lookup error (unknown ref, store unavailable) is left for
// source acquisition to report later, as before this check existed; only a
// confirmed `secret` type is rejected by callers.
func (s *ProjectService) credentialRefIsSecret(ref string) (trimmed string, isSecret bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" || s.credentialLookup == nil {
		return ref, false
	}
	meta, err := s.credentialLookup(ref)
	if err != nil {
		return ref, false
	}
	return ref, meta.Type == credentials.TypeSecret
}

// checkRepoCredential rejects a repo_credential_ref that names a `secret`
// credential - see runtime-secrets design.md D4 and repository-credentials
// spec.md "Secret rejected for source acquisition".
func (s *ProjectService) checkRepoCredential(cfg *domain.ProjectConfig) error {
	if ref, isSecret := s.credentialRefIsSecret(cfg.RepoCredentialRef); isSecret {
		return fmt.Errorf("%w: repo_credential_ref %q is a secret credential and cannot be used for Git access", ErrValidation, ref)
	}
	return nil
}

// checkCIRepoCredentials rejects a secondary ci_repo whose
// credential_ref names a `secret` credential, the same rule
// checkRepoCredential applies to the primary repo. Without it, a
// secret-typed credential_ref on a secondary ci_repo was only caught later
// at git fetch time instead of at project create/update - a known gap
// tracked in the project wiki.
func (s *ProjectService) checkCIRepoCredentials(proj model.Project) error {
	for _, repo := range proj.Repos {
		if ref, isSecret := s.credentialRefIsSecret(repo.CredentialRef); isSecret {
			return fmt.Errorf("%w: ci_repo %q credential_ref %q is a secret credential and cannot be used for Git access", ErrValidation, repo.Name, ref)
		}
	}
	return nil
}

// ErrValidation identifies invalid project configuration supplied by a client.
var ErrValidation = errors.New("project validation failed")

func validateProject(cfg *domain.ProjectConfig, allowLocal bool) error {
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
	// An empty Mode defers to the store's instance-wide default
	// (projectDefaults); see remote-runner-registration design.md D2.
	if !domain.IsValidMode(cfg.Mode) {
		return fmt.Errorf("%w: mode %q is not a recognized execution backend", ErrValidation, cfg.Mode)
	}
	// An empty mode is always fine here: if the server is running at all
	// with allowLocal false, its own startup validation already guarantees
	// the instance mode it would resolve to isn't `local` - see
	// safe-production-execution design.md D1.
	if cfg.Mode == "local" && !allowLocal {
		return fmt.Errorf("%w: mode local: %s", ErrValidation, domain.LocalExecutionDisabledMessage())
	}
	return validateRepoURL(raw)
}

// validateRepoURL accepts only an https:// URL, an ssh:// URL, or an
// scp-like [user@]host:path address, and rejects a host that is a literal
// loopback, link-local, or unspecified address. See
// webhook-ingress-hardening design.md D6. The checks are syntactic only -
// no DNS resolution and no network access.
func validateRepoURL(raw string) error {
	if err := validateRepoURLForm(raw); err != nil {
		return err
	}
	// The same key routes webhooks and identifies the primary source; a URL
	// without one (no repository path) would register but never be reached.
	if _, err := source.SourceMatchKey(raw); err != nil {
		return fmt.Errorf("%w: repo_url must name a repository path", ErrValidation)
	}
	return nil
}

// validateRepoURLForm checks the scheme, address form, and host of raw.
func validateRepoURLForm(raw string) error {
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
	if err == nil {
		return addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsUnspecified()
	}
	// netip.ParseAddr accepts only the canonical dotted-quad form; git's
	// HTTP transport (libcurl) resolves inet_aton-style literals (decimal,
	// octal, hex, short-form) as IPs too, so a legacy encoding of a
	// disallowed address must not fall through to "allowed" - see
	// repo-url-literal-ip-hardening design.md D1.
	legacy, ok := parseLegacyIPv4Literal(h)
	if !ok {
		return false
	}
	return legacy.IsLoopback() || legacy.IsLinkLocalUnicast() || legacy.IsLinkLocalMulticast() || legacy.IsUnspecified()
}

// parseLegacyIPv4Literal decodes an inet_aton-style IPv4 literal: 1-4
// dot-separated components, each decimal, 0-prefixed octal, or 0x-prefixed
// hex, with the last component absorbing the remaining bits (1 part = 32
// bits, 2 parts = 8+24, 3 parts = 8+8+16, 4 parts = 8+8+8+8). It reports
// ok=false for ordinary hostnames and malformed literals.
func parseLegacyIPv4Literal(host string) (netip.Addr, bool) {
	if host == "" {
		return netip.Addr{}, false
	}
	parts := strings.Split(host, ".")
	if len(parts) > 4 {
		return netip.Addr{}, false
	}
	vals := make([]uint64, len(parts))
	for i, p := range parts {
		if p == "" {
			return netip.Addr{}, false
		}
		base := 10
		s := p
		if len(p) >= 2 && p[0] == '0' && (p[1] == 'x' || p[1] == 'X') {
			base = 16
			s = p[2:]
			if s == "" || !isHexDigits(s) {
				return netip.Addr{}, false
			}
		} else if len(p) > 1 && p[0] == '0' {
			base = 8
			if !isOctalDigits(s) {
				return netip.Addr{}, false
			}
		} else {
			if !isDecimalDigits(s) {
				return netip.Addr{}, false
			}
		}
		v, err := strconv.ParseUint(s, base, 32)
		if err != nil {
			return netip.Addr{}, false
		}
		vals[i] = v
	}
	var b [4]byte
	switch len(vals) {
	case 1:
		v := vals[0]
		b[0] = byte(v >> 24)
		b[1] = byte(v >> 16)
		b[2] = byte(v >> 8)
		b[3] = byte(v)
	case 2:
		if vals[0] > 0xFF || vals[1] > 0xFFFFFF {
			return netip.Addr{}, false
		}
		b[0] = byte(vals[0])
		b[1] = byte(vals[1] >> 16)
		b[2] = byte(vals[1] >> 8)
		b[3] = byte(vals[1])
	case 3:
		if vals[0] > 0xFF || vals[1] > 0xFF || vals[2] > 0xFFFF {
			return netip.Addr{}, false
		}
		b[0] = byte(vals[0])
		b[1] = byte(vals[1])
		b[2] = byte(vals[2] >> 8)
		b[3] = byte(vals[2])
	case 4:
		for j, v := range vals {
			if v > 0xFF {
				return netip.Addr{}, false
			}
			b[j] = byte(v)
		}
	}
	return netip.AddrFrom4(b), true
}

func isDecimalDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

func isOctalDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '7' {
			return false
		}
	}
	return len(s) > 0
}

func isHexDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return len(s) > 0
}

// NewProjectService creates a ProjectService backed by the given store.
func NewProjectService(store domain.ProjectStore) *ProjectService {
	return &ProjectService{store: store}
}

// Create validates and persists a new project.
func (s *ProjectService) Create(cfg *domain.ProjectConfig) error {
	return s.CreateValidated(cfg, nil)
}

// SourceValidator checks a candidate registration against its remote and
// returns the pipeline it parsed there, so the service can derive the
// project's sources from the same parse instead of a second Git operation.
type SourceValidator func(*domain.ProjectConfig) (model.Project, error)

// CreateValidated validates the complete remote source before persistence.
func (s *ProjectService) CreateValidated(cfg *domain.ProjectConfig, validateSource SourceValidator) error {
	if err := validateProject(cfg, s.allowLocal); err != nil {
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
	return s.persistValidated(cfg, validateSource)
}

// persistValidated runs the remote validation, then stores the project and its
// derived sources in one store operation, so a failure leaves the registration
// untouched instead of half applied behind an error response. A client-supplied
// Sources value is overwritten here, so no caller can register a source by
// hand. Without a validator only the primary source is known; the next load of
// the configured ref adds the rest.
func (s *ProjectService) persistValidated(cfg *domain.ProjectConfig, validateSource SourceValidator) error {
	var proj model.Project
	if validateSource != nil {
		var err error
		if proj, err = validateSource(cfg); err != nil {
			return fmt.Errorf("%w: validate project source: %v", ErrValidation, err)
		}
	}
	if err := s.checkCIRepoCredentials(proj); err != nil {
		return err
	}
	cfg.Sources = DeriveSources(cfg, proj)
	return s.store.UpsertWithSources(cfg, cfg.Sources)
}

// Update validates and updates an existing project.
func (s *ProjectService) Update(name string, cfg *domain.ProjectConfig) error {
	return s.UpdateValidated(name, cfg, nil)
}

// UpdateValidated replaces a project only after the candidate source and
// pipeline definition have been validated successfully.
func (s *ProjectService) UpdateValidated(name string, cfg *domain.ProjectConfig, validateSource SourceValidator) error {
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
	if err := validateProject(cfg, s.allowLocal); err != nil {
		return err
	}
	if err := s.checkRepoCredential(cfg); err != nil {
		return err
	}
	return s.persistValidated(cfg, validateSource)
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

// activeRun tracks one in-flight run: its cancellation func and the project
// it belongs to, so callers can ask which projects have runs in flight
// without loading run rows from the store (see project-delete-run-guard
// design.md D1).
type activeRun struct {
	cancel  context.CancelFunc
	project string
	// canceledBy is the actor Cancel is canceling this run on behalf of,
	// set just before cancel() runs. Read back by persistCanceledByIfLost
	// when the run's own executeRun/runTaskInternal call - not CancelRun -
	// wins the race to record the canceled transition; see REVIEW.md R9.
	canceledBy string
}

// RunService handles run lifecycle with background execution.
type RunService struct {
	runStore   domain.RunStore
	pipeline   domain.PipelineRunner
	taskRunner domain.TaskRunner
	// remoteJobStore dispatches jobs for a "remote"-mode project instead of
	// executing in-process; see remote-runner-registration design.md D10.
	// Nil (the default, until SetRemoteJobStore is called) means Start,
	// RunTask, and ApproveManualTask never branch into that path.
	remoteJobStore domain.RemoteJobStore
	// runLocks serializes the whole completion path per run ID
	// (remote-runner-registration REVIEW.md R3/R4, remote-run-waits-for-
	// dependents design.md D3): two jobs of one run completing at nearly the
	// same time both record a result, resume the run and finish it, and
	// without this a dependent both racers agree has become eligible could
	// be dispatched twice - and one of them could finish the run before the
	// other one's result is recorded. See remote_dispatch.go's runLocker doc
	// for why an in-process mutex is enough here rather than a DB-level
	// lock, and docs/architecture.md for the single-instance constraint it
	// rests on.
	runLocks *runLocker

	// CancelGrace bounds how long Cancel waits, beyond an extra 5s, for an
	// active run's executeRun goroutine to return after its context is
	// cancelled. Zero selects defaultCancelGrace; see design.md D5.
	CancelGrace time.Duration

	// MaxOutputBytes bounds stored run output; see run-concurrency-and-retention
	// design.md D4. Zero (the default) means unbounded.
	MaxOutputBytes int

	// Metrics records run terminal-status counts and durations; see
	// observability design.md D2. Nil is treated the same as a metrics.NoOp:
	// every call site nil-checks before recording, so a RunService built
	// without one (most tests) behaves exactly as before this field existed.
	Metrics metrics.Recorder

	// slots is a counting semaphore bounding concurrent runs; see
	// run-concurrency-and-retention design.md D1. Nil (the default, set by
	// NewRunService) means unlimited concurrency - SetMaxConcurrentRuns
	// installs a bounded channel pre-filled with one token per slot.
	slots chan struct{}

	mu     sync.Mutex
	active map[int]activeRun
	done   map[int]chan struct{}

	// allowLocal and defaultMode back CheckLocalAllowed - see
	// safe-production-execution design.md D1. allowLocal mirrors
	// runner.allow_local; defaultMode is the instance's configured
	// runner.mode, used to resolve a project's empty Mode the same way the
	// executor does.
	allowLocal  bool
	defaultMode string
}

// SetLocalExecutionPolicy configures CheckLocalAllowed: allowLocal mirrors
// runner.allow_local, and defaultMode is the instance's runner.mode, used
// to resolve an empty project Mode.
func (s *RunService) SetLocalExecutionPolicy(allowLocal bool, defaultMode string) {
	s.allowLocal = allowLocal
	s.defaultMode = defaultMode
}

// CheckLocalAllowed fails a run before any source is acquired when it would
// execute in `local` mode while runner.allow_local is false - see
// safe-production-execution spec.md "Stored local project does not
// execute". mode is normally a stored project's own Mode, already
// populated by the store; an empty mode (e.g. a project lookup that failed
// best-effort) resolves against the instance's configured default the same
// way modeSelectingExecutor does.
func (s *RunService) CheckLocalAllowed(mode string) error {
	if s.allowLocal {
		return nil
	}
	if domain.ResolveMode(mode, s.defaultMode) != "local" {
		return nil
	}
	return fmt.Errorf("%s", domain.LocalExecutionDisabledMessage())
}

// NewRunService creates a RunService.
func NewRunService(runStore domain.RunStore, pipeline domain.PipelineRunner, taskRunner domain.TaskRunner) *RunService {
	return &RunService{
		runStore:   runStore,
		pipeline:   pipeline,
		taskRunner: taskRunner,
		active:     make(map[int]activeRun),
		done:       make(map[int]chan struct{}),
		runLocks:   newRunLocker(),
	}
}

// ActiveRunIDsForProject returns the sorted IDs of runs currently tracked
// in active that belong to projectName. Project deletion must be refused
// (409) while the result is non-empty rather than cascading those runs'
// rows from under them.
// ActiveRunIDsForProject's error return covers only the store-backed
// remote-job check added by remote-runner-registration design.md D10: the
// in-memory half above never fails. Callers - handleDeleteProject in
// particular - MUST fail closed (refuse the delete) on a non-nil error
// rather than treating it as "no active runs", which the archived
// project-delete-run-guard change's invariant ("never fail open into
// deletion") already established for the in-memory-only check this extends.
func (s *RunService) ActiveRunIDsForProject(projectName string) ([]int, error) {
	s.mu.Lock()
	ids := make(map[int]struct{})
	for id, entry := range s.active {
		if entry.project == projectName {
			ids[id] = struct{}{}
		}
	}
	s.mu.Unlock()

	if s.remoteJobStore != nil {
		remoteIDs, err := s.remoteJobStore.OutstandingJobRunIDsForProject(projectName)
		if err != nil {
			return nil, fmt.Errorf("check outstanding remote jobs for %q: %w", projectName, err)
		}
		for _, id := range remoteIDs {
			ids[id] = struct{}{}
		}
	}

	out := make([]int, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Ints(out)
	return out, nil
}

// Start creates a run and executes the pipeline in the background.
// Returns the run ID immediately; execution happens asynchronously.
func (s *RunService) Start(ctx context.Context, projectName string, proj *model.Project, changed []string, opts domain.RunnerOptions) (int, error) {
	if opts.Mode == "remote" && s.remoteJobStore != nil {
		// No in-process execution at all for a remote-mode project: no
		// goroutine, no active/done entry, no concurrency slot - see
		// design.md D10 and tasks.md 2.3/2.4.
		return s.startRemote(projectName, proj, changed, opts)
	}
	if s.pipeline == nil {
		return 0, fmt.Errorf("pipeline runner not configured")
	}
	runID, err := s.createRun(projectName, changed, opts.IncludeManual, proj, opts.Actor)
	if err != nil {
		return 0, err
	}
	startedAt := time.Now()
	slog.Info("run started", "run_id", runID, "project", projectName, "account", opts.Actor)
	output := &bytes.Buffer{}
	redactor := s.prepareRunExecution(runID, output, &opts)

	runCtx, cancel := context.WithCancel(ctx)
	doneCh := make(chan struct{})
	s.mu.Lock()
	s.active[runID] = activeRun{cancel: cancel, project: projectName}
	s.done[runID] = doneCh
	s.mu.Unlock()

	// Decide and record queued-vs-running synchronously, before returning
	// runID, so a caller that immediately queries the run never races the
	// background goroutine's own scheduling; see run-concurrency-and-retention
	// design.md D1/D2. The blocking half (waitAndStart) runs in the goroutine
	// below since it may wait indefinitely. StartTask makes the same
	// synchronous/async split for a single task; acquireSlot composes both
	// halves for the synchronous RunTask path.
	acquired := s.markQueuedIfBusy(runID)

	go func() {
		defer close(doneCh)
		defer cancel()
		defer func() {
			s.mu.Lock()
			delete(s.active, runID)
			delete(s.done, runID)
			s.mu.Unlock()
		}()
		if !acquired && !s.waitAndStart(runCtx, runID) {
			return
		}
		defer s.releaseSlot()
		s.executeRun(runCtx, runID, proj, changed, opts, output, redactor, startedAt)
	}()
	return runID, nil
}

// recordRunFinished records one run's terminal status and duration on
// s.Metrics, a no-op when Metrics is nil (a RunService built without one,
// as most tests do) - see observability design.md D2.
func (s *RunService) recordRunFinished(status, project string, startedAt time.Time) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RunFinished(status, project, time.Since(startedAt))
}

// recordCanceledMetric records the canceled-run metric only when changed is
// true - i.e. this call's CancelRun was the one that actually performed the
// running/queued → canceled transition, per its own atomic status-guarded
// UPDATE. Project and start time are read fresh via GetRun rather than
// pre-checked before CancelRun, since neither changes across cancellation;
// this avoids the check-then-act race a pre-check would reintroduce - see
// REVIEW.md R2.
func (s *RunService) recordCanceledMetric(changed bool, runID int) {
	if !changed || s.Metrics == nil || s.runStore == nil {
		return
	}
	run, err := s.runStore.GetRun(runID)
	if err != nil || run == nil {
		return
	}
	s.recordRunFinished("canceled", run.ProjectName, run.StartedAt)
}

// persistCanceledByIfLost records canceled_by for a run that just
// transitioned to "canceled" via finishRun/runTaskInternal's own store call
// rather than through CancelRun - CancelRun's status-guarded UPDATE (R2) has
// already lost the race and no-ops in that case, so nothing else would ever
// persist who canceled it. A no-op unless Cancel recorded a canceling actor
// on this run's active entry before canceling its context; see REVIEW.md R9.
// Callers only reach this with s.runStore already known non-nil.
func (s *RunService) persistCanceledByIfLost(runID int) {
	s.mu.Lock()
	canceledBy := s.active[runID].canceledBy
	s.mu.Unlock()
	if canceledBy == "" {
		return
	}
	_ = s.runStore.SetCanceledBy(runID, canceledBy)
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
	entry, hasEntry := s.active[runID]
	doneCh, hasDone := s.done[runID]
	if hasEntry {
		entry.canceledBy = actor
		s.active[runID] = entry
	}
	s.mu.Unlock()

	if !hasEntry || !hasDone {
		if s.runStore == nil {
			return fmt.Errorf("runs not supported")
		}
		// A remote-mode run never registers an active/done entry (design.md
		// D10), so it always takes this branch - no mode-conditional is
		// added here. Marking its remaining pending jobs unclaimable is
		// harmless (a no-op) for a run with no remote_jobs rows at all;
		// this call never waits for a claimed/running job, since no stop
		// signal exists (D8) - see tasks.md 4.3.
		if s.remoteJobStore != nil {
			if err := s.remoteJobStore.CancelJobsForRun(runID); err != nil {
				return fmt.Errorf("cancel remote jobs for run %d: %w", runID, err)
			}
		}
		changed, err := s.runStore.CancelRun(runID, actor)
		if err != nil {
			return err
		}
		slog.Info("run canceled", "run_id", runID, "account", actor)
		s.recordCanceledMetric(changed, runID)
		return nil
	}

	entry.cancel()
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
		// runStore is guaranteed non-nil here. doneCh only closes after
		// executeRun/runTaskInternal (and, on the path that actually ran
		// something, its own metric recording) has fully returned - see
		// Start's defer order - so by this point a run that executed has
		// already had its terminal status, including any metric, recorded
		// once. CancelRun's own status-guarded UPDATE is what keeps this
		// from recording a second one: for a run that reached execution,
		// its status here is already terminal (not "running"/"queued"), so
		// CancelRun performs no transition and changed is false; for a run
		// canceled while still queued - which never reached execution, so
		// nothing else ever recorded it - CancelRun does transition it, so
		// this does record. See REVIEW.md R1/R2.
		changed, err := s.runStore.CancelRun(runID, actor)
		if err != nil {
			return err
		}
		slog.Info("run canceled", "run_id", runID, "account", actor)
		s.recordCanceledMetric(changed, runID)
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
	runID, err := s.runStore.CreateRun(projectName, changed, "", includeManual, proj, startedBy)
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
	bounded := newBoundedWriter(output, s.MaxOutputBytes)
	redactor := newRedactingWriter(bounded, opts.ResolvedSecretValues)
	opts.Stdout = redactor
	opts.Stderr = redactor
	opts.OnTaskStart = func(name string) {
		if s.runStore != nil {
			_ = s.runStore.AppendTaskResult(runID, domain.TaskResult{Name: name, Status: "running"})
		}
	}
	opts.OnTaskComplete = func(tr domain.TaskResult) {
		tr.Output = truncateOutput(redactValues(tr.Output, opts.ResolvedSecretValues), s.MaxOutputBytes)
		if s.runStore != nil {
			if err := s.runStore.AppendTaskResult(runID, tr); err != nil {
				slog.Warn("could not append task result", "run_id", runID, "err", redact.Err(err, opts.ResolvedSecretValues))
			}
			if err := s.runStore.FlushRunOutput(runID, output.String()); err != nil {
				slog.Warn("could not flush run output", "run_id", runID, "err", redact.Err(err, opts.ResolvedSecretValues))
			}
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

func (s *RunService) executeRun(ctx context.Context, runID int, proj *model.Project, changed []string, opts domain.RunnerOptions, output *bytes.Buffer, redactor *redactingWriter, startedAt time.Time) {
	defer func() {
		if r := recover(); r != nil && s.runStore != nil {
			_ = redactor.Flush()
			_ = s.runStore.FinishRun(runID, "failed", output.String(), fmt.Sprintf("panic: %v", r), nil)
			s.recordRunFinished("failed", proj.Name, startedAt)
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
	taskResults = redactTaskResults(taskResults, opts.ResolvedSecretValues, s.MaxOutputBytes)
	s.finishRun(runID, proj.Name, output.String(), taskResults, err, opts.SourceSessions, startedAt, opts.ResolvedSecretValues)
}

// redactTaskResults returns a copy of results with each Output field
// redacted (see design.md D5) and bounded to maxOutputBytes (see REVIEW.md
// R1); results itself is left unmodified.
func redactTaskResults(results []domain.TaskResult, values []string, maxOutputBytes int) []domain.TaskResult {
	if len(results) == 0 {
		return results
	}
	out := make([]domain.TaskResult, len(results))
	for i, tr := range results {
		tr.Output = truncateOutput(redactValues(tr.Output, values), maxOutputBytes)
		out[i] = tr
	}
	return out
}

func (s *RunService) finishRun(runID int, projectName string, output string, taskResults []domain.TaskResult, runErr error, sourceSessions domain.SourceSessionManager, startedAt time.Time, secrets []string) {
	status, errMsg := runStatus(taskResults, runErr)
	if s.runStore != nil {
		if err := s.runStore.FinishRun(runID, status, output, errMsg, taskResults); err != nil {
			slog.Warn("could not finish run record", "run_id", runID, "err", redact.Err(err, secrets))
		}
		if status == "canceled" {
			s.persistCanceledByIfLost(runID)
		}
	}
	// runStatus can return "running" (a manual task is still pending) - not
	// a terminal status, so it does not count toward kestrix_runs_total; see
	// observability spec.md "Run outcome counted once".
	if status != "running" {
		s.recordRunFinished(status, projectName, startedAt)
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

// trackActive registers runID in active/done so Cancel terminates the
// run's executing task, returning a derived context and a cleanup func the
// caller must defer. The entry records projectName alongside the cancel func
// so ActiveRunIDsForProject can scope by project without a store round trip.
// Cleanup removes this call's entry (only if a concurrent
// call for the same runID has not already replaced it), cancels the
// context, then closes done — the same order Start's goroutine defers rely
// on. When runStore is nil there is nothing Cancel could record into, so no
// registration happens and cleanup is a no-op; this preserves Cancel's
// invariant that an active entry implies a non-nil runStore.
func (s *RunService) trackActive(ctx context.Context, runID int, projectName string) (context.Context, func()) {
	if s.runStore == nil {
		return ctx, func() {}
	}
	runCtx, cancel := context.WithCancel(ctx)
	doneCh := make(chan struct{})
	s.mu.Lock()
	s.active[runID] = activeRun{cancel: cancel, project: projectName}
	s.done[runID] = doneCh
	s.mu.Unlock()
	return runCtx, func() {
		// Delete only when this call's entry is still current: a second
		// concurrent RunTask/StartTask call for the same runID
		// overwrites these maps, and an unconditional delete here would
		// remove the still-running call's entry so Cancel misses it.
		// Channels are comparable, unlike the CancelFunc.
		s.mu.Lock()
		if cur, ok := s.done[runID]; ok && cur == doneCh {
			delete(s.active, runID)
			delete(s.done, runID)
		}
		s.mu.Unlock()
		cancel()
		close(doneCh)
	}
}

// RunTask executes a single task synchronously and appends the result to an
// existing run; ApproveManualTask runs an approved manual task through it.
// It registers the run in active/done like Start so Cancel terminates the
// task's process (via the runner's process-group wiring) instead of only
// rewriting the run's status in the store; see cancel-kills-task-process
// design.md D1/D2. The single-task re-run route uses StartTask instead.
//
// dispatched is true when opts.Mode is "remote" and this call handed
// taskName to a runner instead of executing it - see design.md D10. A
// caller that finalizes the run from this method's return (as
// finishStartedTask does for StartTask's path) MUST NOT do so when
// dispatched is true: the run stays open for the runner's own completion
// report to finalize instead.
func (s *RunService) RunTask(ctx context.Context, runID int, projectName string, proj *model.Project, taskName string, opts domain.RunnerOptions) (tr domain.TaskResult, dispatched bool, err error) {
	if proj == nil {
		return domain.TaskResult{}, false, fmt.Errorf("project not loaded")
	}
	if opts.Mode == "remote" && s.remoteJobStore != nil {
		return s.dispatchSingleTask(runID, proj, taskName, opts)
	}
	if s.taskRunner == nil {
		return domain.TaskResult{}, false, fmt.Errorf("task runner not configured")
	}
	runCtx, untrack := s.trackActive(ctx, runID, projectName)
	defer untrack()
	if !s.acquireSlot(runCtx, runID) {
		return domain.TaskResult{}, false, fmt.Errorf("run %d canceled while queued", runID)
	}
	defer s.releaseSlot()
	tr, err = s.runTaskInternal(runCtx, runID, projectName, proj, taskName, opts)
	return tr, false, err
}

// StartTask is RunTask's asynchronous counterpart, used by the single-task
// re-run route: it tracks runID, decides queued-vs-running synchronously
// through markQueuedIfBusy - the same accepting half Start uses - and
// returns before the task executes, leaving the rest to executeStartedTask
// in a goroutine. It reports whether the run was recorded as queued; see
// ui-single-task-rerun design.md D1.
func (s *RunService) StartTask(ctx context.Context, runID int, projectName string, proj *model.Project, taskName string, opts domain.RunnerOptions) (queued bool, err error) {
	if proj == nil {
		return false, fmt.Errorf("project not loaded")
	}
	if opts.Mode == "remote" && s.remoteJobStore != nil {
		// Dispatched synchronously, like Start's own remote branch
		// (design.md D10): no active/done entry, no concurrency slot, no
		// goroutine - there is nothing here for executeStartedTask to do.
		_, _, err := s.dispatchSingleTask(runID, proj, taskName, opts)
		return false, err
	}
	if s.taskRunner == nil {
		return false, fmt.Errorf("task runner not configured")
	}
	runCtx, untrack := s.trackActive(ctx, runID, projectName)
	acquired := s.markQueuedIfBusy(runID)
	go s.executeStartedTask(runCtx, untrack, acquired, runID, projectName, proj, taskName, opts)
	return !acquired, nil
}

// executeStartedTask is StartTask's background half: it waits for a slot
// unless one was already acquired, executes the task, and records the run's
// terminal state. A named method rather than a closure so tests can drive
// it directly; see ui-single-task-rerun design.md D2 and Risks.
func (s *RunService) executeStartedTask(ctx context.Context, untrack func(), acquired bool, runID int, projectName string, proj *model.Project, taskName string, opts domain.RunnerOptions) {
	defer untrack()
	if !acquired && !s.waitAndStart(ctx, runID) {
		// Canceled while queued: record nothing here. Cancel waits for this
		// method to return and then makes the canceled transition itself,
		// with canceled_by and its metric - the same as Start's queued path.
		// runTaskInternal never ran, so neither did its workspace cleanup.
		if opts.WorkspaceCleanup != nil {
			opts.WorkspaceCleanup()
		}
		return
	}
	defer s.releaseSlot()
	tr, err := s.runTaskInternal(ctx, runID, projectName, proj, taskName, opts)
	s.finishStartedTask(runID, tr, err, opts.ResolvedSecretValues)
}

// finishStartedTask persists a started task's verdict as the run's final
// record, deriving status and error through runStatus, as finishRun does.
// Unlike finishRun it never overwrites a run Cancel already recorded
// canceled, and never records a metric or canceled_by: runTaskInternal
// already did both from ApplyTaskResult's status.
func (s *RunService) finishStartedTask(runID int, tr domain.TaskResult, runErr error, secrets []string) {
	if s.runStore == nil {
		return
	}
	if run, _ := s.runStore.GetRun(runID); run != nil && run.Status == "canceled" {
		slog.Info("run: task finished after cancel, keeping canceled status", "run_id", runID)
		return
	}
	status, errMsg := runStatus([]domain.TaskResult{tr}, runErr)
	if err := s.runStore.FinishRun(runID, status, tr.Output, errMsg, []domain.TaskResult{tr}); err != nil {
		slog.Warn("could not finish run record", "run_id", runID, "err", redact.Err(err, secrets))
	}
}

func (s *RunService) runTaskInternal(ctx context.Context, runID int, projectName string, proj *model.Project, taskName string, opts domain.RunnerOptions) (domain.TaskResult, error) {
	startedAt := time.Now()
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
		_, _ = s.runStore.ApplyTaskResult(runID, domain.TaskResult{Name: taskName, Status: "running", ApprovedBy: approvedBy, ApprovedAt: approvedAt}, "")
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
	// ApproveManualTask - see design.md D5. Bound after redaction, same
	// ordering the streaming path uses - see REVIEW.md R1.
	tr.Output = truncateOutput(redactValues(tr.Output, opts.ResolvedSecretValues), s.MaxOutputBytes)
	var runStatus string
	if s.runStore != nil {
		runStatus, _ = s.runStore.ApplyTaskResult(runID, tr, tr.Output)
		if runStatus == "canceled" {
			s.persistCanceledByIfLost(runID)
		}
	}
	// ApplyTaskResult's returned status tells us, synchronously and without
	// a separate racy GetRun-based guess, whether this call is the one that
	// finalized the run, including a task canceled mid-flight. This is the
	// only place that records the metric for this run's completion:
	// RunTask/ApproveManualTask never call FinishRun, and StartTask's
	// finishStartedTask calls it only to persist the record, never to record
	// a metric.
	if domain.IsTerminalRunStatus(runStatus) {
		s.recordRunFinished(runStatus, projectName, startedAt)
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

	slog.Info("manual task approved", "run_id", runID, "project", projectName, "account", opts.Actor, "task", taskName)
	tr, _, err := s.RunTask(ctx, runID, projectName, proj, taskName, opts)
	return tr, err
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
		slog.Warn("webhook history: record event failed", "repo", event.Repo, "status", event.Status, "err", redact.Err(err, nil))
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
		slog.Warn("secret resolution: no credential resolver configured", "ref", ref)
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
			slog.Warn("secret resolution failed", "ref", ref, "err", redact.Err(err, nil))
			return "", ErrSecretUnresolved
		}
	}
	if cred.Type != credentials.TypeSecret {
		return "", ErrSecretWrongType
	}
	return cred.Value, nil
}
