// Package web implements the HTTP webhook receiver for MetaCI.
//
// It receives push events from GitLab (and other git hosts), extracts the
// changed-file information, and passes it to the DAG engine for evaluation.
package web

import (
	"encoding/json"
	"fmt"
	"strings"

	"gitlab.com/lunaris-bot/metaci/internal/model"
)

// --- GitLab Webhook Payload ---

// GitLabPushEvent represents the JSON payload sent by GitLab on a push event.
// Only the fields we need are extracted; unknown fields are silently ignored.
type GitLabPushEvent struct {
	ObjectKind string         `json:"object_kind"`
	Ref        string         `json:"ref"`
	Project    GitLabProject  `json:"project"`
	Commits    []GitLabCommit `json:"commits"`
}

// GitLabProject holds the project metadata from the webhook.
type GitLabProject struct {
	PathWithNamespace string `json:"path_with_namespace"`
	DefaultBranch     string `json:"default_branch"`
}

// GitLabCommit holds a single commit's file-change data.
type GitLabCommit struct {
	ID       string   `json:"id"`
	Message  string   `json:"message"`
	Added    []string `json:"added"`
	Modified []string `json:"modified"`
	Removed  []string `json:"removed"`
}

// --- Normalized Event ---

// PushEvent is the host-agnostic representation of a push event.
// After parsing, all downstream code works with PushEvent only.
type PushEvent struct {
	// Repo is the path_with_namespace (e.g. "org/service-a") or a custom
	// identifier that maps to a ci_repo in the pipeline.
	Repo string

	// Branch is the short branch name (e.g. "main", not "refs/heads/main").
	Branch string

	// FilesChanged is the deduplicated set of all files touched across all
	// commits in this push.
	FilesChanged map[string]bool

	// CommitCount is the number of commits in this push.
	CommitCount int
}

// IsEmpty returns true if the event contains no file changes.
func (e *PushEvent) IsEmpty() bool {
	return len(e.FilesChanged) == 0
}

// --- File Classification ---

// FileClassifier determines whether a file is "code" or "non-code" based on
// configurable patterns. This is used to decide whether changes should trigger
// CI tasks.
type FileClassifier struct {
	// CodePatterns is a list of file extensions considered as code.
	// Defaults to common Go, Python, Rust, JS/TS extensions.
	CodePatterns []string

	// ExcludePatterns is a list of path prefixes or extensions to always
	// exclude from triggering builds. E.g. ["docs/", ".md"].
	ExcludePatterns []string
}

// DefaultClassifier returns a classifier with sensible defaults for a
// polyglot Go/Python/Rust project.
func DefaultClassifier() *FileClassifier {
	return &FileClassifier{
		CodePatterns: []string{
			".go", ".py", ".rs", ".js", ".ts", ".java", ".c", ".cpp", ".h",
			".tf", ".yaml", ".yml", ".toml", ".json",
		},
		ExcludePatterns: []string{
			"docs/",
			".md",
			"LICENSE",
			"CHANGELOG",
			".gitignore",
		},
	}
}

// IsCode returns true if the file should be considered as code (i.e., its
// change should trigger CI tasks).
func (fc *FileClassifier) IsCode(path string) bool {
	// Check exclude patterns first.
	for _, pattern := range fc.ExcludePatterns {
		if strings.HasSuffix(path, pattern) || strings.HasPrefix(path, pattern) {
			return false
		}
	}
	// Check code patterns.
	for _, ext := range fc.CodePatterns {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

// HasCodeChanges returns true if any file in the changed set is classified as code.
func (fc *FileClassifier) HasCodeChanges(files map[string]bool) bool {
	for path := range files {
		if fc.IsCode(path) {
			return true
		}
	}
	return false
}

// ChangedRepos extracts the set of repos that have code-level changes.
// The input is a map of repo-name → set of changed files.
func (fc *FileClassifier) ChangedRepos(repoFiles map[string]map[string]bool) map[string]bool {
	result := make(map[string]bool)
	for repo, files := range repoFiles {
		result[repo] = fc.HasCodeChanges(files)
	}
	return result
}

// MatchRepos maps a flat set of changed files to pipeline repo names.
// A repo matches if any changed file is under its configured path (for
// subpath repos) or if it has any code-level change (for git repos, which
// represent the whole repository).
func (fc *FileClassifier) MatchRepos(files map[string]bool, repos []model.Repo) []string {
	var matched []string
	for _, repo := range repos {
		if repo.Kind == "subpath" || repo.Path != "" {
			// subpath: match files under repo.Path prefix
			prefix := strings.TrimSuffix(repo.Path, "/") + "/"
			for f := range files {
				if strings.HasPrefix(f, prefix) || f == repo.Path {
					if fc.HasCodeChanges(files) {
						matched = append(matched, repo.Name)
					}
					break
				}
			}
		} else {
			// git repo: whole repo, any code change matches
			if fc.HasCodeChanges(files) {
				matched = append(matched, repo.Name)
			}
		}
	}
	return matched
}

// --- Parsing ---

// ParseGitLabPushEvent parses raw JSON from a GitLab push webhook into a
// normalized PushEvent. It only processes "push" events (object_kind == "push").
func ParseGitLabPushEvent(payload []byte) (*PushEvent, error) {
	var raw GitLabPushEvent
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("parse webhook JSON: %w", err)
	}

	if raw.ObjectKind != "push" {
		return nil, fmt.Errorf("unsupported event kind %q (only \"push\" is supported)", raw.ObjectKind)
	}

	// Normalize branch: "refs/heads/main" → "main"
	branch := strings.TrimPrefix(raw.Ref, "refs/heads/")
	branch = strings.TrimPrefix(branch, "refs/tags/")

	// Collect all changed files across all commits.
	files := make(map[string]bool)
	for _, c := range raw.Commits {
		for _, f := range c.Added {
			files[f] = true
		}
		for _, f := range c.Modified {
			files[f] = true
		}
		for _, f := range c.Removed {
			files[f] = true
		}
	}

	return &PushEvent{
		Repo:         raw.Project.PathWithNamespace,
		Branch:       branch,
		FilesChanged: files,
		CommitCount:  len(raw.Commits),
	}, nil
}
