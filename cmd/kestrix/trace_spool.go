package main

import (
	"context"
	"sync"
)

// DefaultTraceSpoolMaxBytes is the default capacity (10 MiB) for the in-memory trace spool.
const DefaultTraceSpoolMaxBytes = 10 * 1024 * 1024 // 10 MiB

// DefaultTraceTruncationMarker is inserted when oldest buffered chunks are dropped due to reaching spool capacity.
const DefaultTraceTruncationMarker = "[... output truncated due to offline buffer limit ...]\n"

// TraceChunk represents a contiguous slice of task output starting at a specific byte offset.
type TraceChunk struct {
	Offset   int
	Data     []byte
	IsMarker bool
}

// TraceSpool is a thread-safe bounded in-memory FIFO queue for task trace chunks.
// It maintains monotonic sequential byte offset tracking across all chunks
// and applies a truncation policy when buffered bytes exceed maxBytes.
type TraceSpool struct {
	mu               sync.Mutex
	notifyCh         chan struct{}
	maxBytes         int
	chunks           []TraceChunk
	bufferedBytes    int
	nextOffset       int
	truncationMarker string
	truncated        bool
	inFlight         bool
	dead             bool
}

// NewTraceSpool creates a new TraceSpool with the specified capacity limit in bytes.
// If maxBytes <= 0, DefaultTraceSpoolMaxBytes is used.
// An optional initialOffset may be provided (defaults to 0).
func NewTraceSpool(maxBytes int, initialOffset ...int) *TraceSpool {
	if maxBytes <= 0 {
		maxBytes = DefaultTraceSpoolMaxBytes
	}
	offset := 0
	if len(initialOffset) > 0 {
		offset = initialOffset[0]
	}
	return &TraceSpool{
		notifyCh:         make(chan struct{}),
		maxBytes:         maxBytes,
		nextOffset:       offset,
		truncationMarker: DefaultTraceTruncationMarker,
	}
}

// SetTruncationMarker overrides the default truncation marker.
func (s *TraceSpool) SetTruncationMarker(marker string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.truncationMarker = marker
}

// SetOffset updates the monotonic nextOffset. It should only be called when
// the spool is empty to synchronize with external direct pushes.
func (s *TraceSpool) SetOffset(offset int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextOffset = offset
}

// notifyLocked wakes up goroutines waiting on changes to the spool.
// Must be called while holding s.mu.
func (s *TraceSpool) notifyLocked() {
	close(s.notifyCh)
	s.notifyCh = make(chan struct{})
}

// Append adds a chunk of data to the spool, assigning it the current monotonic offset.
// If the buffer exceeds maxBytes, the truncation policy drops oldest buffered chunks
// and inserts the truncation marker while preserving monotonic sequential byte offsets.
func (s *TraceSpool) Append(p []byte) TraceChunk {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(p) == 0 || s.dead {
		return TraceChunk{Offset: s.nextOffset}
	}

	dataCopy := make([]byte, len(p))
	copy(dataCopy, p)

	chunk := TraceChunk{
		Offset: s.nextOffset,
		Data:   dataCopy,
	}
	s.nextOffset += len(dataCopy)
	s.chunks = append(s.chunks, chunk)
	s.bufferedBytes += len(dataCopy)

	if s.bufferedBytes > s.maxBytes {
		s.applyTruncationLocked()
	}

	s.notifyLocked()
	return chunk
}

// Write implements io.Writer by appending data to the spool. It never returns an error.
func (s *TraceSpool) Write(p []byte) (int, error) {
	s.Append(p)
	return len(p), nil
}

// applyTruncationLocked drops oldest buffered chunks and inserts the truncation marker,
// adjusting subsequent chunk offsets so that offsets remain strictly sequential and contiguous.
// If the head chunk is currently in-flight, it is preserved and truncation applies to subsequent chunks.
// Must be called while holding s.mu.
func (s *TraceSpool) applyTruncationLocked() {
	if len(s.chunks) == 0 {
		return
	}

	markerBytes := []byte(s.truncationMarker)
	if len(markerBytes) > s.maxBytes {
		markerBytes = markerBytes[:s.maxBytes]
	}

	// If the head chunk is currently in-flight, protect it from being dropped.
	if s.inFlight {
		head := s.chunks[0]
		if len(s.chunks) == 1 {
			return
		}

		availableBudget := s.maxBytes - len(head.Data) - len(markerBytes)
		if availableBudget < 0 {
			availableBudget = 0
		}

		startIndex := 1
		if len(s.chunks) > 1 && s.chunks[1].IsMarker {
			startIndex = 2
		}

		fitStart := len(s.chunks)
		usedBytes := 0
		for i := len(s.chunks) - 1; i >= startIndex; i-- {
			chunkLen := len(s.chunks[i].Data)
			if usedBytes+chunkLen <= availableBudget {
				usedBytes += chunkLen
				fitStart = i
			} else {
				break
			}
		}

		var keptChunks []TraceChunk
		if fitStart < len(s.chunks) {
			keptChunks = s.chunks[fitStart:]
		} else if availableBudget > 0 && len(s.chunks) > startIndex {
			lastChunk := s.chunks[len(s.chunks)-1]
			trimmedData := lastChunk.Data[len(lastChunk.Data)-availableBudget:]
			keptChunks = []TraceChunk{{
				Data: trimmedData,
			}}
		}

		markerOffset := head.Offset + len(head.Data)
		markerChunk := TraceChunk{
			Offset:   markerOffset,
			Data:     markerBytes,
			IsMarker: true,
		}

		newChunks := make([]TraceChunk, 0, 2+len(keptChunks))
		newChunks = append(newChunks, head, markerChunk)
		currOffset := markerOffset + len(markerBytes)
		totalBytes := len(head.Data) + len(markerBytes)

		for i := range keptChunks {
			keptChunks[i].Offset = currOffset
			currOffset += len(keptChunks[i].Data)
			totalBytes += len(keptChunks[i].Data)
			newChunks = append(newChunks, keptChunks[i])
		}

		s.chunks = newChunks
		s.bufferedBytes = totalBytes
		s.nextOffset = currOffset
		s.truncated = true
		return
	}

	firstOffset := s.chunks[0].Offset

	// If chunks[0] is already the marker, skip it in budget calculation to replace it.
	startIndex := 0
	if len(s.chunks) > 0 && s.chunks[0].IsMarker {
		startIndex = 1
	}

	// Determine how many old chunks to drop from startIndex forward
	// so that markerBytes + sum(remaining) <= s.maxBytes.
	availableBudget := s.maxBytes - len(markerBytes)

	// Work backwards from newest chunks to find which chunks can fit in availableBudget
	fitStart := len(s.chunks)
	usedBytes := 0
	for i := len(s.chunks) - 1; i >= startIndex; i-- {
		chunkLen := len(s.chunks[i].Data)
		if usedBytes+chunkLen <= availableBudget {
			usedBytes += chunkLen
			fitStart = i
		} else {
			break
		}
	}

	var keptChunks []TraceChunk
	if fitStart < len(s.chunks) {
		keptChunks = s.chunks[fitStart:]
	} else if availableBudget > 0 && len(s.chunks) > startIndex {
		// Even the newest chunk didn't fully fit; trim the tail of the newest chunk
		lastChunk := s.chunks[len(s.chunks)-1]
		trimmedData := lastChunk.Data[len(lastChunk.Data)-availableBudget:]
		keptChunks = []TraceChunk{{
			Data: trimmedData,
		}}
	}

	// Build new chunks list: marker first, then kept chunks with recalculated contiguous offsets
	markerChunk := TraceChunk{
		Offset:   firstOffset,
		Data:     markerBytes,
		IsMarker: true,
	}
	newChunks := make([]TraceChunk, 0, 1+len(keptChunks))
	newChunks = append(newChunks, markerChunk)

	currOffset := firstOffset + len(markerBytes)
	totalBytes := len(markerBytes)

	for i := range keptChunks {
		keptChunks[i].Offset = currOffset
		currOffset += len(keptChunks[i].Data)
		totalBytes += len(keptChunks[i].Data)
		newChunks = append(newChunks, keptChunks[i])
	}

	s.chunks = newChunks
	s.bufferedBytes = totalBytes
	s.nextOffset = currOffset
	s.truncated = true
}

// SetInFlight marks whether the head chunk is currently in-flight being sent.
// While in-flight, applyTruncationLocked will not drop the head chunk.
func (s *TraceSpool) SetInFlight(inFlight bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inFlight = inFlight
}

// InFlight returns whether the head chunk is currently marked in-flight.
func (s *TraceSpool) InFlight() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inFlight
}

// Peek returns the oldest chunk in the spool without removing it.
// Returns false if the spool is empty.
func (s *TraceSpool) Peek() (TraceChunk, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.chunks) == 0 {
		return TraceChunk{}, false
	}
	c := s.chunks[0]
	dataCopy := make([]byte, len(c.Data))
	copy(dataCopy, c.Data)
	return TraceChunk{Offset: c.Offset, Data: dataCopy, IsMarker: c.IsMarker}, true
}

// Pop removes and returns the oldest chunk from the spool.
// Returns false if the spool is empty.
func (s *TraceSpool) Pop() (TraceChunk, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.chunks) == 0 {
		return TraceChunk{}, false
	}
	c := s.chunks[0]
	s.chunks = s.chunks[1:]
	s.bufferedBytes -= len(c.Data)
	s.inFlight = false
	if c.IsMarker || len(s.chunks) == 0 {
		s.truncated = false
	}
	s.notifyLocked()
	return c, true
}

// Acknowledge removes the oldest chunk if its offset and length match the arguments.
// This prevents race conditions if truncation modified the front of the queue.
func (s *TraceSpool) Acknowledge(offset, length int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.chunks) == 0 {
		return false
	}
	if s.chunks[0].Offset == offset && len(s.chunks[0].Data) == length {
		c := s.chunks[0]
		s.chunks = s.chunks[1:]
		s.bufferedBytes -= length
		s.inFlight = false
		if c.IsMarker || len(s.chunks) == 0 {
			s.truncated = false
		}
		s.notifyLocked()
		return true
	}
	return false
}

// Clear removes all chunks from the spool and resets buffer counters.
func (s *TraceSpool) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.chunks = nil
	s.bufferedBytes = 0
	s.truncated = false
	s.inFlight = false
	s.notifyLocked()
}

// MarkDead marks the spool as dead (e.g. after HTTP 409 Conflict),
// clearing buffered chunks and preventing any further appends.
func (s *TraceSpool) MarkDead() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.dead = true
	s.chunks = nil
	s.bufferedBytes = 0
	s.truncated = false
	s.inFlight = false
	s.notifyLocked()
}

// IsDead returns true if the spool has been marked dead.
func (s *TraceSpool) IsDead() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dead
}

// Len returns the number of buffered chunks.
func (s *TraceSpool) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.chunks)
}

// BufferedBytes returns the total number of bytes currently buffered in the spool.
func (s *TraceSpool) BufferedBytes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bufferedBytes
}

// MaxBytes returns the spool's configured capacity limit.
func (s *TraceSpool) MaxBytes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxBytes
}

// NextOffset returns the monotonic byte offset that will be assigned to the next append.
func (s *TraceSpool) NextOffset() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextOffset
}

// IsEmpty returns true if there are no chunks buffered.
func (s *TraceSpool) IsEmpty() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.chunks) == 0
}

// Chunks returns a snapshot copy of all currently buffered chunks.
func (s *TraceSpool) Chunks() []TraceChunk {
	s.mu.Lock()
	defer s.mu.Unlock()

	res := make([]TraceChunk, len(s.chunks))
	for i, c := range s.chunks {
		d := make([]byte, len(c.Data))
		copy(d, c.Data)
		res[i] = TraceChunk{Offset: c.Offset, Data: d, IsMarker: c.IsMarker}
	}
	return res
}

// WaitEmpty blocks until all buffered chunks have been acknowledged or the spool is dead,
// or until ctx is done.
func (s *TraceSpool) WaitEmpty(ctx context.Context) error {
	for {
		s.mu.Lock()
		if len(s.chunks) == 0 || s.dead {
			s.mu.Unlock()
			return nil
		}
		ch := s.notifyCh
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		}
	}
}
