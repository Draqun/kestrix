package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/model"
	"kestrix/internal/runner"
	"kestrix/internal/service"
)

type binaryOutputTestCase struct {
	name     string
	input    string
	expected string
}

var binaryTestCases = []binaryOutputTestCase{
	{name: "clean", input: "clean text", expected: "clean text"},
	{name: "latin1", input: "caf\xe9", expected: "caf\ufffd"},
	{name: "cut_utf8", input: "\xc3", expected: "\ufffd"},
	{name: "nul", input: "a\x00b", expected: "a\ufffdb"},
}

// ── 1.1 Store integration tests for the six writes ───────────────────

func TestPG_BinarySafe_FlushRunOutput(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-bin-flush", "")

	for _, tc := range binaryTestCases {
		t.Run(tc.name, func(t *testing.T) {
			runID := mustCreateRun(t, pg, "proj-bin-flush", nil)
			if err := pg.FlushRunOutput(runID, tc.input); err != nil {
				t.Fatalf("FlushRunOutput failed: %v", err)
			}
			run, err := pg.GetRun(runID)
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if run.Output != tc.expected {
				t.Fatalf("run.Output = %q, want %q", run.Output, tc.expected)
			}
		})
	}
}

func TestPG_BinarySafe_FinishRun(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-bin-finish", "")

	for _, tc := range binaryTestCases {
		t.Run(tc.name, func(t *testing.T) {
			runID := mustCreateRun(t, pg, "proj-bin-finish", nil)
			tr := domain.TaskResult{Name: "task1", Status: "passed", Output: tc.input, Error: tc.input}
			if err := pg.FinishRun(runID, "passed", tc.input, tc.input, []domain.TaskResult{tr}); err != nil {
				t.Fatalf("FinishRun failed: %v", err)
			}
			run, err := pg.GetRun(runID)
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if run.Output != tc.expected {
				t.Errorf("run.Output = %q, want %q", run.Output, tc.expected)
			}
			if run.Error != tc.expected {
				t.Errorf("run.Error = %q, want %q", run.Error, tc.expected)
			}
			if len(run.TaskResults) != 1 {
				t.Fatalf("len(run.TaskResults) = %d, want 1", len(run.TaskResults))
			}
			if run.TaskResults[0].Output != tc.expected {
				t.Errorf("tr.Output = %q, want %q", run.TaskResults[0].Output, tc.expected)
			}
			if run.TaskResults[0].Error != tc.expected {
				t.Errorf("tr.Error = %q, want %q", run.TaskResults[0].Error, tc.expected)
			}
		})
	}
}

func TestPG_BinarySafe_ApplyTaskResult(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-bin-apply", "")

	for _, tc := range binaryTestCases {
		t.Run(tc.name, func(t *testing.T) {
			runID := mustCreateRun(t, pg, "proj-bin-apply", nil)
			tr := domain.TaskResult{Name: "task1", Status: "passed", Output: tc.input, Error: tc.input}
			status, err := pg.ApplyTaskResult(runID, tr, tc.input)
			if err != nil {
				t.Fatalf("ApplyTaskResult failed: %v", err)
			}
			if status != "passed" {
				t.Fatalf("status = %q, want passed", status)
			}
			run, err := pg.GetRun(runID)
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if run.Output != tc.expected {
				t.Errorf("run.Output = %q, want %q", run.Output, tc.expected)
			}
			if len(run.TaskResults) != 1 {
				t.Fatalf("len(run.TaskResults) = %d, want 1", len(run.TaskResults))
			}
			if run.TaskResults[0].Output != tc.expected {
				t.Errorf("tr.Output = %q, want %q", run.TaskResults[0].Output, tc.expected)
			}
			if run.TaskResults[0].Error != tc.expected {
				t.Errorf("tr.Error = %q, want %q", run.TaskResults[0].Error, tc.expected)
			}
		})
	}
}

func TestPG_BinarySafe_AppendTaskResult(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-bin-append", "")

	for _, tc := range binaryTestCases {
		t.Run(tc.name, func(t *testing.T) {
			runID := mustCreateRun(t, pg, "proj-bin-append", nil)
			tr := domain.TaskResult{Name: "task1", Status: "passed", Output: tc.input, Error: tc.input}
			if err := pg.AppendTaskResult(runID, tr); err != nil {
				t.Fatalf("AppendTaskResult failed: %v", err)
			}
			run, err := pg.GetRun(runID)
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if len(run.TaskResults) != 1 {
				t.Fatalf("len(run.TaskResults) = %d, want 1", len(run.TaskResults))
			}
			if run.TaskResults[0].Output != tc.expected {
				t.Errorf("tr.Output = %q, want %q", run.TaskResults[0].Output, tc.expected)
			}
			if run.TaskResults[0].Error != tc.expected {
				t.Errorf("tr.Error = %q, want %q", run.TaskResults[0].Error, tc.expected)
			}
		})
	}
}

func TestPG_BinarySafe_AppendJobTrace(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-bin-trace", "")

	for _, tc := range binaryTestCases {
		t.Run(tc.name, func(t *testing.T) {
			runID := mustRunWithResults(t, pg, "proj-bin-trace", domain.TaskResult{Name: "build", Status: "pending"})
			jobID := mustCreateRemoteJob(t, pg, runID, "build")
			runnerID := mustCreateRunner(t, pg, "r-trace-"+tc.name)
			job, err := pg.ClaimNextJob(runnerID)
			if err != nil || job == nil || job.ID != jobID {
				t.Fatalf("ClaimNextJob = %+v, %v", job, err)
			}

			ok, err := pg.AppendJobTrace(jobID, job.ClaimEpoch, 0, tc.input, 10000, &runnerID)
			if err != nil || !ok {
				t.Fatalf("AppendJobTrace failed: ok=%v, err=%v", ok, err)
			}

			run, err := pg.GetRun(runID)
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if run.Output != tc.expected {
				t.Fatalf("run.Output = %q, want %q", run.Output, tc.expected)
			}
		})
	}
}

func TestPG_BinarySafe_CompleteJob(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-bin-complete", "")

	for _, tc := range binaryTestCases {
		t.Run(tc.name, func(t *testing.T) {
			runID := mustRunWithResults(t, pg, "proj-bin-complete", domain.TaskResult{Name: "build", Status: "pending"})
			jobID := mustCreateRemoteJob(t, pg, runID, "build")
			runnerID := mustCreateRunner(t, pg, "r-complete-"+tc.name)
			job, err := pg.ClaimNextJob(runnerID)
			if err != nil || job == nil || job.ID != jobID {
				t.Fatalf("ClaimNextJob = %+v, %v", job, err)
			}

			ok, err := pg.CompleteJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusPassed, tc.input, tc.input, &runnerID, 10000)
			if err != nil || !ok {
				t.Fatalf("CompleteJob failed: ok=%v, err=%v", ok, err)
			}

			run, err := pg.GetRun(runID)
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if run.Output != tc.expected {
				t.Errorf("run.Output = %q, want %q", run.Output, tc.expected)
			}
			if len(run.TaskResults) < 1 {
				t.Fatalf("run.TaskResults empty")
			}
			last := run.TaskResults[len(run.TaskResults)-1]
			if last.Output != tc.expected {
				t.Errorf("tr.Output = %q, want %q", last.Output, tc.expected)
			}
			if last.Error != tc.expected {
				t.Errorf("tr.Error = %q, want %q", last.Error, tc.expected)
			}
		})
	}
}

// ── 1.2 Service test through RunService.Start ────────────────────────

type binaryOutputPipelineRunner struct {
	output string
}

func (r *binaryOutputPipelineRunner) Run(_ context.Context, _ model.Project, _ map[string]bool, opts domain.RunnerOptions) ([]domain.TaskResult, error) {
	if opts.Stdout != nil {
		_, _ = opts.Stdout.Write([]byte(r.output))
	}
	tr := domain.TaskResult{Name: "build", Status: "passed", Output: r.output}
	if opts.OnTaskComplete != nil {
		opts.OnTaskComplete(tr)
	}
	return []domain.TaskResult{tr}, nil
}

func (r *binaryOutputPipelineRunner) RunTask(_ context.Context, _ model.Project, taskName string, _ domain.RunnerOptions) (domain.TaskResult, error) {
	return domain.TaskResult{Name: taskName, Status: "passed", Output: r.output}, nil
}

func TestPG_BinarySafe_ServiceRunStart(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-bin-svc", "")

	badText := "caf\xe9\x00"
	expected := "caf\ufffd" // adjacent invalid 0xe9 and NUL collapse into one U+FFFD
	runner := &binaryOutputPipelineRunner{output: badText}
	svc := service.NewRunService(pg, runner, runner)

	proj := &model.Project{
		Name:  "proj-bin-svc",
		Tasks: []model.Task{{Name: "build"}},
	}

	runID, err := svc.Start(context.Background(), proj.Name, proj, nil, domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Poll until the run reaches a terminal state or timeout
	deadline := time.Now().Add(5 * time.Second)
	var finalRun *Run
	for time.Now().Before(deadline) {
		r, err := pg.GetRun(runID)
		if err == nil && r != nil && domain.IsTerminalRunStatus(r.Status) {
			finalRun = r
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalRun == nil {
		r, _ := pg.GetRun(runID)
		t.Fatalf("run %d did not reach terminal status; current status: %v, output: %q", runID, r.Status, r.Output)
	}

	if finalRun.Status != "passed" {
		t.Errorf("status = %q, want passed", finalRun.Status)
	}
	if !strings.Contains(finalRun.Output, expected) {
		t.Errorf("output = %q, want containing %q", finalRun.Output, expected)
	}
	if len(finalRun.TaskResults) == 0 || !strings.Contains(finalRun.TaskResults[0].Output, expected) {
		t.Errorf("task results = %+v, want output containing %q", finalRun.TaskResults, expected)
	}
}

// ── 5.2 Local mode project manual scenarios ──────────────────────────

func TestPG_BinarySafe_LocalMode_PrintfInvalidBytes(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-manual-1", "")

	exec := &runner.PipelineExecutor{Mode: "local"}
	svc := service.NewRunService(pg, exec, exec)
	svc.SetLocalExecutionPolicy(true, "local")

	workDir := t.TempDir()
	appDir := filepath.Join(workDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatal(err)
	}

	proj := &model.Project{
		Name: "proj-manual-1",
		Repos: []model.Repo{
			{Name: "app", Path: "app"},
		},
		Tasks: []model.Task{
			{
				Name:    "print-task",
				Trigger: "repo.app.changed",
				Execute: model.ExecuteSpec{
					Repo:   "app",
					Script: `printf 'caf\351\000x\n'`,
				},
			},
		},
	}

	opts := domain.RunnerOptions{
		WorkDir: workDir,
		Mode:    "local",
	}

	runID, err := svc.Start(context.Background(), proj.Name, proj, []string{"app"}, opts)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	var finalRun *Run
	for time.Now().Before(deadline) {
		r, err := pg.GetRun(runID)
		if err == nil && r != nil && domain.IsTerminalRunStatus(r.Status) {
			finalRun = r
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalRun == nil {
		r, _ := pg.GetRun(runID)
		t.Fatalf("run did not finish; status=%v, output=%q", r.Status, r.Output)
	}

	if finalRun.Status != "passed" {
		t.Fatalf("run status = %q, want 'passed'", finalRun.Status)
	}
	wantSub := "caf\ufffdx"
	if !strings.Contains(finalRun.Output, wantSub) {
		t.Fatalf("run output = %q, want containing %q", finalRun.Output, wantSub)
	}
}

func TestPG_BinarySafe_LocalMode_MaxOutputBytesTruncateMultiByte(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-manual-2", "")

	exec := &runner.PipelineExecutor{Mode: "local"}
	svc := service.NewRunService(pg, exec, exec)
	svc.SetLocalExecutionPolicy(true, "local")
	svc.MaxOutputBytes = 1000

	workDir := t.TempDir()
	appDir := filepath.Join(workDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatal(err)
	}

	proj := &model.Project{
		Name: "proj-manual-2",
		Repos: []model.Repo{
			{Name: "app", Path: "app"},
		},
		Tasks: []model.Task{
			{
				Name:    "yes-task",
				Trigger: "repo.app.changed",
				Execute: model.ExecuteSpec{
					Repo:   "app",
					Script: `yes é | head -n 2000`,
				},
			},
		},
	}

	opts := domain.RunnerOptions{
		WorkDir: workDir,
		Mode:    "local",
	}

	runID, err := svc.Start(context.Background(), proj.Name, proj, []string{"app"}, opts)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	var finalRun *Run
	for time.Now().Before(deadline) {
		r, err := pg.GetRun(runID)
		if err == nil && r != nil && domain.IsTerminalRunStatus(r.Status) {
			finalRun = r
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalRun == nil {
		r, _ := pg.GetRun(runID)
		t.Fatalf("run did not finish; status=%v, output=%q", r.Status, r.Output)
	}

	if finalRun.Status != "passed" {
		t.Fatalf("run status = %q, want 'passed'", finalRun.Status)
	}
	marker := strings.TrimSpace(domain.TruncationMarker(1000))
	if !strings.HasSuffix(strings.TrimSpace(finalRun.Output), marker) {
		t.Fatalf("run output did not end with truncation marker %q; output: %q", marker, finalRun.Output)
	}
}
