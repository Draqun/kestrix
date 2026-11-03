package service

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
)

func TestRunService_EventHub_TaskAndTraceEmissions(t *testing.T) {
	hub := NewRunEventHub()
	runStore := &mocks.MockRunStore{}
	pipeline := &mocks.MockPipelineRunner{}
	svc := NewRunService(runStore, pipeline, pipeline)
	svc.SetEventHub(hub)
	if svc.EventHub() != hub {
		t.Fatal("EventHub() mismatch")
	}

	runID := 10
	sub := hub.Subscribe(runID)
	defer hub.Unsubscribe(runID, sub)

	out := &bytes.Buffer{}
	opts := domain.RunnerOptions{}
	redactor := svc.prepareRunExecution(runID, out, &opts)

	// Verify task start
	opts.OnTaskStart("build")
	select {
	case ev := <-sub:
		if ev.Type != EventTypeTask {
			t.Fatalf("ev.Type = %q, want task", ev.Type)
		}
		p := ev.Data.(TaskPayload)
		if p.TaskName != "build" || p.Status != "running" {
			t.Fatalf("task payload = %+v, want build/running", p)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for task start event")
	}

	// Verify trace output
	_, _ = opts.Stdout.Write([]byte("compiling step 1\n"))
	_ = redactor.Flush()
	select {
	case ev := <-sub:
		if ev.Type != EventTypeTrace {
			t.Fatalf("ev.Type = %q, want trace", ev.Type)
		}
		p := ev.Data.(TracePayload)
		if p.Chunk != "compiling step 1\n" || p.Offset != 0 {
			t.Fatalf("trace payload = %+v, want 'compiling step 1\\n' offset 0", p)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for trace event")
	}

	// Verify second trace output offset increases
	_, _ = opts.Stdout.Write([]byte("done\n"))
	_ = redactor.Flush()
	select {
	case ev := <-sub:
		if ev.Type != EventTypeTrace {
			t.Fatalf("ev.Type = %q, want trace", ev.Type)
		}
		p := ev.Data.(TracePayload)
		if p.Chunk != "done\n" || p.Offset != len("compiling step 1\n") {
			t.Fatalf("trace payload = %+v, want 'done\\n' offset %d", p, len("compiling step 1\n"))
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for second trace event")
	}

	// Verify task complete
	opts.OnTaskComplete(domain.TaskResult{Name: "build", Status: domain.TaskStatusPassed})
	select {
	case ev := <-sub:
		if ev.Type != EventTypeTask {
			t.Fatalf("ev.Type = %q, want task", ev.Type)
		}
		p := ev.Data.(TaskPayload)
		if p.TaskName != "build" || p.Status != domain.TaskStatusPassed {
			t.Fatalf("task payload = %+v, want build/passed", p)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for task complete event")
	}

	// Verify finishRun emits status and complete
	svc.finishRun(runID, "test-proj", out.String(), []domain.TaskResult{
		{Name: "build", Status: domain.TaskStatusPassed},
	}, nil, nil, time.Now(), nil)

	select {
	case ev := <-sub:
		if ev.Type != EventTypeStatus {
			t.Fatalf("ev.Type = %q, want status", ev.Type)
		}
		p := ev.Data.(StatusPayload)
		if p.Status != domain.RunStatusPassed {
			t.Fatalf("status payload = %+v, want passed", p)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for status event")
	}

	select {
	case ev := <-sub:
		if ev.Type != EventTypeComplete {
			t.Fatalf("ev.Type = %q, want complete", ev.Type)
		}
		p := ev.Data.(CompletePayload)
		if p.Status != domain.RunStatusPassed {
			t.Fatalf("complete payload = %+v, want passed", p)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for complete event")
	}
}

// TestRunService_EventHub_TraceRedactsSecretsBeforeBroadcast pins the
// design's Security Considerations claim that secrets are redacted before
// trace events reach SSE subscribers (see REVIEW.md R15): the hub writer
// sits between the redactor and boundedWriter, and a wrong order would leak
// secrets to every viewer.
func TestRunService_EventHub_TraceRedactsSecretsBeforeBroadcast(t *testing.T) {
	hub := NewRunEventHub()
	runStore := &mocks.MockRunStore{}
	pipeline := &mocks.MockPipelineRunner{}
	svc := NewRunService(runStore, pipeline, pipeline)
	svc.SetEventHub(hub)

	runID := 11
	sub := hub.Subscribe(runID)
	defer hub.Unsubscribe(runID, sub)

	const secret = "sup3rs3cr3t"
	out := &bytes.Buffer{}
	opts := domain.RunnerOptions{ResolvedSecretValues: []string{secret}}
	redactor := svc.prepareRunExecution(runID, out, &opts)

	opts.OnTaskStart("build")
	<-sub // drain the task-running event

	_, _ = opts.Stdout.Write([]byte("token=" + secret + " done\n"))
	_ = redactor.Flush()

	// The redactor buffers across writes to catch a secret split by a write
	// boundary, so one Write/Flush pair can broadcast several trace chunks;
	// collect them all before asserting none leaked the secret.
	var joined strings.Builder
	collecting := true
	for collecting {
		select {
		case ev := <-sub:
			if ev.Type != EventTypeTrace {
				t.Fatalf("ev.Type = %q, want trace", ev.Type)
			}
			p := ev.Data.(TracePayload)
			if strings.Contains(p.Chunk, secret) {
				t.Fatalf("trace payload leaked the secret: %+v", p)
			}
			joined.WriteString(p.Chunk)
		case <-time.After(100 * time.Millisecond):
			collecting = false
		}
	}
	if joined.Len() == 0 {
		t.Fatal("no trace events received")
	}
	if !strings.Contains(joined.String(), "***") {
		t.Fatalf("trace output = %q, want the redaction marker", joined.String())
	}
}

func TestRunService_EventHub_CancelEmission(t *testing.T) {
	hub := NewRunEventHub()
	runStore := &mocks.MockRunStore{}
	svc := NewRunService(runStore, nil, nil)
	svc.SetEventHub(hub)

	runID := 99
	sub := hub.Subscribe(runID)
	defer hub.Unsubscribe(runID, sub)

	err := svc.Cancel(context.Background(), runID, "alice")
	if err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}

	select {
	case ev := <-sub:
		if ev.Type != EventTypeStatus {
			t.Fatalf("ev.Type = %q, want status", ev.Type)
		}
		p := ev.Data.(StatusPayload)
		if p.Status != domain.RunStatusCanceled {
			t.Fatalf("status payload = %+v, want canceled", p)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for status event")
	}

	select {
	case ev := <-sub:
		if ev.Type != EventTypeComplete {
			t.Fatalf("ev.Type = %q, want complete", ev.Type)
		}
		p := ev.Data.(CompletePayload)
		if p.Status != domain.RunStatusCanceled {
			t.Fatalf("complete payload = %+v, want canceled", p)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for complete event")
	}
}

func TestRunService_AppendRemoteJobTrace_DedupRetries(t *testing.T) {
	hub := NewRunEventHub()
	runStore := &mocks.MockRunStore{}
	pipeline := &mocks.MockPipelineRunner{}
	runID := 42
	taskName := "build"
	remoteJobs := &mocks.MockRemoteJobStore{
		AppendJobTraceOK:       true,
		AppendJobTraceRunID:    &runID,
		AppendJobTraceTaskName: &taskName,
		Jobs: map[int]*domain.RemoteJob{
			1: {ID: 1, RunID: 42, TaskName: "build"},
		},
	}
	svc := NewRunService(runStore, pipeline, pipeline)
	svc.SetRemoteJobStore(remoteJobs)
	svc.SetEventHub(hub)

	sub := hub.Subscribe(runID)
	defer hub.Unsubscribe(runID, sub)

	runnerID := 7
	// 1. Initial chunk at offset 0 succeeds and appends 5 bytes
	ok, err := svc.AppendRemoteJobTrace(1, 1, 0, "hello", &runnerID)
	if err != nil || !ok {
		t.Fatalf("first append failed: ok=%v, err=%v", ok, err)
	}

	select {
	case ev := <-sub:
		if ev.Type != EventTypeTrace {
			t.Fatalf("ev.Type = %q, want trace", ev.Type)
		}
		p, ok := ev.Data.(TracePayload)
		if !ok || p.TaskName != "build" || p.Chunk != "hello" || p.Offset != 0 {
			t.Fatalf("payload = %+v, want build/'hello'/0", ev.Data)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for trace event")
	}

	// 2. Retried chunk at offset 0: mock reports appended = 0
	zero := 0
	remoteJobs.AppendJobTraceAppended = &zero
	ok, err = svc.AppendRemoteJobTrace(1, 1, 0, "hello", &runnerID)
	if err != nil || !ok {
		t.Fatalf("retried append failed: ok=%v, err=%v", ok, err)
	}

	// Assert no duplicate event was broadcast on retry
	select {
	case ev := <-sub:
		t.Fatalf("unexpected event broadcast on retried trace append: %+v", ev)
	case <-time.After(100 * time.Millisecond):
		// Expected: nothing broadcast
	}

	// 3. Partial chunk: mock reports appended = 3 (due to bounds)
	three := 3
	remoteJobs.AppendJobTraceAppended = &three
	ok, err = svc.AppendRemoteJobTrace(1, 1, 5, "world", &runnerID)
	if err != nil || !ok {
		t.Fatalf("partial append failed: ok=%v, err=%v", ok, err)
	}

	select {
	case ev := <-sub:
		if ev.Type != EventTypeTrace {
			t.Fatalf("ev.Type = %q, want trace", ev.Type)
		}
		p, ok := ev.Data.(TracePayload)
		if !ok || p.TaskName != "build" || p.Chunk != "wor" || p.Offset != 5 {
			t.Fatalf("payload = %+v, want build/'wor'/5", ev.Data)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for partial trace event")
	}
}

func TestRunService_ClaimNextJob_EmitsTaskRunningEvent(t *testing.T) {
	hub := NewRunEventHub()
	runStore := &mocks.MockRunStore{}
	pipeline := &mocks.MockPipelineRunner{}
	remoteJobs := &mocks.MockRemoteJobStore{
		ClaimNextJobResult: &domain.RemoteJob{
			ID:       1,
			RunID:    55,
			TaskName: "deploy",
			Status:   domain.RemoteJobStatusClaimed,
		},
	}
	svc := NewRunService(runStore, pipeline, pipeline)
	svc.SetRemoteJobStore(remoteJobs)
	svc.SetEventHub(hub)

	runID := 55
	sub := hub.Subscribe(runID)
	defer hub.Unsubscribe(runID, sub)

	runnerID := 9
	job, err := svc.ClaimNextJob(runnerID)
	if err != nil || job == nil {
		t.Fatalf("ClaimNextJob failed: job=%+v, err=%v", job, err)
	}

	select {
	case ev := <-sub:
		if ev.Type != EventTypeTask {
			t.Fatalf("ev.Type = %q, want task", ev.Type)
		}
		p, ok := ev.Data.(TaskPayload)
		if !ok || p.TaskName != "deploy" || p.Status != domain.TaskStatusRunning {
			t.Fatalf("payload = %+v, want deploy/running", ev.Data)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for task running event")
	}
}

func TestRunService_ClaimNextJob_Errors(t *testing.T) {
	runStore := &mocks.MockRunStore{}
	pipeline := &mocks.MockPipelineRunner{}

	// Store nil
	svc := NewRunService(runStore, pipeline, pipeline)
	if _, err := svc.ClaimNextJob(1); err == nil {
		t.Fatal("expected error when remoteJobStore is nil")
	}

	// Store returns error
	errStore := errors.New("db fail")
	remoteJobs := &mocks.MockRemoteJobStore{ClaimNextJobErr: errStore}
	svc.SetRemoteJobStore(remoteJobs)
	if _, err := svc.ClaimNextJob(1); !errors.Is(err, errStore) {
		t.Fatalf("expected db fail, got %v", err)
	}
}

func TestRunService_AppendRemoteJobTrace_Errors(t *testing.T) {
	runStore := &mocks.MockRunStore{}
	pipeline := &mocks.MockPipelineRunner{}

	// Store nil
	svc := NewRunService(runStore, pipeline, pipeline)
	if _, err := svc.AppendRemoteJobTrace(1, 1, 0, "x", nil); err == nil {
		t.Fatal("expected error when remoteJobStore is nil")
	}

	// Store returns error
	errStore := errors.New("db fail")
	remoteJobs := &mocks.MockRemoteJobStore{AppendJobTraceErr: errStore}
	svc.SetRemoteJobStore(remoteJobs)
	if _, err := svc.AppendRemoteJobTrace(1, 1, 0, "x", nil); !errors.Is(err, errStore) {
		t.Fatalf("expected db fail, got %v", err)
	}

	// Store returns ok=false (stale epoch)
	remoteJobs.AppendJobTraceErr = nil
	remoteJobs.AppendJobTraceOK = false
	ok, err := svc.AppendRemoteJobTrace(1, 1, 0, "x", nil)
	if err != nil || ok {
		t.Fatalf("expected ok=false, err=nil, got ok=%v, err=%v", ok, err)
	}
}
