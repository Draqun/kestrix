package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTaskResult_JSONRoundTrip(t *testing.T) {
	original := TaskResult{
		Name:     "build",
		Status:   "passed",
		Output:   "done",
		Error:    "",
		Duration: 250 * time.Millisecond,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded TaskResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Name != original.Name || decoded.Status != original.Status ||
		decoded.Output != original.Output || decoded.Duration != original.Duration {
		t.Errorf("round-trip mismatch: got %+v, want %+v", decoded, original)
	}
}

func TestTaskResult_JSONFieldNames(t *testing.T) {
	tr := TaskResult{Name: "t", Status: "passed", Output: "ok", Error: ""}
	data, _ := json.Marshal(tr)
	var m map[string]json.RawMessage
	json.Unmarshal(data, &m)
	// name/status/output/duration are always present; error is omitempty.
	for _, key := range []string{"name", "status", "output", "duration"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing JSON field %q in TaskResult output", key)
		}
	}
}

func TestTaskResult_ErrorOmitEmpty(t *testing.T) {
	tr := TaskResult{Name: "t", Status: "passed"}
	data, _ := json.Marshal(tr)
	var m map[string]json.RawMessage
	json.Unmarshal(data, &m)
	if _, ok := m["error"]; ok {
		t.Error("empty Error should be omitted from JSON")
	}
}

func TestTaskResult_ApprovalOmitEmpty(t *testing.T) {
	tr := TaskResult{Name: "t", Status: "manual"}
	data, _ := json.Marshal(tr)
	var m map[string]json.RawMessage
	json.Unmarshal(data, &m)
	if _, ok := m["approved_by"]; ok {
		t.Error("empty ApprovedBy should be omitted from JSON")
	}
	if _, ok := m["approved_at"]; ok {
		t.Error("nil ApprovedAt should be omitted from JSON")
	}
}

func TestTaskResult_ApprovalPresentWhenSet(t *testing.T) {
	now := time.Now()
	tr := TaskResult{Name: "t", Status: "passed", ApprovedBy: "user:alice", ApprovedAt: &now}
	data, _ := json.Marshal(tr)
	var m map[string]json.RawMessage
	json.Unmarshal(data, &m)
	if _, ok := m["approved_by"]; !ok {
		t.Error("ApprovedBy should be present when set")
	}
	if _, ok := m["approved_at"]; !ok {
		t.Error("ApprovedAt should be present when set")
	}
}

func TestProject_JSONRoundTrip(t *testing.T) {
	proj := Project{
		Name:         "demo",
		DefaultImage: "alpine:latest",
		Repos: []Repo{
			{Name: "svc", Path: "src", Kind: "subpath"},
		},
		Tasks: []Task{
			{Name: "build", Trigger: "repo.svc.changed", Manual: true, DependsOn: []string{"a"}},
		},
		Variables: []Variable{
			{Key: "FOO", Value: "bar", Protected: true},
		},
	}
	data, err := json.Marshal(proj)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	for _, key := range []string{"name", "default_image", "repos", "tasks"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing JSON field %q in Project output: %v", key, string(data))
		}
	}

	var decoded Project
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if decoded.Name != "demo" || decoded.DefaultImage != "alpine:latest" {
		t.Errorf("Project round-trip mismatch: %+v", decoded)
	}
	if len(decoded.Repos) != 1 || decoded.Repos[0].Name != "svc" {
		t.Errorf("Repos round-trip mismatch: %+v", decoded.Repos)
	}
	if len(decoded.Tasks) != 1 || decoded.Tasks[0].Trigger != "repo.svc.changed" || !decoded.Tasks[0].Manual {
		t.Errorf("Tasks round-trip mismatch: %+v", decoded.Tasks)
	}
	if len(decoded.Variables) != 1 || decoded.Variables[0].Key != "FOO" || !decoded.Variables[0].Protected {
		t.Errorf("Variables round-trip mismatch: %+v", decoded.Variables)
	}
}

func TestProject_JSONFieldNamesMatchSnapshot(t *testing.T) {
	// Verify that model.Project JSON field names match the old
	// PipelineSnapshot field names, so the new serialized form is
	// compatible with existing pipeline_def JSONB in the DB.
	proj := Project{
		Name:         "demo",
		DefaultImage: "alpine",
	}
	data, _ := json.Marshal(proj)

	// Decode into a raw map to inspect key names.
	var m map[string]json.RawMessage
	json.Unmarshal(data, &m)

	expectedKeys := []string{"name", "default_image", "repos", "tasks"}
	for _, k := range expectedKeys {
		if _, ok := m[k]; !ok {
			t.Errorf("missing JSON field %q (expected to match PipelineSnapshot)", k)
		}
	}
	// variables is omitempty so it should NOT appear when empty.
	if _, ok := m["variables"]; ok {
		t.Error("variables should be omitted when empty")
	}
}

func TestRepo_JSONFieldNames(t *testing.T) {
	r := Repo{Name: "svc", Path: "src", Kind: "subpath"}
	data, _ := json.Marshal(r)
	var m map[string]json.RawMessage
	json.Unmarshal(data, &m)
	for _, key := range []string{"name", "path", "kind"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing JSON field %q in Repo output", key)
		}
	}
}

func TestTask_JSONFieldNames(t *testing.T) {
	tk := Task{
		Name:      "build",
		Trigger:   "repo.svc.changed",
		DependsOn: []string{"a"},
		Manual:    true,
	}
	data, _ := json.Marshal(tk)
	var m map[string]json.RawMessage
	json.Unmarshal(data, &m)
	for _, key := range []string{"name", "trigger", "depends_on", "manual", "execute"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing JSON field %q in Task output: %s", key, string(data))
		}
	}
}

func TestVariable_JSONFieldNames(t *testing.T) {
	v := Variable{Key: "FOO", Value: "bar", Protected: true}
	data, _ := json.Marshal(v)
	var m map[string]json.RawMessage
	json.Unmarshal(data, &m)
	for _, key := range []string{"key", "value", "protected"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing JSON field %q in Variable output: %s", key, string(data))
		}
	}
}

// A run's terminal status is one it never leaves: passed, failed or
// canceled. Everything else is still running.
func TestIsTerminalRunStatus(t *testing.T) {
	for _, status := range []string{RunStatusPassed, RunStatusFailed, RunStatusCanceled} {
		if !IsTerminalRunStatus(status) {
			t.Errorf("IsTerminalRunStatus(%q) = false, want true", status)
		}
	}
	for _, status := range []string{"", "running", "queued", "pending"} {
		if IsTerminalRunStatus(status) {
			t.Errorf("IsTerminalRunStatus(%q) = true, want false", status)
		}
	}
}

func TestIsTerminalJobStatus(t *testing.T) {
	for _, status := range []string{RemoteJobStatusPassed, RemoteJobStatusFailed, RemoteJobStatusCanceled} {
		if !IsTerminalJobStatus(status) {
			t.Errorf("IsTerminalJobStatus(%q) = false, want true", status)
		}
	}
	for _, status := range []string{"", "running", "claimed", "pending"} {
		if IsTerminalJobStatus(status) {
			t.Errorf("IsTerminalJobStatus(%q) = true, want false", status)
		}
	}
}
