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
		"checkout_sha": "immutable123",
		"project": {
			"path_with_namespace": "org/service-a",
			"default_branch": "main",
			"git_http_url": "https://git.example.test/org/service-a.git",
			"git_ssh_url": "",
			"web_url": "https://git.example.test/org/service-a"
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
	if event.CheckoutSHA != "immutable123" {
		t.Errorf("checkout SHA = %q, want immutable123", event.CheckoutSHA)
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
	// URLs keep payload order (http, ssh, web); the empty ssh URL is dropped.
	if len(event.RepoURLs) != 2 || event.RepoURLs[0] != "https://git.example.test/org/service-a.git" || event.RepoURLs[1] != "https://git.example.test/org/service-a" {
		t.Errorf("repo URLs = %v", event.RepoURLs)
	}
	if !event.IsBranch {
		t.Errorf("expected IsBranch = true")
	}
	if event.IsTag {
		t.Errorf("expected IsTag = false")
	}
	if event.Deleted {
		t.Errorf("expected Deleted = false")
	}
	if event.DefaultBranch != "main" {
		t.Errorf("default branch = %q, want %q", event.DefaultBranch, "main")
	}
}

func TestParseGitLabPushEventCollectsAllRepoURLs(t *testing.T) {
	event, err := ParseGitLabPushEvent([]byte(`{"object_kind":"push","ref":"refs/heads/main","project":{
		"git_http_url":"https://h/a.git","git_ssh_url":"git@h:a.git","web_url":"https://h/a"}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://h/a.git", "git@h:a.git", "https://h/a"}
	if len(event.RepoURLs) != 3 {
		t.Fatalf("repo URLs = %v, want %v", event.RepoURLs, want)
	}
	for i := range want {
		if event.RepoURLs[i] != want[i] {
			t.Errorf("repo URL %d = %q, want %q", i, event.RepoURLs[i], want[i])
		}
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

func TestParseGitHubPushEvent(t *testing.T) {
	// Branch push with multiple commits unioning changed files
	payload := `{
		"ref": "refs/heads/main",
		"before": "1111111111111111111111111111111111111111",
		"after": "2222222222222222222222222222222222222222",
		"deleted": false,
		"repository": {
			"full_name": "octocat/Hello-World",
			"default_branch": "main",
			"clone_url": "https://github.com/octocat/Hello-World.git",
			"ssh_url": "git@github.com:octocat/Hello-World.git",
			"html_url": "https://github.com/octocat/Hello-World"
		},
		"commits": [
			{
				"id": "c1",
				"message": "first commit",
				"added": ["file1.go", "file2.go"],
				"modified": [],
				"removed": []
			},
			{
				"id": "c2",
				"message": "second commit",
				"added": [],
				"modified": ["file2.go", "file3.go"],
				"removed": ["old.go"]
			}
		]
	}`

	event, err := ParseGitHubPushEvent([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Repo != "octocat/Hello-World" {
		t.Errorf("repo = %q, want octocat/Hello-World", event.Repo)
	}
	if event.Branch != "main" {
		t.Errorf("branch = %q, want main", event.Branch)
	}
	if !event.IsBranch {
		t.Errorf("expected IsBranch = true")
	}
	if event.IsTag {
		t.Errorf("expected IsTag = false")
	}
	if event.Deleted {
		t.Errorf("expected Deleted = false")
	}
	if event.CheckoutSHA != "2222222222222222222222222222222222222222" {
		t.Errorf("checkout SHA = %q, want 2222222222222222222222222222222222222222", event.CheckoutSHA)
	}
	if event.DefaultBranch != "main" {
		t.Errorf("default branch = %q, want main", event.DefaultBranch)
	}
	if event.CommitCount != 2 {
		t.Errorf("commit count = %d, want 2", event.CommitCount)
	}
	if event.GitLabProjectID != 0 {
		t.Errorf("gitLab project ID = %d, want 0", event.GitLabProjectID)
	}
	// Files unioned across both commits: file1.go, file2.go, file3.go, old.go (4 files)
	if len(event.FilesChanged) != 4 {
		t.Errorf("files changed count = %d, want 4", len(event.FilesChanged))
	}
	for _, f := range []string{"file1.go", "file2.go", "file3.go", "old.go"} {
		if !event.FilesChanged[f] {
			t.Errorf("expected file %q in FilesChanged", f)
		}
	}
	// RepoURLs
	wantURLs := []string{
		"https://github.com/octocat/Hello-World.git",
		"git@github.com:octocat/Hello-World.git",
		"https://github.com/octocat/Hello-World",
	}
	if len(event.RepoURLs) != 3 {
		t.Fatalf("repo URLs = %v, want %v", event.RepoURLs, wantURLs)
	}
	for i := range wantURLs {
		if event.RepoURLs[i] != wantURLs[i] {
			t.Errorf("repo URL %d = %q, want %q", i, event.RepoURLs[i], wantURLs[i])
		}
	}
}

func TestParseGitHubPushEventTag(t *testing.T) {
	payload := `{
		"ref": "refs/tags/v1.2.3",
		"after": "abcdef1234567890abcdef1234567890abcdef12",
		"deleted": false,
		"repository": {
			"full_name": "octocat/Hello-World",
			"default_branch": "main"
		},
		"commits": []
	}`

	event, err := ParseGitHubPushEvent([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Branch != "v1.2.3" {
		t.Errorf("branch = %q, want v1.2.3", event.Branch)
	}
	if event.IsBranch {
		t.Errorf("expected IsBranch = false")
	}
	if !event.IsTag {
		t.Errorf("expected IsTag = true")
	}
	if event.Deleted {
		t.Errorf("expected Deleted = false")
	}
}

func TestParseGitHubPushEventDeleted(t *testing.T) {
	// deleted: true explicitly
	payload := `{
		"ref": "refs/heads/feature-branch",
		"after": "0000000000000000000000000000000000000000",
		"deleted": true,
		"repository": {
			"full_name": "octocat/Hello-World",
			"default_branch": "main"
		},
		"commits": []
	}`

	event, err := ParseGitHubPushEvent([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !event.Deleted {
		t.Errorf("expected Deleted = true")
	}

	// deleted: false but after is all zero SHA
	payloadZeroAfter := `{
		"ref": "refs/heads/feature-branch",
		"after": "0000000000000000000000000000000000000000",
		"deleted": false,
		"repository": {
			"full_name": "octocat/Hello-World",
			"default_branch": "main"
		},
		"commits": []
	}`
	eventZero, err := ParseGitHubPushEvent([]byte(payloadZeroAfter))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !eventZero.Deleted {
		t.Errorf("expected Deleted = true for zero SHA after")
	}
}

func TestParseGitHubPushEventMissingOptionalURLs(t *testing.T) {
	// Only clone_url present; ssh_url and html_url omitted
	payload := `{
		"ref": "refs/heads/main",
		"after": "abc123",
		"repository": {
			"full_name": "octocat/Hello-World",
			"clone_url": "https://github.com/octocat/Hello-World.git"
		},
		"commits": []
	}`

	event, err := ParseGitHubPushEvent([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(event.RepoURLs) != 1 || event.RepoURLs[0] != "https://github.com/octocat/Hello-World.git" {
		t.Errorf("expected only clone_url in RepoURLs, got %v", event.RepoURLs)
	}
}

func TestParseGitHubPushEventInvalidJSON(t *testing.T) {
	_, err := ParseGitHubPushEvent([]byte("not json"))
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
	if event.IsBranch {
		t.Errorf("expected IsBranch = false")
	}
	if !event.IsTag {
		t.Errorf("expected IsTag = true")
	}
	if event.Deleted {
		t.Errorf("expected Deleted = false")
	}
}

func TestParseGitLabPushEventTagNamedLikeBranch(t *testing.T) {
	payload := `{
		"object_kind": "push",
		"ref": "refs/tags/main",
		"checkout_sha": "abc456",
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
	if event.Branch != "main" {
		t.Errorf("branch = %q, want %q", event.Branch, "main")
	}
	if event.IsBranch {
		t.Errorf("expected IsBranch = false for tag named main")
	}
	if !event.IsTag {
		t.Errorf("expected IsTag = true for tag named main")
	}
	if event.Deleted {
		t.Errorf("expected Deleted = false")
	}
}

func TestParseGitLabPushEventDeletion(t *testing.T) {
	for _, tc := range []struct {
		name        string
		checkoutSHA string
		after       string
		wantSHA     string
	}{
		{
			name:        "all zero after with empty checkout_sha",
			checkoutSHA: "",
			after:       "0000000000000000000000000000000000000000",
			wantSHA:     "0000000000000000000000000000000000000000",
		},
		{
			name:        "all zero checkout_sha",
			checkoutSHA: "0000000000000000000000000000000000000000",
			after:       "0000000000000000000000000000000000000000",
			wantSHA:     "0000000000000000000000000000000000000000",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := `{
				"object_kind": "push",
				"ref": "refs/heads/feature-branch",
				"after": "` + tc.after + `",
				"checkout_sha": "` + tc.checkoutSHA + `",
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
			if !event.Deleted {
				t.Errorf("expected Deleted = true")
			}
			if !event.IsBranch {
				t.Errorf("expected IsBranch = true")
			}
			if event.Branch != "feature-branch" {
				t.Errorf("branch = %q, want feature-branch", event.Branch)
			}
			if event.CheckoutSHA != tc.wantSHA {
				t.Errorf("checkout SHA = %q, want %q", event.CheckoutSHA, tc.wantSHA)
			}
		})
	}
}

func TestIsZeroSHA(t *testing.T) {
	if isZeroSHA("") {
		t.Error("expected empty string to not be zero SHA")
	}
	if isZeroSHA("0000000000000000000000000000000000000001") {
		t.Error("expected sha with 1 to not be zero SHA")
	}
	if !isZeroSHA("0000000000000000000000000000000000000000") {
		t.Error("expected 40 zeros to be zero SHA")
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
	if len(event.RepoURLs) != 0 {
		t.Errorf("repo URLs = %v, want none for a payload without URL fields", event.RepoURLs)
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
