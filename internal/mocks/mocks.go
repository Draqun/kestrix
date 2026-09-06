package mocks

import (
	"context"
	"fmt"
	"sync"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/model"
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
	mu                  sync.Mutex
	CreateRunCalls      []createRunCall
	CreateRunID         int
	CreateRunErr        error
	FinishRunCalls      []finishRunCall
	FinishRunErr        error
	FinishRunCh         chan struct{} // closed once after the first FinishRun call
	finishOnce          sync.Once     // protects FinishRunCh from double-close
	AppendTaskCalls     []domain.TaskResult
	AppendTaskErr       error
	FlushRunCalls       []string
	CancelRunCalls      []int
	CancelRunErr        error
	ApplyTaskCalls      []applyTaskCall
	ApplyTaskCh         chan struct{} // closed once after the first ApplyTaskResult call
	applyOnce           sync.Once     // protects ApplyTaskCh from double-close
	ApplyTaskErr        error
	GetRunResult        *domain.Run
	GetRunErr           error
	CountRunsResult     int
	CountRunsErr        error
	ListRunsResult      []domain.Run
	ListRunsErr         error
	ListAllRunsResult   []domain.Run
	ListAllRunsErr      error
	CountByStatusResult map[string]int
	CountByStatusErr    error
	UpdateSourcesCalls  [][]domain.AcquiredSource
	UpdateSourcesErr    error
}

type createRunCall struct {
	ProjectName   string
	Changed       []string
	IncludeManual bool
	Snap          *model.Project
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

func (m *MockRunStore) CreateRun(p string, c []string, im bool, snap *model.Project) (int, error) {
	m.mu.Lock()
	m.CreateRunCalls = append(m.CreateRunCalls, createRunCall{p, c, im, snap})
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
func (m *MockRunStore) AppendTaskResult(runID int, tr domain.TaskResult) error {
	m.mu.Lock()
	m.AppendTaskCalls = append(m.AppendTaskCalls, tr)
	m.mu.Unlock()
	return m.AppendTaskErr
}
func (m *MockRunStore) FlushRunOutput(runID int, output string) error {
	m.mu.Lock()
	m.FlushRunCalls = append(m.FlushRunCalls, output)
	m.mu.Unlock()
	return nil
}
func (m *MockRunStore) UpdateRunSources(_ int, sources []domain.AcquiredSource) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copySources := append([]domain.AcquiredSource(nil), sources...)
	m.UpdateSourcesCalls = append(m.UpdateSourcesCalls, copySources)
	return m.UpdateSourcesErr
}
func (m *MockRunStore) CancelRun(id int) error {
	m.mu.Lock()
	m.CancelRunCalls = append(m.CancelRunCalls, id)
	m.mu.Unlock()
	return m.CancelRunErr
}
func (m *MockRunStore) ApplyTaskResult(id int, tr domain.TaskResult, outputExtra string) error {
	m.mu.Lock()
	m.ApplyTaskCalls = append(m.ApplyTaskCalls, applyTaskCall{id, tr, outputExtra})
	m.mu.Unlock()
	if m.ApplyTaskCh != nil {
		m.applyOnce.Do(func() { close(m.ApplyTaskCh) })
	}
	return m.ApplyTaskErr
}
func (m *MockRunStore) GetRun(id int) (*domain.Run, error) {
	return m.GetRunResult, m.GetRunErr
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
}

type taskRunCall struct {
	Project  domain.Project
	TaskName string
}

func (m *MockPipelineRunner) Run(ctx context.Context, project domain.Project, changed map[string]bool, opts domain.RunnerOptions) ([]domain.TaskResult, error) {
	m.mu.Lock()
	m.RunCalls = append(m.RunCalls, runCall{project, changed})
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
	m.TaskRunCalls = append(m.TaskRunCalls, taskRunCall{Project: project, TaskName: taskName})
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
