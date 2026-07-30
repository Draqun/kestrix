package web

import (
	"encoding/json"
	"testing"

	"kestrix/internal/model"
)

func TestParseGitLabPushEvent(t *testing.T) {
	payload := `{
		"object_kind": "push",
		"ref": "refs/heads/main",
		"project": {
			"path_with_namespace": "org/service-a",
			"default_branch": "main"
		},
		"commits": [
			{
				"id": "abc123",
				"message": "fix handler",
				"added": ["new_file.go"],
				"modified": ["handler.go", "docs/README.md"],
				"removed": ["old_file.go"]
			}
		]
	}`

	event, err := ParseGitLabPushEvent([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Repo != "org/service-a" {
		t.Errorf("repo = %q, want %q", event.Repo, "org/service-a")
	}
	if event.Branch != "main" {
		t.Errorf("branch = %q, want %q", event.Branch, "main")
	}
	if event.CommitCount != 1 {
		t.Errorf("commit count = %d, want 1", event.CommitCount)
	}
	if len(event.FilesChanged) != 4 {
		t.Errorf("files changed = %d, want 4 (new_file.go, handler.go, docs/README.md, old_file.go)", len(event.FilesChanged))
	}
	if !event.FilesChanged["new_file.go"] {
		t.Error("expected new_file.go in changed files")
	}
	if !event.FilesChanged["docs/README.md"] {
		t.Error("expected docs/README.md in changed files")
	}
}

func TestParseGitLabPushEventNotPush(t *testing.T) {
	payload := `{"object_kind": "merge_request"}`
	_, err := ParseGitLabPushEvent([]byte(payload))
	if err == nil {
		t.Fatal("expected error for non-push event")
	}
}

func TestParseGitLabPushEventInvalidJSON(t *testing.T) {
	_, err := ParseGitLabPushEvent([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseGitLabPushEventTag(t *testing.T) {
	payload := `{
		"object_kind": "push",
		"ref": "refs/tags/v1.0.0",
		"project": {
			"path_with_namespace": "org/repo",
			"default_branch": "main"
		},
		"commits": []
	}`

	event, err := ParseGitLabPushEvent([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Branch != "v1.0.0" {
		t.Errorf("branch = %q, want %q (tag should strip refs/tags/)", event.Branch, "v1.0.0")
	}
}

// --- FileClassifier tests ---

func TestFileClassifierCodeChange(t *testing.T) {
	fc := DefaultClassifier()

	if !fc.IsCode("handler.go") {
		t.Error("expected .go to be code")
	}
	if !fc.IsCode("src/auth/main.py") {
		t.Error("expected .py to be code")
	}
	if fc.IsCode("docs/README.md") {
		t.Error("expected .md to NOT be code")
	}
	if fc.IsCode("LICENSE") {
		t.Error("expected LICENSE to NOT be code")
	}
	// A file extension that matches no CodePatterns and is not on the
	// exclude list must fall through and return false. This branch on the
	// end of IsCode was previously uncovered.
	if fc.IsCode("phony.unknown") {
		t.Error("expected unknown extension to NOT be code (fall-through branch)")
	}
	// HasPrefix on ExcludePatterns: paths beginning with a slash-prefix like
	// "docs/" should be excluded even when the rest doesn't carry an
	// excluded suffix.
	if fc.IsCode("docs/internal/notes.txt") {
		t.Error("expected docs/ prefix to exclude via HasPrefix")
	}
}

func TestFileClassifierHasCodeChanges(t *testing.T) {
	fc := DefaultClassifier()

	// Only docs changed — no code.
	docsOnly := map[string]bool{"docs/README.md": true}
	if fc.HasCodeChanges(docsOnly) {
		t.Error("expected no code changes for docs-only")
	}

	// Code changed.
	codeChanged := map[string]bool{"handler.go": true}
	if !fc.HasCodeChanges(codeChanged) {
		t.Error("expected code changes for .go file")
	}
}

func TestFileClassifierChangedRepos(t *testing.T) {
	fc := DefaultClassifier()

	repoFiles := map[string]map[string]bool{
		"service_a": {"handler.go": true},
		"service_b": {"docs/README.md": true},
	}

	result := fc.ChangedRepos(repoFiles)
	if !result["service_a"] {
		t.Error("expected service_a to have code changes")
	}
	if result["service_b"] {
		t.Error("expected service_b to NOT have code changes (docs only)")
	}
}

// TestFileClassifierMatchRepos exercises MatchRepos across both supported
// repo kinds (subpath via path, and git repos with no path). Each branch of
// the if/else on repo.Kind/Path needs at least one assertion so the function
// reaches full line coverage.
func TestFileClassifierMatchRepos(t *testing.T) {
	fc := DefaultClassifier()

	tests := []struct {
		name     string
		files    map[string]bool
		repos    []model.Repo
		expected []string
	}{
		{
			name:  "git repo with code change matches",
			files: map[string]bool{"handler.go": true},
			// Git repo: Kind != "subpath", Path == "". Whole repo matches
			// whenever HasCodeChanges returns true.
			repos:    []model.Repo{{Name: "app", Kind: "git"}},
			expected: []string{"app"},
		},
		{
			name:  "git repo with no code changes does not match",
			files: map[string]bool{"docs/README.md": true},
			repos: []model.Repo{{Name: "app", Kind: "git"}},
		},
		{
			name:  "subpath repo with code file under prefix matches",
			files: map[string]bool{"services/auth/main.go": true},
			repos: []model.Repo{
				{Name: "auth", Kind: "subpath", Path: "services/auth"},
			},
			expected: []string{"auth"},
		},
		{
			name:  "subpath repo with only docs under prefix does not match",
			files: map[string]bool{"services/auth/README.md": true},
			repos: []model.Repo{
				{Name: "auth", Kind: "subpath", Path: "services/auth"},
			},
		},
		{
			name:  "file matching the repo path exactly still wins",
			files: map[string]bool{"packages/foo": true, "packages/foo/main.go": true},
			repos: []model.Repo{
				{Name: "foo", Kind: "subpath", Path: "packages/foo"},
			},
			expected: []string{"foo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fc.MatchRepos(tt.files, tt.repos)
			if len(got) != len(tt.expected) {
				t.Fatalf("matched = %v, want %v", got, tt.expected)
			}
			for i, name := range got {
				if name != tt.expected[i] {
					t.Errorf("matched[%d] = %q, want %q", i, name, tt.expected[i])
				}
			}
		})
	}
}

// --- JSON serialization tests ---

func TestParseGitLabPushEventEmpty(t *testing.T) {
	payload := `{
		"object_kind": "push",
		"ref": "refs/heads/main",
		"project": {
			"path_with_namespace": "org/repo",
			"default_branch": "main"
		},
		"commits": []
	}`

	event, err := ParseGitLabPushEvent([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !event.IsEmpty() {
		t.Error("expected empty event for push with no commits")
	}
}

func TestParseGitLabPushEventMultipleCommits(t *testing.T) {
	payload := `{
		"object_kind": "push",
		"ref": "refs/heads/main",
		"project": {
			"path_with_namespace": "org/repo",
			"default_branch": "main"
		},
		"commits": [
			{
				"id": "c1",
				"message": "first",
				"added": ["a.go"],
				"modified": [],
				"removed": []
			},
			{
				"id": "c2",
				"message": "second",
				"added": [],
				"modified": ["a.go", "b.go"],
				"removed": ["c.go"]
			}
		]
	}`

	event, err := ParseGitLabPushEvent([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.CommitCount != 2 {
		t.Errorf("commit count = %d, want 2", event.CommitCount)
	}
	// a.go appears in both commits but should be deduplicated.
	if len(event.FilesChanged) != 3 {
		t.Errorf("files changed = %d, want 3 (a.go, b.go, c.go)", len(event.FilesChanged))
	}
}

func TestGitLabPushEventJSON(t *testing.T) {
	// Verify our struct round-trips through JSON correctly.
	event := &PushEvent{
		Repo:         "org/test",
		Branch:       "main",
		FilesChanged: map[string]bool{"a.go": true},
		CommitCount:  1,
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var decoded PushEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.Repo != event.Repo {
		t.Errorf("repo = %q, want %q", decoded.Repo, event.Repo)
	}
}
