package service

import (
	"context"
	"strings"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/model"
)

// echoingPipelineRunner writes a fixed string to opts.Stdout, reports it via
// OnTaskComplete, and returns it as the sole task's Output - so a test can
// assert the redactor caught a secret at every sink: the flushed output, the
// run's final output, and the persisted task result.
type echoingPipelineRunner struct {
	taskName string
	output   string
}

func (r *echoingPipelineRunner) Run(_ context.Context, _ domain.Project, _ map[string]bool, opts domain.RunnerOptions) ([]domain.TaskResult, error) {
	if opts.Stdout != nil {
		_, _ = opts.Stdout.Write([]byte(r.output))
	}
	tr := domain.TaskResult{Name: r.taskName, Status: "passed", Output: r.output}
	if opts.OnTaskComplete != nil {
		opts.OnTaskComplete(tr)
	}
	return []domain.TaskResult{tr}, nil
}

func (r *echoingPipelineRunner) RunTask(_ context.Context, _ domain.Project, taskName string, _ domain.RunnerOptions) (domain.TaskResult, error) {
	return domain.TaskResult{Name: taskName, Status: "passed", Output: r.output}, nil
}

// TestRunService_RedactsSecretFromOutputAndTaskResult pins design.md D5: a
// resolved secret echoed by a task is replaced with "***" in the flushed
// run output, the final persisted run output, and the appended task result.
func TestRunService_RedactsSecretFromOutputAndTaskResult(t *testing.T) {
	secret := "super-secret-runtime-value"
	filler := " — build finished successfully and the pipeline is moving on to the next stage of deployment"
	runner := &echoingPipelineRunner{taskName: "deploy", output: "deploying with " + secret + filler}
	finishCh := make(chan struct{})
	mockRun := &mocks.MockRunStore{CreateRunID: 1, FinishRunCh: finishCh}
	svc := NewRunService(mockRun, runner, runner)

	_, err := svc.Start(context.Background(), "proj", &model.Project{
		Name: "proj", Tasks: []model.Task{{Name: "deploy"}},
	}, nil, domain.RunnerOptions{ResolvedSecretValues: []string{secret}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	mockRun.WaitFinish()

	if len(mockRun.FlushRunCalls) == 0 {
		t.Fatal("expected at least one FlushRunOutput call")
	}
	sawRedactedFlush := false
	for _, flushed := range mockRun.FlushRunCalls {
		if strings.Contains(flushed, secret) {
			t.Fatalf("flushed output leaked the secret: %q", flushed)
		}
		if strings.Contains(flushed, "***") {
			sawRedactedFlush = true
		}
	}
	if !sawRedactedFlush {
		t.Fatal("expected at least one flushed output to contain the *** marker")
	}

	if len(mockRun.FinishRunCalls) != 1 {
		t.Fatalf("FinishRunCalls = %d, want 1", len(mockRun.FinishRunCalls))
	}
	finish := mockRun.FinishRunCalls[0]
	if strings.Contains(finish.Output, secret) {
		t.Fatalf("final run output leaked the secret: %q", finish.Output)
	}
	if !strings.Contains(finish.Output, "***") {
		t.Fatalf("final run output missing the *** marker: %q", finish.Output)
	}
	if len(finish.TaskResults) != 1 {
		t.Fatalf("FinishRun task results = %d, want 1", len(finish.TaskResults))
	}
	if got := finish.TaskResults[0].Output; strings.Contains(got, secret) {
		t.Fatalf("FinishRun task result leaked the secret: %q", got)
	} else if !strings.Contains(got, "***") {
		t.Fatalf("FinishRun task result missing the *** marker: %q", got)
	}

	var appendedFound bool
	for _, tr := range mockRun.AppendTaskCalls {
		if tr.Name != "deploy" || tr.Status != "passed" {
			continue
		}
		appendedFound = true
		if strings.Contains(tr.Output, secret) {
			t.Fatalf("AppendTaskResult leaked the secret: %q", tr.Output)
		}
		if !strings.Contains(tr.Output, "***") {
			t.Fatalf("AppendTaskResult missing the *** marker: %q", tr.Output)
		}
	}
	if !appendedFound {
		t.Fatal("expected an AppendTaskResult call for the deploy task")
	}
}

// TestRunService_RunTask_RedactsApplyTaskResultOutputExtra pins design.md D5
// on the RunTask path shared by both a direct single-task run and an
// approved manual task: ApplyTaskResult's outputExtra argument - the
// persisted extra output - is redacted.
func TestRunService_RunTask_RedactsApplyTaskResultOutputExtra(t *testing.T) {
	secret := "super-secret-runtime-value"
	mockRun := &mocks.MockRunStore{ApplyTaskCh: make(chan struct{})}
	mockRunner := &mocks.MockPipelineRunner{
		TaskResultList: []domain.TaskResult{{Name: "manual", Status: "passed", Output: "deploying with " + secret}},
	}
	svc := NewRunService(mockRun, mockRunner, mockRunner)

	tr, err := svc.RunTask(context.Background(), 7, "proj", &model.Project{Name: "proj", Tasks: []model.Task{{Name: "manual"}}}, "manual",
		domain.RunnerOptions{ResolvedSecretValues: []string{secret}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(tr.Output, secret) {
		t.Fatalf("returned task result leaked the secret: %q", tr.Output)
	}
	if !strings.Contains(tr.Output, "***") {
		t.Fatalf("returned task result missing the *** marker: %q", tr.Output)
	}

	mockRun.WaitApply()
	var found bool
	for _, call := range mockRun.ApplyTaskCalls {
		if call.TaskResult.Name != "manual" {
			continue
		}
		found = true
		if strings.Contains(call.OutputExtra, secret) || strings.Contains(call.TaskResult.Output, secret) {
			t.Fatalf("ApplyTaskResult leaked the secret: extra=%q output=%q", call.OutputExtra, call.TaskResult.Output)
		}
	}
	if !found {
		t.Fatal("expected an ApplyTaskResult call for the manual task")
	}
}

// TestRunService_FlushRedactsSecretAtVeryEndOfOutput pins design.md D5's
// "call Flush() ... before persisting": a secret sitting at the very end of
// a run's output - nothing written after it - is still redacted in the
// final persisted output, not just dropped from a lagging buffer.
func TestRunService_FlushRedactsSecretAtVeryEndOfOutput(t *testing.T) {
	secret := "super-secret-runtime-value"
	runner := &echoingPipelineRunner{taskName: "deploy", output: "deploying with " + secret}
	finishCh := make(chan struct{})
	mockRun := &mocks.MockRunStore{CreateRunID: 1, FinishRunCh: finishCh}
	svc := NewRunService(mockRun, runner, runner)

	_, err := svc.Start(context.Background(), "proj", &model.Project{
		Name: "proj", Tasks: []model.Task{{Name: "deploy"}},
	}, nil, domain.RunnerOptions{ResolvedSecretValues: []string{secret}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	mockRun.WaitFinish()

	if len(mockRun.FinishRunCalls) != 1 {
		t.Fatalf("FinishRunCalls = %d, want 1", len(mockRun.FinishRunCalls))
	}
	finish := mockRun.FinishRunCalls[0]
	if want := "deploying with ***"; finish.Output != want {
		t.Fatalf("final output = %q, want %q (the trailing secret must be flushed and redacted)", finish.Output, want)
	}
}
