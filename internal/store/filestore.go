// Package store provides persistent project configuration storage.
//
// Supports PostgreSQL (production) and JSON file (local fallback).
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store is the interface for project configuration storage.
type Store interface {
	Load() (map[string]*ProjectConfig, error)
	Save(projects map[string]*ProjectConfig) error
	Get(name string) (*ProjectConfig, error)
	Upsert(cfg *ProjectConfig) error
	Delete(name string) error
	List() ([]string, error)
	ListAllRuns(limit int) ([]Run, error)
}

// ProjectConfig describes a registered project: where its repo lives,
// which pipeline file to use, and its GitLab integration settings.
type ProjectConfig struct {
	Name     string `json:"name"`
	RepoPath string `json:"repo_path"`
	RepoURL  string `json:"repo_url,omitempty"`
	// PipelineDir is relative to RepoPath and contains all .tf files that
	// together define the pipeline.
	PipelineDir string `json:"pipeline_dir,omitempty"`
	// PipelineFile is the optional file opened by the editor. It is not used to
	// select which definitions are executed; all .tf files in PipelineDir load.
	PipelineFile    string `json:"pipeline_file"`
	GitLabProjectID int    `json:"gitlab_project_id,omitempty"`
	DefaultBranch   string `json:"default_branch,omitempty"`
	WebhookSecret   string `json:"webhook_secret,omitempty"`
	CreatedAt       string `json:"created_at"`
}

// FileStore provides atomic, file-backed project configuration storage.
type FileStore struct {
	path string
	mu   sync.RWMutex
}

// New creates a FileStore at the given path.
// If the parent directory doesn't exist, it is created.
func New(path string) (*FileStore, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create store dir: %w", err)
	}
	return &FileStore{path: path}, nil
}

// DefaultPath returns ~/.metaci/projects.json.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".metaci", "projects.json")
}

// Load reads all projects from disk. Returns empty map if file doesn't exist.
func (s *FileStore) Load() (map[string]*ProjectConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]*ProjectConfig), nil
		}
		return nil, fmt.Errorf("read %s: %w", s.path, err)
	}

	var projects map[string]*ProjectConfig
	if err := json.Unmarshal(data, &projects); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.path, err)
	}
	if projects == nil {
		projects = make(map[string]*ProjectConfig)
	}
	return projects, nil
}

// Save writes all projects to disk atomically.
func (s *FileStore) Save(projects map[string]*ProjectConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(projects, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal projects: %w", err)
	}

	// Atomic write: temp file + rename.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp) //nolint:errcheck
		return fmt.Errorf("rename temp: %w", err)
	}
	return nil
}

// Get returns a project by name, or nil if not found.
func (s *FileStore) Get(name string) (*ProjectConfig, error) {
	projects, err := s.Load()
	if err != nil {
		return nil, err
	}
	proj, ok := projects[name]
	if !ok {
		return nil, nil
	}
	return proj, nil
}

// Upsert creates or updates a project.
func (s *FileStore) Upsert(cfg *ProjectConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	projects, err := s.loadLocked()
	if err != nil {
		return err
	}

	if cfg.CreatedAt == "" {
		cfg.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if cfg.DefaultBranch == "" {
		cfg.DefaultBranch = "main"
	}
	if cfg.PipelineFile == "" {
		cfg.PipelineFile = "pipeline.tf"
	}
	if cfg.PipelineDir == "" {
		// Migrate the previous single-path representation: a value such as
		// "ci/pipeline.tf" becomes PipelineDir="ci", PipelineFile="pipeline.tf".
		cfg.PipelineDir = filepath.Dir(cfg.PipelineFile)
		cfg.PipelineFile = filepath.Base(cfg.PipelineFile)
	}

	projects[cfg.Name] = cfg
	return s.saveLocked(projects)
}

// Delete removes a project by name.
func (s *FileStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	projects, err := s.loadLocked()
	if err != nil {
		return err
	}

	if _, ok := projects[name]; !ok {
		return fmt.Errorf("project %q not found", name)
	}
	delete(projects, name)
	return s.saveLocked(projects)
}

// List returns all project names.
func (s *FileStore) List() ([]string, error) {
	projects, err := s.Load()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(projects))
	for name := range projects {
		names = append(names, name)
	}
	return names, nil
}

// ListAllRuns is not supported on FileStore; returns empty.
func (s *FileStore) ListAllRuns(limit int) ([]Run, error) {
	return []Run{}, nil
}

// loadLocked reads from disk without acquiring the mutex (caller holds it).
func (s *FileStore) loadLocked() (map[string]*ProjectConfig, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]*ProjectConfig), nil
		}
		return nil, fmt.Errorf("read %s: %w", s.path, err)
	}

	var projects map[string]*ProjectConfig
	if err := json.Unmarshal(data, &projects); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.path, err)
	}
	if projects == nil {
		projects = make(map[string]*ProjectConfig)
	}
	return projects, nil
}

// saveLocked writes to disk without acquiring the mutex (caller holds it).
func (s *FileStore) saveLocked(projects map[string]*ProjectConfig) error {
	data, err := json.MarshalIndent(projects, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal projects: %w", err)
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp) //nolint:errcheck
		return fmt.Errorf("rename temp: %w", err)
	}
	return nil
}
