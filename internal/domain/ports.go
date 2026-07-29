package domain

import (
	"context"
	"io"
	"time"
)

// ProjectStore is the port for project configuration persistence.
// Implemented by FileStore and PGProjectStore.
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
	WorkDir        string
	IncludeManual  bool
	Variables      VariableResolver
	Stdout         io.Writer
	Stderr         io.Writer
	OnTaskStart    func(name string)
	OnTaskComplete func(TaskResult)
}

// WebhookLogSink is the port for webhook event logging.
type WebhookLogSink interface {
	Append(entry map[string]interface{})
	List() []map[string]interface{}
}

// ProjectConfig is the configuration of a registered project.
// It lives in domain so both ProjectStore and ProjectService can use it
// without importing store.
type ProjectConfig struct {
	Name            string      `json:"name"`
	RepoPath        string      `json:"repo_path"`
	RepoURL         string      `json:"repo_url,omitempty"`
	PipelineDir     string      `json:"pipeline_dir,omitempty"`
	PipelineFile    string      `json:"pipeline_file"`
	GitLabProjectID int         `json:"gitlab_project_id,omitempty"`
	DefaultBranch   string      `json:"default_branch,omitempty"`
	WebhookSecret   string      `json:"webhook_secret,omitempty"`
	CreatedAt       string      `json:"created_at"`
	ExtraRepos      []ExtraRepo `json:"extra_repos,omitempty"`
}

// ExtraRepo is an additional repository participating in a project.
type ExtraRepo struct {
	Name string `json:"name"`
	Path string `json:"path"`
	URL  string `json:"url,omitempty"`
	Kind string `json:"kind,omitempty"`
}

// Run is a stored run record.
type Run struct {
	ID            int          `json:"id"`
	ProjectName   string       `json:"project_name"`
	Status        string       `json:"status"`
	Changed       []string     `json:"changed"`
	IncludeManual bool         `json:"include_manual"`
	PipelineDef   *Project     `json:"pipeline_def,omitempty"`
	Output        string       `json:"output"`
	Error         string       `json:"error,omitempty"`
	TaskResults   []TaskResult `json:"task_results,omitempty"`
	StartedAt     time.Time    `json:"started_at"`
	FinishedAt    *time.Time   `json:"finished_at,omitempty"`
}
