package service

import (
	"bytes"
	"context"
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
