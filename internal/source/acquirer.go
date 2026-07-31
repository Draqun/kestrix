// Package source acquires local and remote Git repositories for a pipeline run.
package source

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kestrix/internal/domain"
)

const defaultGitTimeout = 5 * time.Minute

// LocalAcquirer resolves repositories that already exist below BaseDir.
type LocalAcquirer struct{}

var _ domain.SourceAcquirer = (*LocalAcquirer)(nil)

func (a *LocalAcquirer) Acquire(_ context.Context, repo domain.Repo, opts domain.AcquisitionOptions) (domain.AcquiredSource, error) {
	if strings.TrimSpace(repo.Path) == "" {
		return domain.AcquiredSource{}, fmt.Errorf("repository %q has no local path", repo.Name)
	}
	base := opts.BaseDir
	if base == "" {
		base = "."
	}
	base, err := filepath.Abs(base)
	if err != nil {
		return domain.AcquiredSource{}, fmt.Errorf("resolve local source root: %w", err)
	}
	dir := filepath.Clean(filepath.Join(base, repo.Path))
	rel, err := filepath.Rel(base, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return domain.AcquiredSource{}, fmt.Errorf("repository path %q escapes source root", repo.Path)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return domain.AcquiredSource{}, fmt.Errorf("repository path %q: %w", repo.Path, err)
	}
	if !info.IsDir() {
		return domain.AcquiredSource{}, fmt.Errorf("repository path %q is not a directory", repo.Path)
	}
	return domain.AcquiredSource{RepoName: repo.Name, Path: dir, URL: repo.URL, RequestedRef: repo.Ref}, nil
}

// HybridAcquirer selects the local or Git adapter from the declaration.
type HybridAcquirer struct {
	Local *LocalAcquirer
	Git   *GitAcquirer
}

var _ domain.SourceAcquirer = (*HybridAcquirer)(nil)

func (a *HybridAcquirer) Acquire(ctx context.Context, repo domain.Repo, opts domain.AcquisitionOptions) (domain.AcquiredSource, error) {
	if repo.URL != "" {
		git := a.Git
		if git == nil {
			git = &GitAcquirer{}
		}
		return git.Acquire(ctx, repo, opts)
	}
	local := a.Local
	if local == nil {
		local = &LocalAcquirer{}
	}
	return local.Acquire(ctx, repo, opts)
}

// GitAcquirer obtains remote repositories using the installed git executable.
// Command is injectable for tests and defaults to "git".
type GitAcquirer struct {
	Command  string
	Resolver domain.CredentialResolver
}

var _ domain.SourceAcquirer = (*GitAcquirer)(nil)

func (a *GitAcquirer) Acquire(ctx context.Context, repo domain.Repo, opts domain.AcquisitionOptions) (domain.AcquiredSource, error) {
	if strings.TrimSpace(repo.URL) == "" {
		return domain.AcquiredSource{}, fmt.Errorf("repository %q has no remote URL", repo.Name)
	}
	if strings.TrimSpace(repo.Ref) == "" {
		return domain.AcquiredSource{}, fmt.Errorf("repository %q has no remote ref", repo.Name)
	}
	if hasEmbeddedCredentials(repo.URL) {
		return domain.AcquiredSource{}, fmt.Errorf("repository %q URL contains embedded credentials", repo.Name)
	}
	if strings.TrimSpace(opts.Workspace) == "" {
		return domain.AcquiredSource{}, fmt.Errorf("repository %q acquisition workspace is required", repo.Name)
	}
	if err := os.MkdirAll(opts.Workspace, 0o700); err != nil {
		return domain.AcquiredSource{}, fmt.Errorf("create source workspace: %w", err)
	}
	if err := os.Chmod(opts.Workspace, 0o700); err != nil {
		return domain.AcquiredSource{}, fmt.Errorf("secure source workspace: %w", err)
	}

	command := a.Command
	if command == "" {
		command = "git"
	}
	resolver := opts.Resolver
	if resolver == nil {
		resolver = a.Resolver
	}
	var credential domain.Credential
	var err error
	if repo.CredentialRef != "" {
		if resolver == nil {
			return domain.AcquiredSource{}, fmt.Errorf("credential reference %q cannot be resolved", repo.CredentialRef)
		}
		credential, err = resolver.Resolve(ctx, repo.CredentialRef)
		if err != nil {
			return domain.AcquiredSource{}, fmt.Errorf("resolve credential %q: %w", repo.CredentialRef, err)
		}
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultGitTimeout
	}
	attempts := opts.Retries + 1
	if attempts < 1 {
		attempts = 1
	}
	destination := filepath.Join(opts.Workspace, repo.Name)
	for attempt := 1; attempt <= attempts; attempt++ {
		result, runErr := a.acquireOnce(ctx, command, repo, destination, timeout, credential)
		if runErr == nil {
			return result, nil
		}
		if !opts.RetainOnFail {
			_ = os.RemoveAll(destination)
		}
		if ctx.Err() != nil {
			return domain.AcquiredSource{}, fmt.Errorf("acquire repository %q: %w", repo.Name, ctx.Err())
		}
		if attempt == attempts {
			return domain.AcquiredSource{}, fmt.Errorf("acquire repository %q: %w", repo.Name, runErr)
		}
	}
	return domain.AcquiredSource{}, fmt.Errorf("acquire repository %q failed", repo.Name)
}
