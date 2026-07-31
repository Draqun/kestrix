package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/model"
)

type recordingAcquirer struct {
	mu         sync.Mutex
	workspaces []string
	fail       bool
}

type recordingSessions struct {
	mu       sync.Mutex
	requests []domain.SourceRequest
	paths    []string
	cleaned  int
	failAt   int
	cancel   context.CancelFunc
}

func (m *recordingSessions) Open(ctx context.Context, request domain.SourceRequest) (domain.SourceSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, request)
	if err := ctx.Err(); err != nil {
		return domain.SourceSession{}, err
	}
	if m.failAt > 0 && len(m.requests) == m.failAt {
		return domain.SourceSession{}, fmt.Errorf("session failed")
	}
	dir, err := os.MkdirTemp("", "runner-session-")
	if err != nil {
		return domain.SourceSession{}, err
	}
	m.paths = append(m.paths, dir)
	if len(m.paths) == 1 && m.cancel != nil {
		m.cancel()
	}
	var once sync.Once
	return domain.SourceSession{
		Source: domain.AcquiredSource{
			RepoName: request.RepoName, URL: request.URL, Path: dir,
			RequestedRef: request.RequestedRef, ResolvedRevision: "0123456789abcdef",
			CredentialRef: request.CredentialRef,
		},
		Cleanup: func() {
			once.Do(func() {
				_ = os.RemoveAll(dir)
				m.mu.Lock()
				defer m.mu.Unlock()
				m.cleaned++
			})
		},
	}, nil
}

func (m *recordingSessions) Pin(context.Context, int, []domain.AcquiredSource) error { return nil }
func (m *recordingSessions) Release(context.Context, int) error                      { return nil }
func (m *recordingSessions) Maintain(context.Context) error                          { return nil }

func (a *recordingAcquirer) Acquire(_ context.Context, repo domain.Repo, opts domain.AcquisitionOptions) (domain.AcquiredSource, error) {
	if a.fail {
		return domain.AcquiredSource{}, fmt.Errorf("acquisition failed")
	}
	dir := filepath.Join(opts.Workspace, repo.Name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return domain.AcquiredSource{}, err
	}
	a.mu.Lock()
	a.workspaces = append(a.workspaces, opts.Workspace)
	a.mu.Unlock()
	return domain.AcquiredSource{RepoName: repo.Name, Path: dir, RequestedRef: repo.Ref, ResolvedRevision: "abc1234"}, nil
}

func remoteProject() model.Project {
	return model.Project{
		Name:  "remote",
		Repos: []model.Repo{{Name: "app", URL: "https://example.test/app.git", Ref: "main"}},
		Tasks: []model.Task{{Name: "build", Trigger: "repo.app.changed", Execute: model.ExecuteSpec{Repo: "app", Script: "true"}}},
	}
}

func TestSourceAcquisitionFailurePreventsTaskExecution(t *testing.T) {
	acquirer := &recordingAcquirer{fail: true}
	_, err := RunLocal(context.Background(), remoteProject(), map[string]bool{"app": true}, Options{
		WorkDir:        t.TempDir(),
		SourceAcquirer: acquirer,
	})
	if err == nil || err.Error() != "acquisition failed" {
		t.Fatalf("err = %v", err)
	}
}

func TestConcurrentRunsReceiveDistinctSourceWorkspaces(t *testing.T) {
	acquirer := &recordingAcquirer{}
	project := remoteProject()
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = RunLocal(context.Background(), project, map[string]bool{"app": true}, Options{
				WorkDir:        t.TempDir(),
				SourceAcquirer: acquirer,
			})
		}()
	}
	wg.Wait()
	acquirer.mu.Lock()
	defer acquirer.mu.Unlock()
	if len(acquirer.workspaces) != 2 || acquirer.workspaces[0] == acquirer.workspaces[1] {
		t.Fatalf("workspaces = %v, want two distinct workspaces", acquirer.workspaces)
	}
	for _, workspace := range acquirer.workspaces {
		if _, err := os.Stat(workspace); !os.IsNotExist(err) {
			t.Fatalf("workspace %q was not cleaned: %v", workspace, err)
		}
	}
}

func TestSourceSessionsAcquireEveryRemoteBeforeTasksAndCleanupOnFailure(t *testing.T) {
	manager := &recordingSessions{failAt: 2}
	project := remoteProject()
	project.Repos = append(project.Repos, model.Repo{Name: "docs", URL: "https://example.test/docs.git", Ref: "v1"})
	started := false
	_, err := RunLocal(context.Background(), project, map[string]bool{"app": true}, Options{
		WorkDir: t.TempDir(), SourceSessions: manager,
		OnTaskStart: func(string) { started = true },
	})
	if err == nil || err.Error() != "session failed" {
		t.Fatalf("err = %v", err)
	}
	if started {
		t.Fatal("task started before all sources were acquired")
	}
	if manager.cleaned != 1 {
		t.Fatalf("cleanup count = %d, want 1", manager.cleaned)
	}
	if _, statErr := os.Stat(manager.paths[0]); !os.IsNotExist(statErr) {
		t.Fatalf("first source session was not cleaned: %v", statErr)
	}
}

func TestSourceSessionTimeoutPreventsTasksAndCleansPartialAcquisition(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	manager := &recordingSessions{cancel: cancel}
	project := remoteProject()
	project.Repos = append(project.Repos, model.Repo{Name: "docs", URL: "https://example.test/docs.git", Ref: "main"})
	started := false

	_, err := RunLocal(ctx, project, map[string]bool{"app": true}, Options{
		WorkDir: t.TempDir(), SourceSessions: manager,
		OnTaskStart: func(string) { started = true },
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context cancellation", err)
	}
	if started {
		t.Fatal("task started after source acquisition context was canceled")
	}
	if manager.cleaned != 1 {
		t.Fatalf("cleanup count = %d, want 1", manager.cleaned)
	}
	if _, statErr := os.Stat(manager.paths[0]); !os.IsNotExist(statErr) {
		t.Fatalf("partial source session was not cleaned: %v", statErr)
	}
}

func TestConcurrentRunsReceiveDistinctSourceSessionsAndCleanThem(t *testing.T) {
	manager := &recordingSessions{}
	project := remoteProject()
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = RunLocal(context.Background(), project, map[string]bool{"app": true}, Options{
				WorkDir: t.TempDir(), SourceSessions: manager,
			})
		}()
	}
	wg.Wait()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if len(manager.paths) != 2 || manager.paths[0] == manager.paths[1] {
		t.Fatalf("source paths = %v, want distinct sessions", manager.paths)
	}
	if manager.cleaned != 2 {
		t.Fatalf("cleanup count = %d, want 2", manager.cleaned)
	}
	for _, path := range manager.paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("source session %q was not cleaned: %v", path, err)
		}
	}
}

func TestSourceSessionsUseIsolatedPathsAndPersistResolvedMetadata(t *testing.T) {
	manager := &recordingSessions{}
	var acquired []domain.AcquiredSource
	project := remoteProject()
	project.Tasks[0].Execute.Script = "test -d \"$CI_PROJECT_DIR\""
	results, err := RunLocal(context.Background(), project, map[string]bool{"app": true}, Options{
		WorkDir: t.TempDir(), SourceSessions: manager,
		OnSourcesAcquired: func(sources []domain.AcquiredSource) error {
			acquired = append(acquired, sources...)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("RunLocal: %v", err)
	}
	if len(results) != 1 || results[0].Status != "passed" {
		t.Fatalf("results = %+v", results)
	}
	if len(acquired) != 1 || acquired[0].RequestedRef != "main" || acquired[0].ResolvedRevision == "" {
		t.Fatalf("acquired = %+v", acquired)
	}
	if len(manager.requests) != 1 || manager.requests[0].Purpose != "pipeline-run" {
		t.Fatalf("requests = %+v", manager.requests)
	}
	if _, statErr := os.Stat(manager.paths[0]); !os.IsNotExist(statErr) {
		t.Fatalf("source session was not cleaned: %v", statErr)
	}
}
