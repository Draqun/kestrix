package service

import (
	"sync"
	"testing"
	"time"
)

func TestRunEventHub_SubscribeAndBroadcast(t *testing.T) {
	hub := NewRunEventHub()
	runID := 42

	sub1 := hub.Subscribe(runID)
	defer hub.Unsubscribe(runID, sub1)
	sub2 := hub.Subscribe(runID)
	defer hub.Unsubscribe(runID, sub2)

	if count := hub.SubscriberCount(runID); count != 2 {
		t.Fatalf("SubscriberCount = %d, want 2", count)
	}

	// Broadcast task
	hub.BroadcastTask(runID, "build", "running", "")

	select {
	case ev := <-sub1:
		if ev.Type != EventTypeTask {
			t.Errorf("sub1 ev.Type = %q, want %q", ev.Type, EventTypeTask)
		}
		task, ok := ev.Data.(TaskPayload)
		if !ok || task.TaskName != "build" || task.Status != "running" {
			t.Errorf("sub1 payload = %+v, want build/running", ev.Data)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("sub1 timeout waiting for task event")
	}

	select {
	case ev := <-sub2:
		if ev.Type != EventTypeTask {
			t.Errorf("sub2 ev.Type = %q, want %q", ev.Type, EventTypeTask)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("sub2 timeout waiting for task event")
	}

	// Broadcast trace
	hub.BroadcastTrace(runID, "build", "line 1\n", 0)
	select {
	case ev := <-sub1:
		if ev.Type != EventTypeTrace {
			t.Errorf("sub1 ev.Type = %q, want %q", ev.Type, EventTypeTrace)
		}
		trace, ok := ev.Data.(TracePayload)
		if !ok || trace.TaskName != "build" || trace.Chunk != "line 1\n" || trace.Offset != 0 {
			t.Errorf("sub1 payload = %+v, want task_name 'build' chunk 'line 1\\n' offset 0", ev.Data)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("sub1 timeout waiting for trace event")
	}

	// Broadcast status
	hub.BroadcastStatus(runID, "failed", "exit code 1")
	select {
	case ev := <-sub1:
		if ev.Type != EventTypeStatus {
			t.Errorf("sub1 ev.Type = %q, want %q", ev.Type, EventTypeStatus)
		}
		status, ok := ev.Data.(StatusPayload)
		if !ok || status.Status != "failed" || status.Error != "exit code 1" {
			t.Errorf("sub1 payload = %+v, want failed / exit code 1", ev.Data)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("sub1 timeout waiting for status event")
	}

	// Broadcast complete
	hub.BroadcastComplete(runID, "failed")
	select {
	case ev := <-sub1:
		if ev.Type != EventTypeComplete {
			t.Errorf("sub1 ev.Type = %q, want %q", ev.Type, EventTypeComplete)
		}
		comp, ok := ev.Data.(CompletePayload)
		if !ok || comp.Status != "failed" || comp.RunID != runID {
			t.Errorf("sub1 payload = %+v, want complete failed runID 42", ev.Data)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("sub1 timeout waiting for complete event")
	}
}

func TestRunEventHub_RunIsolation(t *testing.T) {
	hub := NewRunEventHub()
	runA := 101
	runB := 102

	subA := hub.Subscribe(runA)
	defer hub.Unsubscribe(runA, subA)
	subB := hub.Subscribe(runB)
	defer hub.Unsubscribe(runB, subB)

	hub.BroadcastTask(runA, "lint", "passed", "")

	select {
	case ev := <-subA:
		if ev.Type != EventTypeTask {
			t.Errorf("subA ev.Type = %q", ev.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subA expected event")
	}

	select {
	case ev := <-subB:
		t.Fatalf("subB received unexpected event from runA: %+v", ev)
	default:
		// expected: no event on runB
	}
}

func TestRunEventHub_Unsubscribe(t *testing.T) {
	hub := NewRunEventHub()
	runID := 55

	// Unsubscribe on non-existent runID should not panic
	dummyCh := make(chan RunEvent)
	hub.Unsubscribe(999, dummyCh)

	// Broadcast on non-existent runID should not panic
	hub.Broadcast(999, RunEvent{})

	sub := hub.Subscribe(runID)
	if count := hub.SubscriberCount(runID); count != 1 {
		t.Fatalf("SubscriberCount = %d, want 1", count)
	}

	hub.Unsubscribe(runID, sub)
	if count := hub.SubscriberCount(runID); count != 0 {
		t.Fatalf("SubscriberCount = %d, want 0 after unsubscribe", count)
	}

	// Broadcast after unsubscribe should not panic or block
	hub.BroadcastTask(runID, "test", "passed", "")

	select {
	case ev := <-sub:
		t.Fatalf("sub received event after unsubscribe: %+v", ev)
	default:
		// success
	}
}

func TestRunEventHub_SlowConsumerDropped(t *testing.T) {
	// Zero buffer size should fallback to DefaultEventBufferSize
	defaultHub := NewRunEventHubWithBuffer(0)
	if defaultHub.bufferSize != DefaultEventBufferSize {
		t.Fatalf("bufferSize = %d, want %d", defaultHub.bufferSize, DefaultEventBufferSize)
	}

	// Test dropping sole subscriber cleans up subscriber map entry
	soleHub := NewRunEventHubWithBuffer(1)
	soleSub := soleHub.Subscribe(123)
	soleHub.BroadcastTrace(123, "build", "1", 0)
	soleHub.BroadcastTrace(123, "build", "2", 1) // overflow drops soleSub
	if count := soleHub.SubscriberCount(123); count != 0 {
		t.Fatalf("SubscriberCount = %d, want 0", count)
	}
	<-soleSub // drain 1
	if _, ok := <-soleSub; ok {
		t.Fatal("soleSub should be closed")
	}

	bufferSize := 2
	hub := NewRunEventHubWithBuffer(bufferSize)
	runID := 77

	slowSub := hub.Subscribe(runID)
	fastSub := hub.Subscribe(runID)
	defer hub.Unsubscribe(runID, fastSub)

	// Send bufferSize events (fills buffer)
	hub.BroadcastTrace(runID, "build", "msg 1", 0)
	hub.BroadcastTrace(runID, "build", "msg 2", 5)

	// fastSub reads one event so it won't overflow
	<-fastSub

	// Send 3rd event: slowSub buffer is full and will overflow; fastSub has space
	hub.BroadcastTrace(runID, "build", "msg 3", 10)

	// slowSub should have been closed and removed from subscribers
	if count := hub.SubscriberCount(runID); count != 1 {
		t.Fatalf("SubscriberCount = %d, want 1 (slow consumer should be dropped)", count)
	}

	// slowSub channel should now be closed after draining existing items
	<-slowSub // msg 1
	<-slowSub // msg 2
	_, ok := <-slowSub
	if ok {
		t.Error("slowSub channel should have been closed")
	}
}

// TestRunEventHub_NilReceiverIsSafe verifies every publish call site can
// invoke a nil *RunEventHub unconditionally, with no per-call nil check
// (see REVIEW.md R9).
func TestRunEventHub_NilReceiverIsSafe(t *testing.T) {
	var hub *RunEventHub
	hub.BroadcastStatus(1, "running", "")
	hub.BroadcastTask(1, "build", "running", "")
	hub.BroadcastTrace(1, "build", "chunk", 0)
	hub.BroadcastComplete(1, "passed")
}

func TestRunEventHub_ConcurrentAccess(t *testing.T) {
	hub := NewRunEventHub()
	runID := 99

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sub := hub.Subscribe(runID)
			time.Sleep(5 * time.Millisecond)
			hub.Unsubscribe(runID, sub)
		}()
	}

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			hub.BroadcastTrace(runID, "build", "chunk", idx)
		}(i)
	}

	wg.Wait()
}
