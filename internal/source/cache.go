package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"kestrix/internal/config"
	"kestrix/internal/domain"
)

const cacheMetadataName = "metadata.json"

// CacheOptions configures the persistent mirror cache.
type CacheOptions struct {
	Root     string
	MaxAge   time.Duration
	MaxBytes int64
	Command  string
	Resolver domain.CredentialResolver
}

// GitCache implements provider-neutral source sessions using private bare Git
// mirrors. Mirrors are shared; writable checkouts never are.
type GitCache struct {
	root     string
	maxAge   time.Duration
	maxBytes int64
	command  string
	resolver domain.CredentialResolver
	now      func() time.Time
	// maintenance tracks the goroutines started by maintainAsync so a caller
	// that is about to delete the cache root can wait for them; see
	// WaitForMaintenance. maintenanceMu keeps Add and Wait apart: a WaitGroup
	// may not take a positive delta at zero while a Wait is in flight, and
	// Open can start maintenance at any moment.
	maintenance   sync.WaitGroup
	maintenanceMu sync.RWMutex
}

type cacheMetadata struct {
	URL           string    `json:"url"`
	CredentialRef string    `json:"credential_ref,omitempty"`
	LastUsed      time.Time `json:"last_used"`
}

type cacheEntry struct {
	key      string
	path     string
	metadata cacheMetadata
	size     int64
}

var _ domain.SourceSessionManager = (*GitCache)(nil)

// NewGitCache validates and creates a private mirror root.
func NewGitCache(opts CacheOptions) (*GitCache, error) {
	if strings.TrimSpace(opts.Root) == "" {
		return nil, fmt.Errorf("source cache directory is required")
	}
	if opts.MaxAge <= 0 {
		return nil, fmt.Errorf("source cache max age must be positive")
	}
	if opts.MaxBytes <= 0 {
		return nil, fmt.Errorf("source cache max bytes must be positive")
	}
	if err := os.MkdirAll(opts.Root, 0o700); err != nil {
		return nil, fmt.Errorf("create source cache: %w", err)
	}
	info, err := os.Stat(opts.Root)
	if err != nil {
		return nil, fmt.Errorf("inspect source cache: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("source cache path is not a directory")
	}
	if err := os.Chmod(opts.Root, 0o700); err != nil {
		return nil, fmt.Errorf("secure source cache: %w", err)
	}
	command := opts.Command
	if command == "" {
		command = "git"
	}
	return &GitCache{root: opts.Root, maxAge: opts.MaxAge, maxBytes: opts.MaxBytes, command: command, resolver: opts.Resolver, now: time.Now}, nil
}

func canonicalSourceURL(raw string) (string, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "", fmt.Errorf("remote source URL is required")
	}
	if hasEmbeddedCredentials(clean) {
		return "", fmt.Errorf("remote source URL contains embedded credentials")
	}
	parsed, err := url.Parse(clean)
	if err != nil {
		return "", fmt.Errorf("invalid remote source URL")
	}
	if parsed.Scheme != "" {
		parsed.Scheme = strings.ToLower(parsed.Scheme)
		parsed.Host = strings.ToLower(parsed.Host)
		clean = parsed.String()
	}
	return strings.TrimSuffix(clean, "/"), nil
}

func sourceCacheKey(rawURL, credentialRef string) (string, string, error) {
	canonical, err := canonicalSourceURL(rawURL)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(canonical + "\x00" + strings.TrimSpace(credentialRef)))
	return hex.EncodeToString(sum[:]), canonical, nil
}

func safeRepoName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "project"
	}
	name = filepath.Base(filepath.Clean(name))
	if name == "." || name == string(filepath.Separator) {
		return "project"
	}
	return name
}

func (c *GitCache) Open(ctx context.Context, request domain.SourceRequest) (domain.SourceSession, error) {
	key, canonical, err := sourceCacheKey(request.URL, request.CredentialRef)
	if err != nil {
		return domain.SourceSession{}, err
	}
	requested := strings.TrimSpace(request.RequestedRef)
	revision := strings.TrimSpace(request.Revision)
	if revision == "" {
		revision = requested
	}
	if revision == "" {
		return domain.SourceSession{}, fmt.Errorf("remote source ref is required")
	}

	credential, err := c.resolveCredential(ctx, request.CredentialRef)
	if err != nil {
		return domain.SourceSession{}, err
	}
	lock, err := acquireCacheLock(ctx, filepath.Join(c.root, key+".lock"), false)
	if err != nil {
		return domain.SourceSession{}, fmt.Errorf("lock source cache entry: %w", err)
	}
	defer lock.release()

	entryPath := filepath.Join(c.root, key)
	mirrorPath, err := c.refreshEntry(ctx, entryPath, canonical, request.CredentialRef, credential)
	if err != nil {
		return domain.SourceSession{}, err
	}
	sha, err := c.resolveRevision(ctx, mirrorPath, revision)
	if err != nil {
		return domain.SourceSession{}, err
	}
	sessionRoot, err := os.MkdirTemp("", "kestrix-source-session-")
	if err != nil {
		return domain.SourceSession{}, fmt.Errorf("create source session: %w", err)
	}
	cleanup := onceCleanup(sessionRoot)
	if err := os.Chmod(sessionRoot, 0o700); err != nil {
		cleanup()
		return domain.SourceSession{}, fmt.Errorf("secure source session: %w", err)
	}
	destination := filepath.Join(sessionRoot, safeRepoName(request.RepoName))
	if _, err := c.git(ctx, nil, "clone", "--quiet", "--no-checkout", mirrorPath, destination); err != nil {
		cleanup()
		return domain.SourceSession{}, fmt.Errorf("materialize cached source: %w", err)
	}
	if _, err := c.git(ctx, nil, "-C", destination, "remote", "set-url", "origin", canonical); err != nil {
		cleanup()
		return domain.SourceSession{}, fmt.Errorf("configure source remote: %w", err)
	}
	if _, err := c.git(ctx, nil, "-C", destination, "checkout", "--quiet", "--detach", "--force", sha); err != nil {
		cleanup()
		return domain.SourceSession{}, fmt.Errorf("checkout resolved source: %w", err)
	}
	if err := c.writeMetadata(entryPath, cacheMetadata{URL: canonical, CredentialRef: request.CredentialRef, LastUsed: c.now().UTC()}); err != nil {
		cleanup()
		return domain.SourceSession{}, err
	}
	lock.release()
	if request.Purpose != "" && !strings.Contains(request.Purpose, "run") && request.Purpose != "manual-approval" {
		c.maintainAsync()
	}

	source := domain.AcquiredSource{
		RepoName: safeRepoName(request.RepoName), URL: canonical, Path: destination,
		RequestedRef: requested, ResolvedRevision: sha, CredentialRef: request.CredentialRef,
		Primary: request.Primary,
	}
	return domain.SourceSession{Source: source, Cleanup: cleanup}, nil
}

func onceCleanup(path string) func() {
	var once sync.Once
	return func() {
		once.Do(func() { _ = os.RemoveAll(path) })
	}
}

func pinSourceName(name string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(name)))
	return hex.EncodeToString(sum[:8])
}

func (c *GitCache) resolveCredential(ctx context.Context, ref string) (domain.Credential, error) {
	if strings.TrimSpace(ref) == "" {
		return domain.Credential{}, nil
	}
	if c.resolver == nil {
		return domain.Credential{}, fmt.Errorf("credential reference %q cannot be resolved", ref)
	}
	credential, err := c.resolver.Resolve(ctx, ref)
	if err != nil {
		return domain.Credential{}, fmt.Errorf("resolve credential %q: %w", ref, err)
	}
	return credential, nil
}

func (c *GitCache) refreshEntry(ctx context.Context, entryPath, canonical, credentialRef string, credential domain.Credential) (string, error) {
	mirrorPath := filepath.Join(entryPath, "mirror.git")
	if _, err := os.Stat(mirrorPath); err == nil {
		output, validationErr := c.git(ctx, nil, "-C", mirrorPath, "rev-parse", "--is-bare-repository")
		if validationErr == nil && strings.TrimSpace(string(output)) == "true" {
			_, validationErr = c.git(ctx, nil, "-C", mirrorPath, "fsck", "--connectivity-only", "--no-dangling")
		}
		if validationErr != nil && ctx.Err() != nil {
			return "", ctx.Err()
		}
		if validationErr != nil || strings.TrimSpace(string(output)) != "true" {
			quarantine := entryPath + ".corrupt-" + strconv.FormatInt(c.now().UnixNano(), 10)
			if err := os.Rename(entryPath, quarantine); err != nil {
				return "", fmt.Errorf("quarantine corrupt source cache: %w", err)
			}
			defer os.RemoveAll(quarantine)
			return c.seedEntry(ctx, entryPath, canonical, credentialRef, credential)
		}
		if err := c.fetch(ctx, mirrorPath, canonical, credential); err != nil {
			return "", err
		}
		return mirrorPath, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect source cache entry: %w", err)
	}
	return c.seedEntry(ctx, entryPath, canonical, credentialRef, credential)
}

func (c *GitCache) seedEntry(ctx context.Context, entryPath, canonical, credentialRef string, credential domain.Credential) (string, error) {
	temporary, err := os.MkdirTemp(c.root, ".seed-")
	if err != nil {
		return "", fmt.Errorf("create cache seed: %w", err)
	}
	defer os.RemoveAll(temporary)
	if err := os.Chmod(temporary, 0o700); err != nil {
		return "", fmt.Errorf("secure cache seed: %w", err)
	}
	mirrorPath := filepath.Join(temporary, "mirror.git")
	if _, err := c.git(ctx, nil, "init", "--quiet", "--bare", mirrorPath); err != nil {
		return "", fmt.Errorf("initialize source cache: %w", err)
	}
	if _, err := c.git(ctx, nil, "-C", mirrorPath, "remote", "add", "origin", canonical); err != nil {
		return "", fmt.Errorf("configure source cache remote: %w", err)
	}
	if err := c.fetch(ctx, mirrorPath, canonical, credential); err != nil {
		return "", err
	}
	if err := c.writeMetadata(temporary, cacheMetadata{URL: canonical, CredentialRef: credentialRef, LastUsed: c.now().UTC()}); err != nil {
		return "", err
	}
	if err := os.Rename(temporary, entryPath); err != nil {
		return "", fmt.Errorf("publish source cache entry: %w", err)
	}
	return filepath.Join(entryPath, "mirror.git"), nil
}

func (c *GitCache) fetch(ctx context.Context, mirrorPath, canonical string, credential domain.Credential) error {
	env, cleanup, err := credentialEnvironment(credential)
	if err != nil {
		return err
	}
	defer cleanup()
	if _, err := c.git(ctx, env, "-C", mirrorPath, "fetch", "--quiet", "--force", "--prune", "origin", "+refs/heads/*:refs/remotes/origin/*", "+refs/tags/*:refs/tags/*"); err != nil {
		return fmt.Errorf("refresh remote source %s: %w", redactURL(canonical), err)
	}
	return nil
}

func (c *GitCache) resolveRevision(ctx context.Context, mirrorPath, revision string) (string, error) {
	if strings.HasPrefix(revision, "-") || strings.ContainsAny(revision, "\x00\n\r") {
		return "", fmt.Errorf("invalid source revision")
	}
	candidates := []string{revision + "^{commit}"}
	if !strings.HasPrefix(revision, "refs/") {
		candidates = append([]string{"refs/remotes/origin/" + revision + "^{commit}", "refs/tags/" + revision + "^{commit}"}, candidates...)
	}
	for _, candidate := range candidates {
		output, err := c.git(ctx, nil, "-C", mirrorPath, "rev-parse", "--verify", candidate)
		if err == nil {
			sha := strings.TrimSpace(string(output))
			if len(sha) >= 7 {
				return sha, nil
			}
		}
	}
	return "", fmt.Errorf("requested source revision %q does not exist", revision)
}

func (c *GitCache) git(ctx context.Context, extraEnv []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.command, args...)
	cmd.Env = append(config.Current().Environ(), extraEnv...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("git command failed: %s", redactGitOutput(string(output)))
	}
	return output, nil
}

func redactURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "[remote]"
	}
	if parsed.User != nil {
		username := parsed.User.Username()
		if parsed.Scheme == "ssh" && username != "" {
			parsed.User = url.User(username)
		} else {
			parsed.User = nil
		}
	}
	return parsed.String()
}

func (c *GitCache) writeMetadata(entryPath string, metadata cacheMetadata) error {
	data, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode source cache metadata: %w", err)
	}
	temporary, err := os.CreateTemp(entryPath, ".metadata-")
	if err != nil {
		return fmt.Errorf("create source cache metadata: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("secure source cache metadata: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write source cache metadata: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close source cache metadata: %w", err)
	}
	if err := os.Rename(temporaryPath, filepath.Join(entryPath, cacheMetadataName)); err != nil {
		return fmt.Errorf("publish source cache metadata: %w", err)
	}
	return nil
}

type cacheLock struct{ file *os.File }

func acquireCacheLock(ctx context.Context, path string, nonBlocking bool) (*cacheLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &cacheLock{file: file}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			file.Close()
			return nil, err
		}
		if nonBlocking {
			file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (l *cacheLock) release() {
	if l == nil || l.file == nil {
		return
	}
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	_ = l.file.Close()
	l.file = nil
}

func (c *GitCache) maintainAsync() {
	c.maintenanceMu.RLock()
	c.maintenance.Add(1)
	c.maintenanceMu.RUnlock()
	go func() {
		defer c.maintenance.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = c.Maintain(ctx)
	}()
}

// WaitForMaintenance blocks until the background maintenance Open may have
// started has finished. Maintain deletes cache entries, so a caller about to
// remove the cache root has to wait for it or race the removal against it.
//
// It covers that goroutine and nothing else. Open starts maintenance only for
// purposes that are not part of a run (see the call site), yet every Open
// takes an entry lock, writes metadata and clones the mirror - so a caller
// with run Opens still in flight has to wait for those separately. This is not
// on its own a barrier before removing the root; the e2e test pairs it with a
// signal for its own Release.
//
// An Open that starts maintenance while this is waiting blocks until it
// returns, so the WaitGroup's "Add at zero during Wait" rule cannot be
// tripped.
func (c *GitCache) WaitForMaintenance() {
	c.maintenanceMu.Lock()
	defer c.maintenanceMu.Unlock()
	c.maintenance.Wait()
}

func (c *GitCache) Pin(ctx context.Context, runID int, sources []domain.AcquiredSource) error {
	if runID <= 0 {
		return fmt.Errorf("run ID must be positive")
	}
	for _, source := range sources {
		if strings.TrimSpace(source.URL) == "" {
			continue
		}
		key, _, err := sourceCacheKey(source.URL, source.CredentialRef)
		if err != nil {
			return err
		}
		lock, err := acquireCacheLock(ctx, filepath.Join(c.root, key+".lock"), false)
		if err != nil {
			return err
		}
		mirrorPath := filepath.Join(c.root, key, "mirror.git")
		ref := fmt.Sprintf("refs/kestrix/runs/%d/%s", runID, pinSourceName(source.RepoName))
		_, updateErr := c.git(ctx, nil, "-C", mirrorPath, "update-ref", ref, source.ResolvedRevision)
		lock.release()
		if updateErr != nil {
			return fmt.Errorf("pin source %q: %w", source.RepoName, updateErr)
		}
	}
	return nil
}

func (c *GitCache) Release(ctx context.Context, runID int) error {
	prefix := fmt.Sprintf("refs/kestrix/runs/%d/", runID)
	entries, err := c.entries()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		lock, err := acquireCacheLock(ctx, filepath.Join(c.root, entry.key+".lock"), false)
		if err != nil {
			return err
		}
		mirrorPath := filepath.Join(entry.path, "mirror.git")
		output, listErr := c.git(ctx, nil, "-C", mirrorPath, "for-each-ref", "--format=%(refname)", prefix)
		if listErr == nil {
			for _, ref := range strings.Fields(string(output)) {
				_, listErr = c.git(ctx, nil, "-C", mirrorPath, "update-ref", "-d", ref)
				if listErr != nil {
					break
				}
			}
		}
		lock.release()
		if listErr != nil {
			return fmt.Errorf("release run source pins: %w", listErr)
		}
	}
	return nil
}

func (c *GitCache) Maintain(ctx context.Context) error {
	entries, err := c.entries()
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].metadata.LastUsed.Before(entries[j].metadata.LastUsed) })
	var total int64
	for _, entry := range entries {
		total += entry.size
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		expired := c.now().Sub(entry.metadata.LastUsed) > c.maxAge
		oversize := total > c.maxBytes
		if !expired && !oversize {
			continue
		}
		lock, err := acquireCacheLock(ctx, filepath.Join(c.root, entry.key+".lock"), true)
		if err != nil {
			continue
		}
		pinned, pinErr := c.entryPinned(ctx, entry.path)
		if pinErr == nil && !pinned {
			if err := os.RemoveAll(entry.path); err != nil {
				lock.release()
				return fmt.Errorf("evict source cache entry: %w", err)
			}
			total -= entry.size
		}
		lock.release()
		if pinErr != nil {
			return pinErr
		}
	}
	return nil
}

func (c *GitCache) entryPinned(ctx context.Context, entryPath string) (bool, error) {
	output, err := c.git(ctx, nil, "-C", filepath.Join(entryPath, "mirror.git"), "for-each-ref", "--format=%(refname)", "refs/kestrix/runs/")
	if err != nil {
		return false, fmt.Errorf("inspect source cache pins: %w", err)
	}
	return strings.TrimSpace(string(output)) != "", nil
}

func (c *GitCache) entries() ([]cacheEntry, error) {
	dirs, err := os.ReadDir(c.root)
	if err != nil {
		return nil, fmt.Errorf("list source cache: %w", err)
	}
	entries := make([]cacheEntry, 0, len(dirs))
	for _, dir := range dirs {
		if !dir.IsDir() || strings.HasPrefix(dir.Name(), ".") || strings.Contains(dir.Name(), ".corrupt-") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(c.root, dir.Name(), cacheMetadataName))
		if err != nil {
			continue
		}
		var metadata cacheMetadata
		if json.Unmarshal(data, &metadata) != nil {
			continue
		}
		path := filepath.Join(c.root, dir.Name())
		size, err := treeSize(path)
		if err != nil {
			return nil, err
		}
		entries = append(entries, cacheEntry{key: dir.Name(), path: path, metadata: metadata, size: size})
	}
	return entries, nil
}

func treeSize(root string) (int64, error) {
	var size int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			size += info.Size()
		}
		return nil
	})
	return size, err
}
