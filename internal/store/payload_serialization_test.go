package store

import (
	"encoding/json"
	"testing"

	"kestrix/internal/domain"
)

func TestStoredRemoteJobPayload_JSONRoundTrip(t *testing.T) {
	orig := storedRemoteJobPayload{
		Task: domain.Task{
			Name:    "build",
			Timeout: "15m",
			Execute: domain.ExecuteSpec{Repo: "app", Script: "make build"},
		},
		Repos: []domain.Repo{
			{Name: "app", Path: "services/app"},
		},
		DefaultImage: "golang:1.26",
		Archive:      []byte("source-archive"),
		Artifacts:    [][]byte{[]byte("artifact-1")},
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded storedRemoteJobPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Task.Timeout != "15m" {
		t.Errorf("Task.Timeout = %q, want '15m'", decoded.Task.Timeout)
	}
	if decoded.Task.Name != "build" {
		t.Errorf("Task.Name = %q, want 'build'", decoded.Task.Name)
	}
}
