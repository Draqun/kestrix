package domain

import (
	"context"
	"sync"

	"gitlab.com/lunaris-bot/metaci/internal/model"
)

// MockProjectStore records calls and returns configurable values.
type MockProjectStore struct {
	mu          sync.Mutex
	UpsertCalls []*ProjectConfig
	UpsertErr   error
	GetCalls    []string
	GetResult   *ProjectConfig
	GetErr      error
	DeleteCalls []string
	DeleteErr   error
	ListResult  []string
	ListErr     error
	LoadResult  map[string]*ProjectConfig
	LoadErr     error
}

func (m *MockProjectStore) Load() (map[string]*ProjectConfig, error) {
	return m.LoadResult, m.LoadErr
}
func (m *MockProjectStore) Save(map[string]*ProjectConfig) error { return nil }
func (m *MockProjectStore) Get(name string) (*ProjectConfig, error) {
	m.mu.Lock()
	m.GetCalls = append(m.GetCalls, name)
	m.mu.Unlock()
	return m.GetResult, m.GetErr
}
func (m *MockProjectStore) FindByGitLabID(int) (*ProjectConfig, error) {
	return nil, nil
}
func (m *MockProjectStore) Upsert(cfg *ProjectConfig) error {
	m.mu.Lock()
	m.UpsertCalls = append(m.UpsertCalls, cfg)
	m.mu.Unlock()
	return m.UpsertErr
}
func (m *MockProjectStore) Delete(name string) error {
	m.mu.Lock()
	m.DeleteCalls = append(m.DeleteCalls, name)
	m.mu.Unlock()
	return m.DeleteErr
}
func (m *MockProjectStore) List() ([]string, error) {
	return m.ListResult, m.ListErr
}

// MockRunStore records calls for RunService tests.
type MockRunStore struct {
	mu               sync.Mutex
	CreateRunCalls   []createRunCall
	CreateRunID      int
	CreateRunErr     error
	FinishRunCalls   []finishRunCall
	FinishRunErr     error
	AppendTaskCalls  []TaskResult
	AppendTaskErr    error
	FlushRunCalls    []string
	CancelRunCalls   []int
	ApplyTaskCalls   []applyTaskCall
	ApplyTaskErr     error
	GetRunResult     *Run
	GetRunErr        error
	CountRunsResult  int
	CountRunsErr     error
	ListRunsResult   []Run
	ListRunsErr      error
	ListAllRunsResult []Run
	ListAllRunsErr   error
	CountByStatusResult map[string]int
	CountByStatusErr    error
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
	TaskResults []TaskResult
}

type applyTaskCall struct {
	ID          int
	TaskResult  TaskResult
	OutputExtra string
}

func (m *MockRunStore) CreateRun(p string, c []string, im bool, snap *model.Project) (int, error) {
	m.mu.Lock()
	m.CreateRunCalls = append(m.CreateRunCalls, createRunCall{p, c, im, snap})
	m.mu.Unlock()
	return m.CreateRunID, m.CreateRunErr
}
func (m *MockRunStore) FinishRun(id int, status, output, errMsg string, trs []TaskResult) error {
	m.mu.Lock()
	m.FinishRunCalls = append(m.FinishRunCalls, finishRunCall{id, status, output, errMsg, trs})
	m.mu.Unlock()
	return m.FinishRunErr
}
func (m *MockRunStore) AppendTaskResult(runID int, tr TaskResult) error {
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
func (m *MockRunStore) CancelRun(id int) error {
	m.mu.Lock()
	m.CancelRunCalls = append(m.CancelRunCalls, id)
	m.mu.Unlock()
	return nil
}
func (m *MockRunStore) ApplyTaskResult(id int, tr TaskResult, outputExtra string) error {
	m.mu.Lock()
	m.ApplyTaskCalls = append(m.ApplyTaskCalls, applyTaskCall{id, tr, outputExtra})
	m.mu.Unlock()
	return m.ApplyTaskErr
}
func (m *MockRunStore) GetRun(id int) (*Run, error)   { return m.GetRunResult, m.GetRunErr }
func (m *MockRunStore) CountRuns(string) (int, error) { return m.CountRunsResult, m.CountRunsErr }
func (m *MockRunStore) ListRuns(string, int, int) ([]Run, error) {
	return m.ListRunsResult, m.ListRunsErr
}
func (m *MockRunStore) ListAllRuns(int) ([]Run, error) { return m.ListAllRunsResult, m.ListAllRunsErr }
func (m *MockRunStore) CountRunsByStatus() (map[string]int, error) {
	return m.CountByStatusResult, m.CountByStatusErr
}

// MockPipelineRunner returns configurable task results.
type MockPipelineRunner struct {
	mu             sync.Mutex
	RunCalls       []runCall
	TaskResultList []TaskResult
	RunErr         error
	PanicMsg       interface{}
}

type runCall struct {
	Project model.Project
	Changed map[string]bool
}

func (m *MockPipelineRunner) Run(ctx context.Context, project model.Project, changed map[string]bool, opts RunnerOptions) ([]TaskResult, error) {
	m.mu.Lock()
	m.RunCalls = append(m.RunCalls, runCall{project, changed})
	m.mu.Unlock()
	if m.PanicMsg != nil {
		panic(m.PanicMsg)
	}
	return m.TaskResultList, m.RunErr
}

// MockWebhookLogSink records appended entries.
type MockWebhookLogSink struct {
	mu       sync.Mutex
	Entries  []map[string]interface{}
}

func (m *MockWebhookLogSink) Append(entry map[string]interface{}) {
	m.mu.Lock()
	m.Entries = append(m.Entries, entry)
	m.mu.Unlock()
}
func (m *MockWebhookLogSink) List() []map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]map[string]interface{}, len(m.Entries))
	copy(out, m.Entries)
	return out
}