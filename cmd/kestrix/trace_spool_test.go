package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── Task 1.1: Thread-safe in-memory TraceSpool with monotonic offset tracking ──

func TestTraceSpool_MonotonicOffsetTracking(t *testing.T) {
	spool := NewTraceSpool(1024, 100)

	if spool.NextOffset() != 100 {
		t.Fatalf("expected initial offset 100, got %d", spool.NextOffset())
	}

	c1 := spool.Append([]byte("hello "))
	if c1.Offset != 100 || string(c1.Data) != "hello " {
		t.Fatalf("c1: offset=%d data=%q, want 100, hello ", c1.Offset, string(c1.Data))
	}
	if spool.NextOffset() != 106 {
		t.Fatalf("expected NextOffset 106, got %d", spool.NextOffset())
	}

	c2 := spool.Append([]byte("world\n"))
	if c2.Offset != 106 || string(c2.Data) != "world\n" {
		t.Fatalf("c2: offset=%d data=%q, want 106, world\\n", c2.Offset, string(c2.Data))
	}
	if spool.NextOffset() != 112 {
		t.Fatalf("expected NextOffset 112, got %d", spool.NextOffset())
	}

	if spool.Len() != 2 {
		t.Fatalf("expected 2 chunks, got %d", spool.Len())
	}
	if spool.BufferedBytes() != 12 {
		t.Fatalf("expected 12 buffered bytes, got %d", spool.BufferedBytes())
	}
}

func TestTraceSpool_ThreadSafety(t *testing.T) {
	spool := NewTraceSpool(1024 * 1024)
	const numGoroutines = 20
	const numAppends = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < numAppends; i++ {
				spool.Append([]byte(fmt.Sprintf("g%02d-i%02d\n", id, i)))
			}
		}(g)
	}

	wg.Wait()

	totalExpectedBytes := numGoroutines * numAppends * 8
	if spool.BufferedBytes() != totalExpectedBytes {
		t.Fatalf("expected %d buffered bytes, got %d", totalExpectedBytes, spool.BufferedBytes())
	}
	if spool.Len() != numGoroutines*numAppends {
		t.Fatalf("expected %d chunks, got %d", numGoroutines*numAppends, spool.Len())
	}
	if spool.NextOffset() != totalExpectedBytes {
		t.Fatalf("expected NextOffset %d, got %d", totalExpectedBytes, spool.NextOffset())
	}

	// Verify contiguous offsets across all chunks
	chunks := spool.Chunks()
	expectedOffset := 0
	for idx, c := range chunks {
		if c.Offset != expectedOffset {
			t.Fatalf("chunk %d: offset %d != expected %d", idx, c.Offset, expectedOffset)
		}
		expectedOffset += len(c.Data)
	}
}

func TestTraceSpool_SetOffset(t *testing.T) {
	spool := NewTraceSpool(1024, 0)
	spool.SetOffset(250)
	if spool.NextOffset() != 250 {
		t.Fatalf("expected NextOffset 250, got %d", spool.NextOffset())
	}
	c := spool.Append([]byte("hello"))
	if c.Offset != 250 {
		t.Fatalf("expected chunk offset 250, got %d", c.Offset)
	}
}

// ── Task 1.2: Truncation policy when buffered chunks exceed capacity limit ──

func TestTraceSpool_TruncationPolicyAndByteCalculation(t *testing.T) {
	// Custom truncation marker for predictable length calculation in test
	marker := "[... output truncated due to offline buffer limit ...]\n"
	markerLen := len(marker)

	// Set spool capacity to allow marker + 40 bytes
	maxBytes := markerLen + 40
	spool := NewTraceSpool(maxBytes, 0)
	spool.SetTruncationMarker(marker)

	// Append chunks of 20 bytes each
	spool.Append([]byte("chunk-1: 0123456789\n")) // 20 bytes, offset 0..20
	spool.Append([]byte("chunk-2: 0123456789\n")) // 20 bytes, offset 20..40
	spool.Append([]byte("chunk-3: 0123456789\n")) // 20 bytes, offset 40..60

	// At this point: 60 bytes <= maxBytes (markerLen is 55, maxBytes is 95).
	if spool.BufferedBytes() != 60 {
		t.Fatalf("expected 60 buffered bytes before overflow, got %d", spool.BufferedBytes())
	}
	if spool.Len() != 3 {
		t.Fatalf("expected 3 chunks, got %d", spool.Len())
	}

	// Now append chunk-4 and chunk-5, causing total bytes (100) to exceed maxBytes (95)
	spool.Append([]byte("chunk-4: 0123456789\n")) // 20 bytes, offset 60..80
	spool.Append([]byte("chunk-5: 0123456789\n")) // 20 bytes, offset 80..100 -> overflow!

	// Capacity check: buffered bytes MUST NOT exceed maxBytes
	if spool.BufferedBytes() > maxBytes {
		t.Fatalf("buffered bytes %d exceeds maxBytes %d", spool.BufferedBytes(), maxBytes)
	}

	chunks := spool.Chunks()
	if len(chunks) == 0 {
		t.Fatal("expected chunks in spool after truncation, got 0")
	}

	// First chunk MUST be the truncation marker
	if string(chunks[0].Data) != marker {
		t.Fatalf("first chunk data = %q, want marker %q", string(chunks[0].Data), marker)
	}
	if !strings.Contains(string(chunks[0].Data), "[... output truncated due to offline buffer limit ...]") {
		t.Fatalf("first chunk does not contain expected marker phrase")
	}
	if chunks[0].Offset != 0 {
		t.Fatalf("marker chunk offset = %d, want 0", chunks[0].Offset)
	}

	// Verify exact byte calculation: BufferedBytes() matches sum of chunk lengths
	sumBytes := 0
	for _, c := range chunks {
		sumBytes += len(c.Data)
	}
	if spool.BufferedBytes() != sumBytes {
		t.Fatalf("BufferedBytes() = %d does not match sum of chunks %d", spool.BufferedBytes(), sumBytes)
	}

	// Verify contiguous sequential offsets without gaps
	expectedOffset := 0
	for i, c := range chunks {
		if c.Offset != expectedOffset {
			t.Fatalf("chunk %d: offset %d != expected %d", i, c.Offset, expectedOffset)
		}
		expectedOffset += len(c.Data)
	}
	if spool.NextOffset() != expectedOffset {
		t.Fatalf("NextOffset %d != expected %d", spool.NextOffset(), expectedOffset)
	}

	// Latest output MUST be preserved (chunk-5 and chunk-4)
	lastChunk := chunks[len(chunks)-1]
	if string(lastChunk.Data) != "chunk-5: 0123456789\n" {
		t.Fatalf("last chunk data = %q, want chunk-5", string(lastChunk.Data))
	}
}

func TestTraceSpool_RepeatedTruncationMaintainsOffsets(t *testing.T) {
	marker := "[... output truncated due to offline buffer limit ...]\n"
	markerLen := len(marker)
	maxBytes := markerLen + 30
	spool := NewTraceSpool(maxBytes, 50) // starts at offset 50
	spool.SetTruncationMarker(marker)

	// Append multiple chunks causing multiple successive truncation sweeps
	for i := 0; i < 10; i++ {
		spool.Append([]byte(fmt.Sprintf("line-%02d: content\n", i)))
	}

	if spool.BufferedBytes() > maxBytes {
		t.Fatalf("buffered bytes %d exceeds maxBytes %d", spool.BufferedBytes(), maxBytes)
	}

	chunks := spool.Chunks()
	if len(chunks) < 2 {
		t.Fatalf("expected at least marker and one kept chunk, got %d", len(chunks))
	}

	// First chunk is marker starting at initial offset 50
	if chunks[0].Offset != 50 {
		t.Fatalf("marker offset = %d, want 50", chunks[0].Offset)
	}

	// Verify sequential offsets
	curr := 50
	for idx, c := range chunks {
		if c.Offset != curr {
			t.Fatalf("chunk %d: offset %d, want %d", idx, c.Offset, curr)
		}
		curr += len(c.Data)
	}
	if spool.NextOffset() != curr {
		t.Fatalf("NextOffset = %d, want %d", spool.NextOffset(), curr)
	}
}

func TestTraceSpool_RepeatedTruncationAfterMarkerAcknowledged(t *testing.T) {
	marker := "[... output truncated due to offline buffer limit ...]\n"
	markerLen := len(marker)
	maxBytes := markerLen + 40
	spool := NewTraceSpool(maxBytes, 0)
	spool.SetTruncationMarker(marker)

	// Append chunks until truncation occurs (21 * 5 = 105 bytes > maxBytes 95)
	spool.Append([]byte("chunk-01: 0123456789\n")) // 21 bytes
	spool.Append([]byte("chunk-02: 0123456789\n")) // 21 bytes
	spool.Append([]byte("chunk-03: 0123456789\n")) // 21 bytes
	spool.Append([]byte("chunk-04: 0123456789\n")) // 21 bytes
	spool.Append([]byte("chunk-05: 0123456789\n")) // 21 bytes -> exceeds maxBytes (markerLen+40), truncates!

	// Spool must have truncated: chunk[0] is marker
	peek, ok := spool.Peek()
	if !ok || !peek.IsMarker {
		t.Fatalf("expected marker at head of spool after first truncation")
	}

	// Now acknowledge / pop the marker chunk, leaving remaining regular chunks in spool
	acked := spool.Acknowledge(peek.Offset, len(peek.Data))
	if !acked {
		t.Fatalf("failed to acknowledge marker chunk")
	}

	// At this point, head of spool is a regular chunk, NOT a marker
	head, ok := spool.Peek()
	if !ok {
		t.Fatalf("expected remaining chunk after marker ack")
	}
	if head.IsMarker {
		t.Fatalf("expected regular chunk at head, but got marker")
	}

	// Append more chunks to trigger a second truncation sweep
	spool.Append([]byte("chunk-06: 0123456789\n"))
	spool.Append([]byte("chunk-07: 0123456789\n"))
	spool.Append([]byte("chunk-08: 0123456789\n"))
	spool.Append([]byte("chunk-09: 0123456789\n"))
	spool.Append([]byte("chunk-10: 0123456789\n"))

	// Head must again be a marker with IsMarker == true
	head2, ok := spool.Peek()
	if !ok || !head2.IsMarker {
		t.Fatalf("expected new marker at head after second truncation sweep")
	}

	chunks := spool.Chunks()
	// Verify contiguous offsets across all remaining chunks
	expectedOffset := chunks[0].Offset
	for i, c := range chunks {
		if c.Offset != expectedOffset {
			t.Fatalf("chunk %d offset %d != expected %d", i, c.Offset, expectedOffset)
		}
		expectedOffset += len(c.Data)
	}
	if spool.NextOffset() != expectedOffset {
		t.Fatalf("NextOffset %d != expected %d", spool.NextOffset(), expectedOffset)
	}
}

func TestTraceSpool_TruncationWithInFlightChunkPreservesInFlightAndContiguousOffsets(t *testing.T) {
	marker := "[... output truncated due to offline buffer limit ...]\n"
	markerLen := len(marker)
	maxBytes := markerLen + 40
	spool := NewTraceSpool(maxBytes, 0)
	spool.SetTruncationMarker(marker)

	// Append initial chunk (20 bytes)
	c1 := spool.Append([]byte("initial-chunk-01234\n")) // 20 bytes, offset 0..20
	if c1.Offset != 0 {
		t.Fatalf("expected offset 0, got %d", c1.Offset)
	}

	// Mark in-flight (as when flusher is sending it over HTTP)
	spool.SetInFlight(true)
	if !spool.InFlight() {
		t.Fatal("expected InFlight() to be true")
	}

	// Now append a burst of chunks causing buffer overflow
	spool.Append([]byte("burst-chunk-01-0123\n")) // 20 bytes
	spool.Append([]byte("burst-chunk-02-0123\n")) // 20 bytes
	spool.Append([]byte("burst-chunk-03-0123\n")) // 20 bytes
	spool.Append([]byte("burst-chunk-04-0123\n")) // 20 bytes -> overflow!

	chunks := spool.Chunks()
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks (in-flight + marker), got %d", len(chunks))
	}

	// Head chunk MUST be the in-flight chunk, NOT the marker
	if string(chunks[0].Data) != "initial-chunk-01234\n" {
		t.Fatalf("expected head chunk to be in-flight chunk, got %q", string(chunks[0].Data))
	}
	if chunks[0].Offset != 0 {
		t.Fatalf("expected head chunk offset 0, got %d", chunks[0].Offset)
	}

	// Second chunk MUST be the truncation marker immediately following head chunk
	if !chunks[1].IsMarker {
		t.Fatalf("expected second chunk to be marker")
	}
	if chunks[1].Offset != 20 {
		t.Fatalf("expected marker chunk offset 20, got %d", chunks[1].Offset)
	}

	// Acknowledge the in-flight chunk
	acked := spool.Acknowledge(0, 20)
	if !acked {
		t.Fatalf("failed to acknowledge in-flight chunk")
	}
	if spool.InFlight() {
		t.Fatalf("expected InFlight() to be false after acknowledge")
	}

	// Now head chunk is the marker at offset 20
	head, ok := spool.Peek()
	if !ok || !head.IsMarker || head.Offset != 20 {
		t.Fatalf("expected marker at offset 20 at head of spool, got %+v", head)
	}
}

// ── Inspection, Acknowledge, Clear, MarkDead, WaitEmpty ──

func TestTraceSpool_PeekPopAcknowledge(t *testing.T) {
	spool := NewTraceSpool(1024, 0)
	spool.Append([]byte("chunk-1"))
	spool.Append([]byte("chunk-2"))

	// Peek
	c, ok := spool.Peek()
	if !ok || string(c.Data) != "chunk-1" || c.Offset != 0 {
		t.Fatalf("Peek: got (%+v, %v)", c, ok)
	}
	if spool.Len() != 2 {
		t.Fatalf("Peek must not remove chunk, got len %d", spool.Len())
	}

	// Acknowledge wrong offset should fail
	if spool.Acknowledge(99, len("chunk-1")) {
		t.Fatal("Acknowledge with wrong offset should return false")
	}

	// Acknowledge matching chunk-1
	if !spool.Acknowledge(0, len("chunk-1")) {
		t.Fatal("Acknowledge chunk-1 failed")
	}
	if spool.Len() != 1 {
		t.Fatalf("len = %d, want 1", spool.Len())
	}
	if spool.BufferedBytes() != len("chunk-2") {
		t.Fatalf("bufferedBytes = %d, want %d", spool.BufferedBytes(), len("chunk-2"))
	}

	// Pop chunk-2
	c2, ok := spool.Pop()
	if !ok || string(c2.Data) != "chunk-2" || c2.Offset != len("chunk-1") {
		t.Fatalf("Pop: got (%+v, %v)", c2, ok)
	}
	if !spool.IsEmpty() {
		t.Fatal("spool should be empty")
	}

	// Pop on empty returns false
	_, ok = spool.Pop()
	if ok {
		t.Fatal("Pop on empty should return false")
	}
}

func TestTraceSpool_ClearAndMarkDead(t *testing.T) {
	spool := NewTraceSpool(1024, 0)
	spool.Append([]byte("data"))
	if spool.IsEmpty() {
		t.Fatal("should not be empty")
	}

	spool.Clear()
	if !spool.IsEmpty() || spool.BufferedBytes() != 0 {
		t.Fatal("Clear failed to reset spool")
	}

	spool.Append([]byte("more"))
	if spool.IsEmpty() {
		t.Fatal("should have buffered after Clear")
	}

	spool.MarkDead()
	if !spool.IsDead() {
		t.Fatal("IsDead should be true")
	}
	if !spool.IsEmpty() {
		t.Fatal("MarkDead should clear buffered chunks")
	}

	// Further appends should be discarded when dead
	spool.Append([]byte("discarded"))
	if !spool.IsEmpty() {
		t.Fatal("Appends after MarkDead should be discarded")
	}
}

func TestTraceSpool_WaitEmpty(t *testing.T) {
	spool := NewTraceSpool(1024, 0)
	spool.Append([]byte("line"))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	// WaitEmpty should timeout if not cleared
	if err := spool.WaitEmpty(ctx); err == nil {
		t.Fatal("expected timeout error")
	}

	// WaitEmpty completes when spool is drained
	go func() {
		time.Sleep(10 * time.Millisecond)
		spool.Pop()
	}()

	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	if err := spool.WaitEmpty(ctx2); err != nil {
		t.Fatalf("WaitEmpty returned error: %v", err)
	}
}
