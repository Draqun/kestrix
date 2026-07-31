package source

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"kestrix/internal/domain"
)

type cacheCredentialResolver struct {
	mu     sync.Mutex
	calls  int
	secret string
	err    error
}

func (r *cacheCredentialResolver) Resolve(context.Context, string) (domain.Credential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.err != nil {
		return domain.Credential{}, r.err
	}
	return domain.Credential{Type: "token", Username: "git", Token: r.secret}, nil
}

func newTestCache(t *testing.T, resolver domain.CredentialResolver) *GitCache {
	t.Helper()
	cache, err := NewGitCache(CacheOptions{Root: t.TempDir(), MaxAge: time.Hour, MaxBytes: 1 << 30, Resolver: resolver})
	if err != nil {
		t.Fatal(err)
	}
	return cache
}

func advanceBareRepository(t *testing.T, root string) string {
	t.Helper()
	work := filepath.Join(root, "work")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, work, "add", ".")
	gitTest(t, work, "-c", "user.email=test@example.test", "-c", "user.name=Test", "commit", "-m", "two")
	sha := gitTest(t, work, "rev-parse", "HEAD")
	gitTest(t, work, "push", "origin", "main")
	return sha
}

func TestGitCacheSeedsRefreshesAndResolvesBranchTagCommit(t *testing.T) {
	bare, first, root := createBareRepository(t)
	cache := newTestCache(t, nil)

	for _, ref := range []string{"main", "v1", first} {
		session, err := cache.Open(context.Background(), domain.SourceRequest{RepoName: "app", URL: bare, RequestedRef: ref})
		if err != nil {
			t.Fatalf("Open(%s): %v", ref, err)
		}
		if session.Source.ResolvedRevision != first || session.Source.RequestedRef != ref {
			t.Fatalf("source=%+v", session.Source)
		}
		if _, err := os.Stat(filepath.Join(session.Source.Path, "README.md")); err != nil {
			t.Fatal(err)
		}
		session.Cleanup()
		session.Cleanup()
	}

	second := advanceBareRepository(t, root)
	session, err := cache.Open(context.Background(), domain.SourceRequest{RepoName: "app", URL: bare, RequestedRef: "main"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Cleanup()
	if session.Source.ResolvedRevision != second {
		t.Fatalf("resolved=%s want=%s", session.Source.ResolvedRevision, second)
	}
	entries, err := cache.entries()
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%+v err=%v", entries, err)
	}
}

func TestGitCacheRefreshFailureDoesNotUseStaleBranch(t *testing.T) {
	bare, _, _ := createBareRepository(t)
	cache := newTestCache(t, nil)
	session, err := cache.Open(context.Background(), domain.SourceRequest{RepoName: "app", URL: bare, RequestedRef: "main"})
	if err != nil {
		t.Fatal(err)
	}
	session.Cleanup()
	unavailable := bare + ".unavailable"
	if err := os.Rename(bare, unavailable); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Open(context.Background(), domain.SourceRequest{RepoName: "app", URL: bare, RequestedRef: "main"}); err == nil {
		t.Fatal("expected refresh failure instead of stale checkout")
	}
}

func TestGitCacheIdentityMetadataAndRunJSONContainNoSecretsOrPaths(t *testing.T) {
	bare, _, _ := createBareRepository(t)
	resolver := &cacheCredentialResolver{secret: "super-secret-token"}
	cache := newTestCache(t, resolver)
	session, err := cache.Open(context.Background(), domain.SourceRequest{RepoName: "app", URL: bare, RequestedRef: "main", CredentialRef: "cred-private"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Cleanup()

	data, err := json.Marshal(session.Source)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), session.Source.Path) || strings.Contains(string(data), "super-secret-token") {
		t.Fatalf("source JSON leaked path or secret: %s", data)
	}
	if !strings.Contains(string(data), "cred-private") {
		t.Fatalf("source JSON omitted non-secret identity: %s", data)
	}
	err = filepath.Walk(cache.root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		contents, readErr := os.ReadFile(path)
		if readErr == nil && strings.Contains(string(contents), "super-secret-token") {
			t.Fatalf("cache file %s contains credential payload", path)
		}
		return readErr
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGitCacheConcurrentSessionsAreIsolated(t *testing.T) {
	bare, _, _ := createBareRepository(t)
	cache := newTestCache(t, nil)
	type result struct {
		session domain.SourceSession
		err     error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			s, err := cache.Open(context.Background(), domain.SourceRequest{RepoName: "app", URL: bare, RequestedRef: "main"})
			results <- result{s, err}
		}()
	}
	first := <-results
	second := <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("errors=%v,%v", first.err, second.err)
	}
	defer first.session.Cleanup()
	defer second.session.Cleanup()
	if first.session.Source.Path == second.session.Source.Path {
		t.Fatal("concurrent sessions shared a checkout")
	}
	if err := os.WriteFile(filepath.Join(first.session.Source.Path, "only-first"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(second.session.Source.Path, "only-first")); !os.IsNotExist(err) {
		t.Fatalf("workspace mutation leaked: %v", err)
	}
}

func TestCacheLockWaiterHonorsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "entry.lock")
	first, err := acquireCacheLock(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer first.release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := acquireCacheLock(ctx, path, false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock error=%v", err)
	}
}

func TestCacheLockForOneSourceDoesNotBlockIndependentSource(t *testing.T) {
	firstBare, _, _ := createBareRepository(t)
	secondBare, _, _ := createBareRepository(t)
	cache := newTestCache(t, nil)
	firstKey, _, err := sourceCacheKey(firstBare, "")
	if err != nil {
		t.Fatal(err)
	}
	lock, err := acquireCacheLock(context.Background(), filepath.Join(cache.root, firstKey+".lock"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.release()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	session, err := cache.Open(ctx, domain.SourceRequest{RepoName: "other", URL: secondBare, RequestedRef: "main"})
	if err != nil {
		t.Fatalf("independent source was blocked: %v", err)
	}
	session.Cleanup()
}

func TestGitCacheRebuildsCorruptEntryOnce(t *testing.T) {
	bare, first, _ := createBareRepository(t)
	cache := newTestCache(t, nil)
	session, err := cache.Open(context.Background(), domain.SourceRequest{RepoName: "app", URL: bare, RequestedRef: "main"})
	if err != nil {
		t.Fatal(err)
	}
	session.Cleanup()
	entries, _ := cache.entries()
	objectPath := filepath.Join(entries[0].path, "mirror.git", "objects", first[:2], first[2:])
	if err := os.Chmod(objectPath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(objectPath, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := cache.Open(context.Background(), domain.SourceRequest{RepoName: "app", URL: bare, RequestedRef: "main"})
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	rebuilt.Cleanup()
}

func TestGitCacheCorruptEntryFailsCleanlyWhenRebuildFails(t *testing.T) {
	bare, first, _ := createBareRepository(t)
	cache := newTestCache(t, nil)
	session, err := cache.Open(context.Background(), domain.SourceRequest{RepoName: "app", URL: bare, RequestedRef: "main"})
	if err != nil {
		t.Fatal(err)
	}
	session.Cleanup()
	entries, _ := cache.entries()
	objectPath := filepath.Join(entries[0].path, "mirror.git", "objects", first[:2], first[2:])
	if err := os.Chmod(objectPath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(objectPath, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(bare, bare+".unavailable"); err != nil {
		t.Fatal(err)
	}

	if _, err := cache.Open(context.Background(), domain.SourceRequest{RepoName: "app", URL: bare, RequestedRef: "main"}); err == nil {
		t.Fatal("expected rebuild failure")
	}
	matches, err := filepath.Glob(filepath.Join(cache.root, "*.corrupt-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("quarantine was not cleaned: %v", matches)
	}
}

func TestGitCacheHitStillResolvesCredentialAndFailsClosed(t *testing.T) {
	bare, _, _ := createBareRepository(t)
	resolver := &cacheCredentialResolver{secret: "token"}
	cache := newTestCache(t, resolver)
	request := domain.SourceRequest{RepoName: "app", URL: bare, RequestedRef: "main", CredentialRef: "cred"}
	first, err := cache.Open(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	first.Cleanup()
	resolver.mu.Lock()
	resolver.err = errors.New("credential revoked")
	resolver.mu.Unlock()
	if _, err := cache.Open(context.Background(), request); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("cache hit error=%v", err)
	}
	resolver.mu.Lock()
	calls := resolver.calls
	resolver.mu.Unlock()
	if calls != 2 {
		t.Fatalf("credential resolve calls=%d want=2", calls)
	}
}

func TestGitCachePinsSurviveRestartAndProtectRetention(t *testing.T) {
	bare, _, _ := createBareRepository(t)
	root := t.TempDir()
	cache, err := NewGitCache(CacheOptions{Root: root, MaxAge: time.Nanosecond, MaxBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	session, err := cache.Open(context.Background(), domain.SourceRequest{RepoName: "app", URL: bare, RequestedRef: "main"})
	if err != nil {
		t.Fatal(err)
	}
	source := session.Source
	session.Cleanup()
	if err := cache.Pin(context.Background(), 42, []domain.AcquiredSource{source}); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewGitCache(CacheOptions{Root: root, MaxAge: time.Nanosecond, MaxBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = func() time.Time { return time.Now().Add(time.Hour) }
	if err := restarted.Maintain(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries, _ := restarted.entries()
	if len(entries) != 1 {
		t.Fatalf("pinned entry evicted: %+v", entries)
	}
	if err := restarted.Release(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Maintain(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries, _ = restarted.entries()
	if len(entries) != 0 {
		t.Fatalf("released entry retained: %+v", entries)
	}
}

func TestGitCacheRetentionUsesLRUAndSkipsLockedEntries(t *testing.T) {
	firstBare, _, _ := createBareRepository(t)
	secondBare, _, _ := createBareRepository(t)
	cache := newTestCache(t, nil)
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return clock }
	first, err := cache.Open(context.Background(), domain.SourceRequest{RepoName: "first", URL: firstBare, RequestedRef: "main"})
	if err != nil {
		t.Fatal(err)
	}
	first.Cleanup()
	clock = clock.Add(time.Hour)
	second, err := cache.Open(context.Background(), domain.SourceRequest{RepoName: "second", URL: secondBare, RequestedRef: "main"})
	if err != nil {
		t.Fatal(err)
	}
	second.Cleanup()
	entries, err := cache.entries()
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries=%+v err=%v", entries, err)
	}
	var newest cacheEntry
	for _, entry := range entries {
		if entry.metadata.URL == secondBare {
			newest = entry
		}
	}
	cache.maxBytes = newest.size
	cache.maxAge = 24 * time.Hour
	if err := cache.Maintain(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries, _ = cache.entries()
	if len(entries) != 1 || entries[0].metadata.URL != secondBare {
		t.Fatalf("LRU eviction retained wrong entries: %+v", entries)
	}

	key, _, _ := sourceCacheKey(secondBare, "")
	lock, err := acquireCacheLock(context.Background(), filepath.Join(cache.root, key+".lock"), false)
	if err != nil {
		t.Fatal(err)
	}
	cache.maxAge = time.Nanosecond
	clock = clock.Add(time.Hour)
	if err := cache.Maintain(context.Background()); err != nil {
		lock.release()
		t.Fatal(err)
	}
	lock.release()
	entries, _ = cache.entries()
	if len(entries) != 1 {
		t.Fatalf("locked entry was evicted: %+v", entries)
	}
}

func TestGitCacheMaintenanceReportsFilesystemFailure(t *testing.T) {
	cache := newTestCache(t, nil)
	root := cache.root
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cache.Maintain(context.Background()); err == nil {
		t.Fatal("expected cache maintenance filesystem error")
	}
}
