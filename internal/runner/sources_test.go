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

func TestPrepareSourcesAlreadyPreparedAndCallbackFailure(t *testing.T) {
	project := remoteProject()
	opts := Options{SourcesPrepared: true}
	got, cleanup, err := prepareSources(context.Background(), project, &opts)
	if err != nil || got.Name != project.Name {
		t.Fatalf("already prepared: project=%+v err=%v", got, err)
	}
	cleanup()

	opts = Options{
		SourceAcquirer: &recordingAcquirer{},
		OnSourcesAcquired: func([]domain.AcquiredSource) error {
			return errors.New("callback failed")
		},
		WorkDir: t.TempDir(),
	}
	_, cleanup, err = prepareSources(context.Background(), project, &opts)
	if err == nil || err.Error() != "callback failed" {
		t.Fatalf("callback error=%v", err)
	}
	cleanup()
}

func TestSourcePathHelpers(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "repo")
	if err := os.MkdirAll(inside, 0o700); err != nil {
		t.Fatal(err)
	}

	if !pathWithin(root, inside) || pathWithin(root, filepath.Join(root, "..", "outside")) {
		t.Fatal("pathWithin returned unexpected result")
	}
	if got, err := repoDirFor(root, model.Repo{Name: "repo"}, map[string]string{"repo": inside}); err != nil || got != inside {
		t.Fatalf("repoDirFor mapped path=%q err=%v", got, err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := repoDirFor(root, model.Repo{Name: "repo"}, map[string]string{"repo": file}); err == nil {
		t.Fatal("repoDirFor should reject a file")
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := repoDirFor(root, model.Repo{Name: "repo", Path: "sub"}, nil); err != nil || got != filepath.Join(root, "sub") {
		t.Fatalf("repoDirFor fallback=%q err=%v", got, err)
	}

	cleaned := false
	cleanupWorkspace(&Options{WorkspaceCleanup: func() { cleaned = true }})
	if !cleaned {
		t.Fatal("workspace cleanup callback was not called")
	}
}

func TestSourcePreparationErrorSeams(t *testing.T) {
	oldAbs, oldTemp, oldChmod := sourceAbsPath, sourceMkdirTemp, sourceChmod
	t.Cleanup(func() { sourceAbsPath, sourceMkdirTemp, sourceChmod = oldAbs, oldTemp, oldChmod })
	sourceAbsPath = func(string) (string, error) { return "", errors.New("abs") }
	if _, _, err := prepareSources(context.Background(), remoteProject(), &Options{WorkDir: t.TempDir()}); err == nil {
		t.Fatal("expected source base directory error")
	}

	sourceAbsPath = oldAbs
	sourceMkdirTemp = func(string, string) (string, error) { return "", errors.New("temp") }
	if _, _, err := prepareSources(context.Background(), remoteProject(), &Options{WorkDir: t.TempDir()}); err == nil {
		t.Fatal("expected source workspace error")
	}

	sourceMkdirTemp = oldTemp
	sourceChmod = func(string, os.FileMode) error { return errors.New("chmod") }
	if _, _, err := prepareSources(context.Background(), remoteProject(), &Options{WorkDir: t.TempDir()}); err == nil {
		t.Fatal("expected source workspace permissions error")
	}

	sourceChmod = oldChmod
	manager := &recordingSessions{failAt: 1}
	if _, _, err := prepareSources(context.Background(), remoteProject(), &Options{WorkDir: t.TempDir(), SourceSessions: manager}); err == nil {
		t.Fatal("expected source session error")
	}
	if _, _, err := prepareSources(context.Background(), remoteProject(), &Options{WorkDir: t.TempDir(), SourceAcquirer: &recordingAcquirer{fail: true}}); err == nil {
		t.Fatal("expected source acquisition error")
	}
}

func TestSourcePathHelperErrors(t *testing.T) {
	oldAbs := sourceAbsPath
	t.Cleanup(func() { sourceAbsPath = oldAbs })
	sourceAbsPath = func(string) (string, error) { return "", errors.New("abs") }
	if pathWithin(".", ".") {
		t.Fatal("pathWithin should reject root abs resolution errors")
	}
	calls := 0
	sourceAbsPath = func(path string) (string, error) {
		calls++
		if calls == 2 {
			return "", errors.New("abs")
		}
		return filepath.Abs(path)
	}
	if _, err := repoDirFor(".", model.Repo{Name: "repo"}, map[string]string{"repo": "any"}); err == nil {
		t.Fatal("expected mapped repo stat error")
	}
	if pathWithin(".", ".") {
		t.Fatal("pathWithin should reject abs resolution errors")
	}
}
