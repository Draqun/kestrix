package mocks

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/model"
	"kestrix/internal/source"
)

// MockProjectStore records calls and returns configurable values.
type MockProjectStore struct {
	mu                   sync.Mutex
	UpsertCalls          []*domain.ProjectConfig
	UpsertErr            error
	GetCalls             []string
	GetResult            *domain.ProjectConfig
	GetErr               error
	FindByGitLabIDResult *domain.ProjectConfig
	FindByGitLabIDErr    error
	DeleteCalls          []string
	DeleteErr            error
	ListResult           []string
	ListErr              error
	LoadResult           map[string]*domain.ProjectConfig
	LoadErr              error
	Projects             map[string]*domain.ProjectConfig
	ReplaceSourcesCalls  []ReplaceSourcesCall
	ReplaceSourcesErr    error
	FindBySourceKeyErr   error
}

// ReplaceSourcesCall records one ReplaceSources invocation.
type ReplaceSourcesCall struct {
	Name    string
	Sources []domain.ProjectSource
}

func (m *MockProjectStore) Load() (map[string]*domain.ProjectConfig, error) {
	if m.LoadResult != nil {
		return m.LoadResult, m.LoadErr
	}
	if m.Projects == nil {
		m.Projects = make(map[string]*domain.ProjectConfig)
	}
	return m.Projects, m.LoadErr
}
func (m *MockProjectStore) Save(map[string]*domain.ProjectConfig) error { return nil }
func (m *MockProjectStore) Get(name string) (*domain.ProjectConfig, error) {
	m.mu.Lock()
	m.GetCalls = append(m.GetCalls, name)
	m.mu.Unlock()
	if m.GetErr != nil {
		return nil, m.GetErr
	}
	if m.GetResult != nil {
		return m.GetResult, nil
	}
	return m.Projects[name], nil
}
func (m *MockProjectStore) FindByGitLabID(id int) (*domain.ProjectConfig, error) {
	if m.FindByGitLabIDErr != nil {
		return nil, m.FindByGitLabIDErr
	}
	if m.FindByGitLabIDResult != nil {
		return m.FindByGitLabIDResult, nil
	}
	for _, project := range m.Projects {
		if project.GitLabProjectID == id {
			return project, nil
		}
	}
	return nil, nil
}
func (m *MockProjectStore) Upsert(cfg *domain.ProjectConfig) error {
	m.mu.Lock()
	m.UpsertCalls = append(m.UpsertCalls, cfg)
	m.mu.Unlock()
	if m.Projects == nil {
		m.Projects = make(map[string]*domain.ProjectConfig)
	}
	m.Projects[cfg.Name] = cfg
	return m.UpsertErr
}
func (m *MockProjectStore) Delete(name string) error {
	m.mu.Lock()
	m.DeleteCalls = append(m.DeleteCalls, name)
	m.mu.Unlock()
	if m.DeleteErr != nil {
		return m.DeleteErr
	}
	if m.Projects != nil {
		if _, ok := m.Projects[name]; !ok {
			return fmt.Errorf("project %q not found", name)
		}
	}
	delete(m.Projects, name)
	return nil
}

// UpsertWithSources models the store's single transaction: the project row and
// the source set are both applied, or neither is when an error is configured.
func (m *MockProjectStore) UpsertWithSources(cfg *domain.ProjectConfig, sources []domain.ProjectSource) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.UpsertCalls = append(m.UpsertCalls, cfg)
	m.ReplaceSourcesCalls = append(m.ReplaceSourcesCalls, ReplaceSourcesCall{Name: cfg.Name, Sources: sources})
	if m.UpsertErr != nil {
		return m.UpsertErr
	}
	if m.ReplaceSourcesErr != nil {
		return m.ReplaceSourcesErr
	}
	if m.Projects == nil {
		m.Projects = make(map[string]*domain.ProjectConfig)
	}
	cfg.Sources = sources
	m.Projects[cfg.Name] = cfg
	return nil
}

// ReplaceSources records the call and, when the project is known, stores the
// set on it so Get and FindBySourceKey see what the store would.
func (m *MockProjectStore) ReplaceSources(name string, sources []domain.ProjectSource) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ReplaceSourcesCalls = append(m.ReplaceSourcesCalls, ReplaceSourcesCall{Name: name, Sources: sources})
	if m.ReplaceSourcesErr != nil {
		return m.ReplaceSourcesErr
	}
	if p, ok := m.Projects[name]; ok {
		p.Sources = sources
	}
	return nil
}

// FindBySourceKey matches keys against each stored project's Sources, using
// the same match key the real store computes, ordered by project name.
func (m *MockProjectStore) FindBySourceKey(keys []string) ([]*domain.ProjectConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FindBySourceKeyErr != nil {
		return nil, m.FindBySourceKeyErr
	}
	wanted := make(map[string]bool, len(keys))
	for _, k := range keys {
		wanted[k] = true
	}
	var matched []*domain.ProjectConfig
	for _, p := range m.Projects {
		for _, src := range p.Sources {
			if key, err := source.SourceMatchKey(src.URL); err == nil && wanted[key] {
				matched = append(matched, p)
				break
			}
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].Name < matched[j].Name })
	return matched, nil
}

func (m *MockProjectStore) List() ([]string, error) {
	if m.ListResult != nil {
		return m.ListResult, m.ListErr
	}
	projects := make([]string, 0, len(m.Projects))
	for name := range m.Projects {
		projects = append(projects, name)
	}
	return projects, m.ListErr
}

// MockRunStore records calls for RunService tests.
type MockRunStore struct {
	// MockRemoteJobStore and MockRunnerStore are embedded so MockRunStore
	// satisfies domain.RemoteJobStore and domain.RunnerStore too, mirroring
	// PGStore's real shape (one adapter implementing every port over the
	// same database) - see remote-runner-registration.
	MockRemoteJobStore
	MockRunnerStore

	mu             sync.Mutex
	CreateRunCalls []createRunCall
	CreateRunID    int
	CreateRunErr   error
	FinishRunCalls []finishRunCall
	FinishRunErr   error
	// FinishRunIfDoneCalls records the run IDs FinishRunIfDone was called
	// with, in call order.
	FinishRunIfDoneCalls []int
	FinishRunIfDoneErr   error
	FinishRunCh          chan struct{} // closed once after the first FinishRun call
	finishOnce           sync.Once     // protects FinishRunCh from double-close
	AppendTaskCalls      []domain.TaskResult
	AppendTaskErr        error
	FlushRunCalls        []string
	FlushRunErr          error
	CancelRunCalls       []cancelRunCall
	CancelRunErr         error
	// CancelRunNoChange makes CancelRun report changed=false (as if a
	// concurrent caller, or an already-terminal run, beat this call to the
	// transition) instead of the default changed=true.
	CancelRunNoChange     bool
	ApplyTaskCalls        []applyTaskCall
	ApplyTaskCh           chan struct{} // closed once after the first ApplyTaskResult call
	applyOnce             sync.Once     // protects ApplyTaskCh from double-close
	ApplyTaskErr          error
	GetRunResult          *domain.Run
	GetRunErr             error
	CountRunsResult       int
	CountRunsErr          error
	ListRunsResult        []domain.Run
	ListRunsErr           error
	ListAllRunsResult     []domain.Run
	ListAllRunsErr        error
	CountByStatusResult   map[string]int
	CountByStatusErr      error
	UpdateSourcesCalls    [][]domain.AcquiredSource
	UpdateSourcesErr      error
	FailInterruptedCalls  []string
	FailInterruptedResult int
	FailInterruptedErr    error
	SetRunQueuedCalls     []int
	SetRunQueuedErr       error
	StartQueuedRunCalls   []int
	StartQueuedRunResult  bool
	StartQueuedRunErr     error
	SetCanceledByCalls    []cancelRunCall
	SetCanceledByErr      error
}

type createRunCall struct {
	ProjectName   string
	Changed       []string
	RerunTask     string
	IncludeManual bool
	Snap          *model.Project
	StartedBy     string
}

type cancelRunCall struct {
	ID         int
	CanceledBy string
}

type finishRunCall struct {
	ID          int
	Status      string
	Output      string
	ErrMsg      string
	TaskResults []domain.TaskResult
}

type applyTaskCall struct {
	ID          int
	TaskResult  domain.TaskResult
	OutputExtra string
}

func (m *MockRunStore) CreateRun(p string, c []string, rerunTask string, im bool, snap *model.Project, startedBy string) (int, error) {
	m.mu.Lock()
	m.CreateRunCalls = append(m.CreateRunCalls, createRunCall{p, c, rerunTask, im, snap, startedBy})
	m.mu.Unlock()
	return m.CreateRunID, m.CreateRunErr
}
func (m *MockRunStore) FinishRun(id int, status, output, errMsg string, trs []domain.TaskResult) error {
	m.mu.Lock()
	m.FinishRunCalls = append(m.FinishRunCalls, finishRunCall{id, status, output, errMsg, trs})
	m.mu.Unlock()
	if m.FinishRunCh != nil {
		m.finishOnce.Do(func() { close(m.FinishRunCh) })
	}
	return m.FinishRunErr
}
func (m *MockRunStore) FinishRunIfDone(runID int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FinishRunIfDoneCalls = append(m.FinishRunIfDoneCalls, runID)
	return m.FinishRunIfDoneErr
}
func (m *MockRunStore) AppendTaskResult(runID int, tr domain.TaskResult) error {
	m.mu.Lock()
	m.AppendTaskCalls = append(m.AppendTaskCalls, tr)
	m.mu.Unlock()
	return m.AppendTaskErr
}
func (m *MockRunStore) FlushRunOutput(runID int, output string) error {
	m.mu.Lock()
	m.FlushRunCalls = append(m.FlushRunCalls, output)
	err := m.FlushRunErr
	m.mu.Unlock()
	return err
}
func (m *MockRunStore) UpdateRunSources(_ int, sources []domain.AcquiredSource) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copySources := append([]domain.AcquiredSource(nil), sources...)
	m.UpdateSourcesCalls = append(m.UpdateSourcesCalls, copySources)
	return m.UpdateSourcesErr
}
func (m *MockRunStore) CancelRun(id int, canceledBy string) (bool, error) {
	m.mu.Lock()
	m.CancelRunCalls = append(m.CancelRunCalls, cancelRunCall{id, canceledBy})
	m.mu.Unlock()
	if m.CancelRunErr != nil {
		return false, m.CancelRunErr
	}
	return !m.CancelRunNoChange, nil
}
func (m *MockRunStore) FailInterruptedRuns(reason string) (int, error) {
	m.mu.Lock()
	m.FailInterruptedCalls = append(m.FailInterruptedCalls, reason)
	m.mu.Unlock()
	return m.FailInterruptedResult, m.FailInterruptedErr
}
func (m *MockRunStore) SetCanceledBy(id int, canceledBy string) error {
	m.mu.Lock()
	m.SetCanceledByCalls = append(m.SetCanceledByCalls, cancelRunCall{id, canceledBy})
	m.mu.Unlock()
	return m.SetCanceledByErr
}
func (m *MockRunStore) ApplyTaskResult(id int, tr domain.TaskResult, outputExtra string) (string, error) {
	m.mu.Lock()
	m.ApplyTaskCalls = append(m.ApplyTaskCalls, applyTaskCall{id, tr, outputExtra})
	m.mu.Unlock()
	if m.ApplyTaskCh != nil {
		m.applyOnce.Do(func() { close(m.ApplyTaskCh) })
	}
	if m.ApplyTaskErr != nil {
		return "", m.ApplyTaskErr
	}
	// Mirrors PGStore.ApplyTaskResult's per-call status computation closely
	// enough for tests: the newest task result's status decides whether the
	// run is now terminal.
	switch tr.Status {
	case "failed":
		return "failed", nil
	case "canceled":
		return "canceled", nil
	case "manual", "pending", "running":
		return "running", nil
	default:
		return "passed", nil
	}
}
func (m *MockRunStore) GetRun(id int) (*domain.Run, error) {
	return m.GetRunResult, m.GetRunErr
}
func (m *MockRunStore) SetRunQueued(id int) error {
	m.mu.Lock()
	m.SetRunQueuedCalls = append(m.SetRunQueuedCalls, id)
	m.mu.Unlock()
	return m.SetRunQueuedErr
}

// SetRunQueuedCallCount returns the number of recorded SetRunQueued calls.
// Synchronized (unlike reading SetRunQueuedCalls directly), so a test can
// poll it to wait for a concurrent RunService goroutine to reach the queued
// state before racing it with a slot release.
func (m *MockRunStore) SetRunQueuedCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.SetRunQueuedCalls)
}
func (m *MockRunStore) StartQueuedRun(id int) (bool, error) {
	m.mu.Lock()
	m.StartQueuedRunCalls = append(m.StartQueuedRunCalls, id)
	m.mu.Unlock()
	return m.StartQueuedRunResult, m.StartQueuedRunErr
}

// WaitFinish blocks until FinishRun has been called at least once.
// After it returns, the caller may safely read FinishRunCalls and
// AppendTaskCalls without racing the background goroutine.
func (m *MockRunStore) WaitFinish() {
	ch := m.FinishRunCh
	if ch != nil {
		<-ch
	}
	// Acquire and release the mutex to establish a happens-before
	// edge with the goroutine that wrote to the slices.
	m.mu.Lock()
	_ = m // keep mutex reference to satisfy staticcheck SA2001
	m.mu.Unlock()
}

// AppendTaskCallCount returns the number of recorded AppendTaskResult calls.
func (m *MockRunStore) AppendTaskCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.AppendTaskCalls)
}

// WaitApply blocks until ApplyTaskResult has been called at least once.
func (m *MockRunStore) WaitApply() {
	ch := m.ApplyTaskCh
	if ch != nil {
		<-ch
	}
	m.mu.Lock()
	_ = m // keep mutex reference to satisfy staticcheck SA2001
	m.mu.Unlock()
}

// ApplyTaskCallCount returns the number of recorded ApplyTaskResult calls.
// Call after WaitApply or WaitFinish to avoid racing the background goroutine.
func (m *MockRunStore) ApplyTaskCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.ApplyTaskCalls)
}

// FirstApplyTaskResult returns the TaskResult from the first ApplyTaskResult call.
// Returns zero value if no calls were recorded.
func (m *MockRunStore) FirstApplyTaskResult() domain.TaskResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.ApplyTaskCalls) == 0 {
		return domain.TaskResult{}
	}
	return m.ApplyTaskCalls[0].TaskResult
}
func (m *MockRunStore) CountRuns(string) (int, error) {
	return m.CountRunsResult, m.CountRunsErr
}
func (m *MockRunStore) ListRuns(string, int, int) ([]domain.Run, error) {
	return m.ListRunsResult, m.ListRunsErr
}
func (m *MockRunStore) ListAllRuns(int) ([]domain.Run, error) {
	return m.ListAllRunsResult, m.ListAllRunsErr
}
func (m *MockRunStore) CountRunsByStatus() (map[string]int, error) {
	return m.CountByStatusResult, m.CountByStatusErr
}

// MockPipelineRunner returns configurable task results.
type MockPipelineRunner struct {
	mu              sync.Mutex
	RunCalls        []runCall
	TaskRunCalls    []taskRunCall
	TaskResultList  []domain.TaskResult
	RunErr          error
	PanicMsg        interface{}
	AcquiredSources []domain.AcquiredSource
}

type runCall struct {
	Project domain.Project
	Changed map[string]bool
	Actor   string
}

type taskRunCall struct {
	Project  domain.Project
	TaskName string
	Actor    string
}

func (m *MockPipelineRunner) Run(ctx context.Context, project domain.Project, changed map[string]bool, opts domain.RunnerOptions) ([]domain.TaskResult, error) {
	m.mu.Lock()
	m.RunCalls = append(m.RunCalls, runCall{project, changed, opts.Actor})
	m.mu.Unlock()
	if m.PanicMsg != nil {
		panic(m.PanicMsg)
	}
	if opts.OnSourcesAcquired != nil {
		if err := opts.OnSourcesAcquired(m.AcquiredSources); err != nil {
			return nil, err
		}
	}
	return m.TaskResultList, m.RunErr
}

func (m *MockPipelineRunner) RunTask(ctx context.Context, project domain.Project, taskName string, opts domain.RunnerOptions) (domain.TaskResult, error) {
	m.mu.Lock()
	m.TaskRunCalls = append(m.TaskRunCalls, taskRunCall{Project: project, TaskName: taskName, Actor: opts.Actor})
	m.mu.Unlock()
	if m.PanicMsg != nil {
		panic(m.PanicMsg)
	}
	if opts.OnSourcesAcquired != nil && !opts.SourcesPrepared {
		if err := opts.OnSourcesAcquired(m.AcquiredSources); err != nil {
			return domain.TaskResult{}, err
		}
	}
	if len(m.TaskResultList) > 0 {
		return m.TaskResultList[0], m.RunErr
	}
	return domain.TaskResult{Name: taskName, Status: "passed"}, m.RunErr
}

// MockWebhookLogSink records webhook events in memory and serves them back.
// It implements both domain.WebhookLogSink and domain.WebhookLogStore so unit
// tests can exercise the write and read paths against one store.
// RecordErr, ListErr, and CountErr force the corresponding failure path.
type MockWebhookLogSink struct {
	mu        sync.Mutex
	Entries   []domain.WebhookEvent
	RecordErr error
	ListErr   error
	CountErr  error
}

// Record stores an event, stamping a receive time and id when unset.
func (m *MockWebhookLogSink) Record(event domain.WebhookEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.RecordErr != nil {
		return m.RecordErr
	}
	if event.ReceivedAt.IsZero() {
		event.ReceivedAt = time.Now().UTC()
	}
	event.ID = len(m.Entries) + 1
	m.Entries = append(m.Entries, event)
	return nil
}

// List returns the recorded events in insertion order, oldest first.
func (m *MockWebhookLogSink) List() []domain.WebhookEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]domain.WebhookEvent, len(m.Entries))
	copy(out, m.Entries)
	return out
}

// matching applies the filter to the recorded events, newest first.
func (m *MockWebhookLogSink) matching(filter domain.WebhookEventFilter) []domain.WebhookEvent {
	var out []domain.WebhookEvent
	for i := len(m.Entries) - 1; i >= 0; i-- {
		e := m.Entries[i]
		if filter.ProjectName != "" && e.ProjectName != filter.ProjectName {
			continue
		}
		if filter.Status != "" && e.Status != filter.Status {
			continue
		}
		if filter.Since != nil && e.ReceivedAt.Before(*filter.Since) {
			continue
		}
		if filter.Until != nil && e.ReceivedAt.After(*filter.Until) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (m *MockWebhookLogSink) ListWebhookEvents(filter domain.WebhookEventFilter, limit, offset int) ([]domain.WebhookEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ListErr != nil {
		return nil, m.ListErr
	}
	matched := m.matching(filter)
	if offset < 0 {
		offset = 0
	}
	if offset >= len(matched) {
		return nil, nil
	}
	end := len(matched)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	out := make([]domain.WebhookEvent, end-offset)
	copy(out, matched[offset:end])
	return out, nil
}

func (m *MockWebhookLogSink) CountWebhookEvents(filter domain.WebhookEventFilter) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.CountErr != nil {
		return 0, m.CountErr
	}
	return len(m.matching(filter)), nil
}

// MockRemoteJobStore records calls and returns configurable values for
// domain.RemoteJobStore; see remote-runner-registration.
type MockRemoteJobStore struct {
	mu sync.Mutex

	CreateRemoteJobCalls []CreateRemoteJobCall
	CreateRemoteJobErr   error
	// CreateRemoteJobErrFor fails the dispatch of the named task only, so a
	// test can make one task of a batch fail while the others succeed.
	CreateRemoteJobErrFor map[string]error
	nextJobID             int

	ClaimNextJobResult *domain.RemoteJob
	ClaimNextJobErr    error

	Jobs            map[int]*domain.RemoteJob
	GetRemoteJobErr error

	AppendJobTraceOK  bool
	AppendJobTraceErr error

	CompleteJobOK    bool
	CompleteJobErr   error
	CompleteJobCalls []CompleteJobCall

	PendingOrClaimedIDs        []int
	PendingOrClaimedErr        error
	CancelJobsForRunErr        error
	CancelJobsForRunCalls      []int
	ReclaimedCount             int
	ReclaimedFailed            int
	ReclaimErr                 error
	reclaimCalls               int
	OutstandingJobRunIDsResult []int
	OutstandingJobRunIDsErr    error
	DeleteSecretsErr           error
	deleteSecretsCalls         int

	// SecretValues is what RemoteJobSecretValues returns for a job ID.
	SecretValues    map[int][]string
	SecretValuesErr error
	// AppendJobTraceChunks records each AppendJobTrace chunk, as stored.
	AppendJobTraceChunks []string

	snapshots                 map[int]remoteRunSnapshotMock
	SetRemoteRunSnapshotErr   error
	SetRemoteRunSnapshotCalls int
	GetRemoteRunSnapshotErr   error
	GetRemoteRunSnapshotCalls int

	Artifacts          map[string][]byte
	PutArtifactErr     error
	ListArtifactsErr   error
	DeleteArtifactsErr error
}

// CreateRemoteJobCall records one CreateRemoteJob invocation.
type CreateRemoteJobCall struct {
	RunID    int
	TaskName string
	Payload  domain.RemoteJobPayload
}

// CompleteJobCall records one CompleteJob invocation.
type CompleteJobCall struct {
	JobID          int
	Epoch          int
	Status         string
	Output         string
	TaskError      string
	CallerRunnerID *int
	MaxOutputBytes int
}

func (m *MockRemoteJobStore) CreateRemoteJob(runID int, taskName string, payload domain.RemoteJobPayload) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CreateRemoteJobCalls = append(m.CreateRemoteJobCalls, CreateRemoteJobCall{RunID: runID, TaskName: taskName, Payload: payload})
	if err, ok := m.CreateRemoteJobErrFor[taskName]; ok {
		return 0, err
	}
	if m.CreateRemoteJobErr != nil {
		return 0, m.CreateRemoteJobErr
	}
	m.nextJobID++
	return m.nextJobID, nil
}

func (m *MockRemoteJobStore) ClaimNextJob(runnerID int) (*domain.RemoteJob, error) {
	return m.ClaimNextJobResult, m.ClaimNextJobErr
}

func (m *MockRemoteJobStore) GetRemoteJob(jobID int) (*domain.RemoteJob, error) {
	if m.GetRemoteJobErr != nil {
		return nil, m.GetRemoteJobErr
	}
	if m.Jobs == nil {
		return nil, nil
	}
	return m.Jobs[jobID], nil
}

func (m *MockRemoteJobStore) AppendJobTrace(jobID, epoch, offset int, chunk string, maxOutputBytes int, callerRunnerID *int) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AppendJobTraceChunks = append(m.AppendJobTraceChunks, chunk)
	return m.AppendJobTraceOK, m.AppendJobTraceErr
}

func (m *MockRemoteJobStore) RemoteJobSecretValues(jobID int) ([]string, error) {
	return m.SecretValues[jobID], m.SecretValuesErr
}

func (m *MockRemoteJobStore) CompleteJob(jobID, epoch int, status, output, taskError string, callerRunnerID *int, maxOutputBytes int) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CompleteJobCalls = append(m.CompleteJobCalls, CompleteJobCall{JobID: jobID, Epoch: epoch, Status: status, Output: output, TaskError: taskError, CallerRunnerID: callerRunnerID, MaxOutputBytes: maxOutputBytes})
	return m.CompleteJobOK, m.CompleteJobErr
}

func (m *MockRemoteJobStore) PendingOrClaimedJobIDsForRun(runID int) ([]int, error) {
	return m.PendingOrClaimedIDs, m.PendingOrClaimedErr
}

func (m *MockRemoteJobStore) CancelJobsForRun(runID int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CancelJobsForRunCalls = append(m.CancelJobsForRunCalls, runID)
	return m.CancelJobsForRunErr
}

func (m *MockRemoteJobStore) ReclaimStaleJobs(olderThan time.Time, maxReclaims int, reason string) (int, int, error) {
	m.mu.Lock()
	m.reclaimCalls++
	m.mu.Unlock()
	return m.ReclaimedCount, m.ReclaimedFailed, m.ReclaimErr
}

func (m *MockRemoteJobStore) DeleteTerminalRemoteSecrets() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteSecretsCalls++
	return m.DeleteSecretsErr
}

func (m *MockRemoteJobStore) PutRemoteArtifact(runID int, taskName string, archive []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.PutArtifactErr != nil {
		return m.PutArtifactErr
	}
	if m.Artifacts == nil {
		m.Artifacts = make(map[string][]byte)
	}
	key := fmt.Sprintf("%d:%s", runID, taskName)
	m.Artifacts[key] = archive
	return nil
}

func (m *MockRemoteJobStore) ListRemoteArtifactsByTasks(runID int, taskNames []string) (map[string][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ListArtifactsErr != nil {
		return nil, m.ListArtifactsErr
	}
	out := make(map[string][]byte)
	for _, taskName := range taskNames {
		key := fmt.Sprintf("%d:%s", runID, taskName)
		if arch, ok := m.Artifacts[key]; ok {
			out[taskName] = arch
		}
	}
	return out, nil
}

func (m *MockRemoteJobStore) DeleteTerminalRemoteArtifacts() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.DeleteArtifactsErr != nil {
		return m.DeleteArtifactsErr
	}
	return nil
}

// DeleteSecretsCallCount returns the number of DeleteTerminalRemoteSecrets
// calls, synchronized for polling from another goroutine.
func (m *MockRemoteJobStore) DeleteSecretsCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deleteSecretsCalls
}

// ReclaimCallCount returns the number of recorded ReclaimStaleJobs calls,
// synchronized so a test can poll it from another goroutine (a background
// reclaim loop).
func (m *MockRemoteJobStore) ReclaimCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reclaimCalls
}

// CompleteJobCallCount returns the number of recorded CompleteJob calls,
// synchronized so a test can poll it from another goroutine (for example a
// background runPollLoop) without racing CompleteJob's own append to
// CompleteJobCalls.
func (m *MockRemoteJobStore) CompleteJobCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.CompleteJobCalls)
}

func (m *MockRemoteJobStore) OutstandingJobRunIDsForProject(projectName string) ([]int, error) {
	return m.OutstandingJobRunIDsResult, m.OutstandingJobRunIDsErr
}

func (m *MockRemoteJobStore) SetRemoteRunSnapshot(runID int, archive []byte, variables map[string]map[string]string, secretValues []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.snapshots == nil {
		m.snapshots = make(map[int]remoteRunSnapshotMock)
	}
	m.snapshots[runID] = remoteRunSnapshotMock{archive: archive, variables: variables, secretValues: secretValues}
	m.SetRemoteRunSnapshotCalls++
	return m.SetRemoteRunSnapshotErr
}

func (m *MockRemoteJobStore) GetRemoteRunSnapshot(runID int) ([]byte, map[string]map[string]string, []string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GetRemoteRunSnapshotCalls++
	if m.GetRemoteRunSnapshotErr != nil {
		return nil, nil, nil, m.GetRemoteRunSnapshotErr
	}
	snap, ok := m.snapshots[runID]
	if !ok {
		return nil, nil, nil, nil
	}
	return snap.archive, snap.variables, snap.secretValues, nil
}

// remoteRunSnapshotMock is MockRemoteJobStore's in-memory stand-in for the
// runs.remote_snapshot column.
type remoteRunSnapshotMock struct {
	archive      []byte
	variables    map[string]map[string]string
	secretValues []string
}

var _ domain.RemoteJobStore = (*MockRemoteJobStore)(nil)

// MockRunnerStore records calls and returns configurable values for
// domain.RunnerStore; see remote-runner-registration.
type MockRunnerStore struct {
	mu sync.Mutex

	CreateRunnerID    int
	CreateRunnerToken string
	CreateRunnerErr   error
	// CreateRunnerTags records the tags of the last CreateRunner call.
	CreateRunnerTags []string

	ListRunnersResult []domain.Runner
	ListRunnersErr    error

	AuthenticateRunnerID  int
	AuthenticateRunnerOK  bool
	AuthenticateRunnerErr error

	RevokeRunnerErr   error
	RevokeRunnerCalls []int
}

func (m *MockRunnerStore) CreateRunner(name string, tags []string) (int, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CreateRunnerTags = tags
	return m.CreateRunnerID, m.CreateRunnerToken, m.CreateRunnerErr
}

func (m *MockRunnerStore) ListRunners() ([]domain.Runner, error) {
	return m.ListRunnersResult, m.ListRunnersErr
}

func (m *MockRunnerStore) AuthenticateRunner(token string) (int, bool, error) {
	return m.AuthenticateRunnerID, m.AuthenticateRunnerOK, m.AuthenticateRunnerErr
}

func (m *MockRunnerStore) RevokeRunner(id int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RevokeRunnerCalls = append(m.RevokeRunnerCalls, id)
	return m.RevokeRunnerErr
}

var _ domain.RunnerStore = (*MockRunnerStore)(nil)
