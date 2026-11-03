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
