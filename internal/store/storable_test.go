package store

import (
	"reflect"
	"testing"
	"unsafe"
)

func TestStorable(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		samePtr  bool
	}{
		{
			name:     "clean text returned as it is",
			input:    "hello world 123",
			expected: "hello world 123",
			samePtr:  true,
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "latin-1 byte",
			input:    "caf\xe9",
			expected: "caf\ufffd",
		},
		{
			name:     "cut multi-byte character",
			input:    "\xc3",
			expected: "\ufffd",
		},
		{
			name:     "nul byte",
			input:    "a\x00b",
			expected: "a\ufffdb",
		},
		{
			name:     "adjacent nuls collapse",
			input:    "a\x00\x00b",
			expected: "a\ufffdb",
		},
		{
			name:     "adjacent nul and invalid byte collapse",
			input:    "a\x00\xe9b",
			expected: "a\ufffdb",
		},
		{
			name:     "adjacent invalid byte and nul collapse",
			input:    "a\xe9\x00b",
			expected: "a\ufffdb",
		},
		{
			name:     "adjacent invalid bytes and nuls collapse",
			input:    "\x00\x00\xe9\x00\xff",
			expected: "\ufffd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := storable(tt.input)
			if got != tt.expected {
				t.Fatalf("storable(%q) = %q, want %q", tt.input, got, tt.expected)
			}
			if tt.samePtr && len(tt.input) > 0 {
				inPtr := unsafe.StringData(tt.input)
				outPtr := unsafe.StringData(got)
				if inPtr != outPtr {
					t.Errorf("storable returned a new copy of clean text, want exact original slice")
				}
			}
		})
	}
}

func TestCleanTaskResultAndResults(t *testing.T) {
	orig := []TaskResult{
		{
			Name:   "clean-task",
			Status: "passed",
			Output: "clean output",
			Error:  "",
		},
		{
			Name:   "dirty-task",
			Status: "failed",
			Output: "out: caf\xe9\x00",
			Error:  "err: \x00bad",
		},
	}

	// Make a deep copy to verify caller's slice and structs are not modified
	beforeCopy := make([]TaskResult, len(orig))
	copy(beforeCopy, orig)

	cleaned := cleanTaskResults(orig)

	// Verify orig is not modified
	if !reflect.DeepEqual(orig, beforeCopy) {
		t.Fatalf("orig slice was modified: got %+v, want %+v", orig, beforeCopy)
	}

	// Verify cleaned results
	if len(cleaned) != 2 {
		t.Fatalf("len(cleaned) = %d, want 2", len(cleaned))
	}

	if cleaned[0].Output != "clean output" || cleaned[0].Error != "" {
		t.Errorf("cleaned[0] = %+v, want clean output", cleaned[0])
	}
	if cleaned[1].Output != "out: caf\ufffd" {
		t.Errorf("cleaned[1].Output = %q, want %q", cleaned[1].Output, "out: caf\ufffd")
	}
	if cleaned[1].Error != "err: \ufffdbad" {
		t.Errorf("cleaned[1].Error = %q, want %q", cleaned[1].Error, "err: \ufffdbad")
	}

	// Single task result test
	single := TaskResult{Name: "single", Status: "passed", Output: "a\x00b", Error: "c\xe9d"}
	singleCleaned := cleanTaskResult(single)
	if single.Output != "a\x00b" || single.Error != "c\xe9d" {
		t.Fatalf("single was modified: %+v", single)
	}
	if singleCleaned.Output != "a\ufffdb" || singleCleaned.Error != "c\ufffdd" {
		t.Fatalf("singleCleaned = %+v", singleCleaned)
	}

	// Empty slice test
	if res := cleanTaskResults(nil); res != nil {
		t.Errorf("cleanTaskResults(nil) = %+v, want nil", res)
	}
}
