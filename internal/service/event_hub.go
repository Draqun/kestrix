package service

import (
	"sync"
)

// Event type constants for SSE streaming.
const (
	EventTypeStatus   = "status"
	EventTypeTask     = "task"
	EventTypeTrace    = "trace"
	EventTypeComplete = "complete"
)

// StatusPayload carries overall run status updates.
type StatusPayload struct {
	Type   string `json:"type,omitempty"`
	RunID  int    `json:"run_id"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// TaskPayload carries individual task state transitions.
type TaskPayload struct {
	Type     string `json:"type,omitempty"`
	TaskName string `json:"task_name"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

// TracePayload carries incremental run log chunks with offsets, attributed
// to the task that produced them (see REVIEW.md R6).
type TracePayload struct {
	Type     string `json:"type,omitempty"`
	TaskName string `json:"task_name,omitempty"`
	Chunk    string `json:"chunk"`
	Offset   int    `json:"offset"`
}

// CompletePayload marks run termination and terminal status.
type CompletePayload struct {
	Type   string `json:"type,omitempty"`
	RunID  int    `json:"run_id"`
	Status string `json:"status"`
}

// RunEvent is an envelope containing an event type and typed data payload.
type RunEvent struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// DefaultEventBufferSize is the bounded buffer size for subscriber channels.
// Every redactor Write becomes its own trace event (see eventHubTraceWriter),
// so a task that prints many small lines in a burst can queue well past a
// tight buffer before the HTTP handler goroutine drains it; a buffer that
// small drops a healthy subscriber as if it were slow (see REVIEW.md R7).
// 64 proved too tight in practice, so the default is wider here.
const DefaultEventBufferSize = 512

// RunEventHub is an in-memory pub/sub broker distributing run lifecycle,
// task transitions, and trace chunks to active SSE listeners.
type RunEventHub struct {
	mu          sync.RWMutex
	subscribers map[int]map[chan RunEvent]struct{}
	bufferSize  int
}

// NewRunEventHub creates a RunEventHub with standard buffer sizing.
func NewRunEventHub() *RunEventHub {
	return NewRunEventHubWithBuffer(DefaultEventBufferSize)
}

// NewRunEventHubWithBuffer creates a RunEventHub with a custom channel buffer.
func NewRunEventHubWithBuffer(bufferSize int) *RunEventHub {
	if bufferSize <= 0 {
		bufferSize = DefaultEventBufferSize
	}
	return &RunEventHub{
		subscribers: make(map[int]map[chan RunEvent]struct{}),
		bufferSize:  bufferSize,
	}
}

// Subscribe registers a new subscriber channel for the given runID.
func (h *RunEventHub) Subscribe(runID int) chan RunEvent {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch := make(chan RunEvent, h.bufferSize)
	subs, ok := h.subscribers[runID]
	if !ok {
		subs = make(map[chan RunEvent]struct{})
		h.subscribers[runID] = subs
	}
	subs[ch] = struct{}{}
	return ch
}

// Unsubscribe removes a subscriber channel for the given runID.
func (h *RunEventHub) Unsubscribe(runID int, ch chan RunEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()

	subs, ok := h.subscribers[runID]
	if !ok {
		return
	}
	delete(subs, ch)
	if len(subs) == 0 {
		delete(h.subscribers, runID)
	}
}

// SubscriberCount returns the current count of active subscribers for runID.
func (h *RunEventHub) SubscriberCount(runID int) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return len(h.subscribers[runID])
}

// Broadcast distributes event to all active subscribers of runID.
// If a subscriber channel is full (slow consumer), the channel is dropped and closed
// to prevent goroutine leaks and avoid blocking execution. h may be nil - every
// publish call site can broadcast unconditionally without its own nil check
// (see REVIEW.md R9).
//
// A run with no subscribers is the common case on every trace write, so the
// no-subscriber check runs under RLock first: only a run someone is actually
// watching pays for the exclusive lock in the loop below (see REVIEW.md R22).
func (h *RunEventHub) Broadcast(runID int, event RunEvent) {
	if h == nil {
		return
	}
	h.mu.RLock()
	_, watched := h.subscribers[runID]
	h.mu.RUnlock()
	if !watched {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// subs may be nil here if every subscriber for runID was dropped (slow
	// consumer or Unsubscribe) between the RLock check above and this Lock;
	// ranging over and measuring a nil map is safe and does nothing, so no
	// separate re-check is needed.
	subs := h.subscribers[runID]
	for ch := range subs {
		select {
		case ch <- event:
		default:
			// Drop slow consumer to avoid blocking control plane.
			delete(subs, ch)
			close(ch)
		}
	}
	if len(subs) == 0 {
		delete(h.subscribers, runID)
	}
}

// BroadcastStatus publishes an overall run status update. Pass "" for
// errMsg when there is no error (see REVIEW.md R23 - this used to be a
// variadic ...string read only at index 0, which let a caller pass extra
// values that were silently ignored).
func (h *RunEventHub) BroadcastStatus(runID int, status, errMsg string) {
	h.Broadcast(runID, RunEvent{
		Type: EventTypeStatus,
		Data: StatusPayload{
			Type:   EventTypeStatus,
			RunID:  runID,
			Status: status,
			Error:  errMsg,
		},
	})
}

// BroadcastTask publishes a task state transition. Pass "" for errMsg when
// there is no error (see REVIEW.md R23).
func (h *RunEventHub) BroadcastTask(runID int, taskName, status, errMsg string) {
	h.Broadcast(runID, RunEvent{
		Type: EventTypeTask,
		Data: TaskPayload{
			Type:     EventTypeTask,
			TaskName: taskName,
			Status:   status,
			Error:    errMsg,
		},
	})
}

// BroadcastTrace publishes a trace chunk append with byte offset, tagged
// with the task it came from.
func (h *RunEventHub) BroadcastTrace(runID int, taskName, chunk string, offset int) {
	h.Broadcast(runID, RunEvent{
		Type: EventTypeTrace,
		Data: TracePayload{
			Type:     EventTypeTrace,
			TaskName: taskName,
			Chunk:    chunk,
			Offset:   offset,
		},
	})
}

// NewCompleteEvent builds the RunEvent BroadcastComplete publishes, so a
// caller that must emit it directly - the SSE handler's immediate-terminal
// shortcut, which sends it to one freshly-subscribed client before any
// broadcast would reach them - does not duplicate the payload construction
// (see REVIEW.md R12).
func NewCompleteEvent(runID int, status string) RunEvent {
	return RunEvent{
		Type: EventTypeComplete,
		Data: CompletePayload{
			Type:   EventTypeComplete,
			RunID:  runID,
			Status: status,
		},
	}
}

// BroadcastComplete publishes a terminal run complete event.
func (h *RunEventHub) BroadcastComplete(runID int, status string) {
	h.Broadcast(runID, NewCompleteEvent(runID, status))
}
