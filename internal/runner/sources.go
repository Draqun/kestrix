package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"kestrix/internal/domain"
	"kestrix/internal/model"
	"kestrix/internal/source"
)

var (
	sourceAbsPath   = filepath.Abs
	sourceMkdirTemp = os.MkdirTemp
	sourceChmod     = os.Chmod
)

// prepareSources acquires all repositories before DAG resolution. Remote
// sources share a private per-run root; local sources retain their existing
// checkout paths and are only validated.
func prepareSources(ctx context.Context, project model.Project, opts *Options) (model.Project, func(), error) {
	if opts.SourcesPrepared {
		return project, func() {}, nil
	}
	// Project values are passed by value, but their repository slice may still
	// be shared with concurrent runs. Copy it before rewriting remote paths.
	project.Repos = append([]model.Repo(nil), project.Repos...)
	baseDir := opts.WorkDir
	if baseDir == "" {
		baseDir = "."
	}
	baseDir, err := sourceAbsPath(baseDir)
	if err != nil {
		return model.Project{}, func() {}, fmt.Errorf("resolve source base directory: %w", err)
	}
	hybrid := &source.HybridAcquirer{}
	var acquirer domain.SourceAcquirer = hybrid
	if opts.SourceAcquirer != nil {
		acquirer = opts.SourceAcquirer
	}
	workspace := ""
	if opts.SourceSessions == nil {
		for _, repo := range project.Repos {
			if repo.URL == "" {
				continue
			}
			workspace, err = sourceMkdirTemp("", "kestrix-source-")
			if err != nil {
				return model.Project{}, func() {}, fmt.Errorf("create source workspace: %w", err)
			}
			if err := sourceChmod(workspace, 0o700); err != nil {
				_ = os.RemoveAll(workspace)
				return model.Project{}, func() {}, fmt.Errorf("secure source workspace: %w", err)
			}
			break
		}
	}
	var cleanupOnce sync.Once
	cleanups := make([]func(), 0, len(project.Repos)+1)
	cleanup := func() {
		cleanupOnce.Do(func() {
			for i := len(cleanups) - 1; i >= 0; i-- {
				cleanups[i]()
			}
			if workspace != "" && !opts.RetainSourceWorkspace {
				_ = os.RemoveAll(workspace)
			}
		})
	}
	if opts.RepoPaths == nil {
		opts.RepoPaths = make(map[string]string, len(project.Repos))
	}
	acquired := make([]domain.AcquiredSource, 0, len(project.Repos))
	for i := range project.Repos {
		repo := project.Repos[i]
		var result domain.AcquiredSource
		if repo.URL != "" && opts.SourceSessions != nil {
			session, openErr := opts.SourceSessions.Open(ctx, domain.SourceRequest{
				RepoName: repo.Name, URL: repo.URL, RequestedRef: repo.Ref,
				CredentialRef: repo.CredentialRef, Purpose: "pipeline-run",
			})
			if openErr != nil {
				cleanup()
				return model.Project{}, func() {}, openErr
			}
			result = session.Source
			if session.Cleanup != nil {
				cleanups = append(cleanups, session.Cleanup)
			}
		} else {
			result, err = acquirer.Acquire(ctx, repo, domain.AcquisitionOptions{
				Workspace:    workspace,
				BaseDir:      baseDir,
				Timeout:      opts.SourceTimeout,
				Retries:      opts.SourceRetries,
				RetainOnFail: opts.RetainSourceWorkspace,
				Resolver:     opts.CredentialResolver,
			})
		}
		if err != nil {
			cleanup()
			return model.Project{}, func() {}, err
		}
		acquired = append(acquired, result)
		if repo.URL != "" {
			opts.RepoPaths[repo.Name] = result.Path
		}
	}
	if opts.OnSourcesAcquired != nil {
		if err := opts.OnSourcesAcquired(acquired); err != nil {
			cleanup()
			return model.Project{}, func() {}, err
		}
	}
	return project, cleanup, nil
}

func repoDirFor(workDir string, repo model.Repo, paths map[string]string) (string, error) {
	if path, ok := paths[repo.Name]; ok {
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("repo path %q: %w", repo.Path, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("repo path %q is not a directory", repo.Path)
		}
		return path, nil
	}
	return repoDir(workDir, repo)
}

func pathWithin(root, path string) bool {
	root, err := sourceAbsPath(root)
	if err != nil {
		return false
	}
	path, err = sourceAbsPath(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func cleanupWorkspace(opts *Options) {
	if opts.WorkspaceCleanup != nil {
		cleanup := opts.WorkspaceCleanup
		opts.WorkspaceCleanup = nil
		cleanup()
	}
}
