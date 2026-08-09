package web

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"kestrix/internal/domain"
)

// projectSourceSession is transient operation state. Its checkout path is
// never copied into project configuration or persisted run metadata.
type projectSourceSession struct {
	Root    string
	Source  domain.AcquiredSource
	Cleanup func()
}

func (s *Server) openRecordedRunSources(ctx context.Context, run *domain.Run) (string, map[string]string, func(), error) {
	if run == nil || run.PipelineDef == nil {
		return "", nil, func() {}, fmt.Errorf("run has no pipeline snapshot")
	}
	if s.sourceSessions == nil {
		return "", nil, func() {}, fmt.Errorf("source session manager is not configured")
	}
	paths := make(map[string]string)
	cleanups := make([]func(), 0, len(run.Sources))
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			for i := len(cleanups) - 1; i >= 0; i-- {
				cleanups[i]()
			}
		})
	}
	primaryRoot := ""
	for _, recorded := range run.Sources {
		if recorded.URL == "" {
			continue
		}
		session, err := s.sourceSessions.Open(ctx, domain.SourceRequest{
			RepoName: recorded.RepoName, URL: recorded.URL,
			RequestedRef: recorded.RequestedRef, Revision: recorded.ResolvedRevision,
			CredentialRef: recorded.CredentialRef, Purpose: "manual-approval",
			Primary: recorded.Primary,
		})
		if err != nil {
			cleanup()
			return "", nil, func() {}, err
		}
		if session.Cleanup != nil {
			cleanups = append(cleanups, session.Cleanup)
		}
		if recorded.Primary {
			primaryRoot = session.Source.Path
		} else {
			paths[recorded.RepoName] = session.Source.Path
		}
	}
	if primaryRoot == "" {
		cleanup()
		return "", nil, func() {}, fmt.Errorf("run has no recorded primary source")
	}
	return primaryRoot, paths, cleanup, nil
}

func (s *Server) openProjectSource(ctx context.Context, p *domain.ProjectConfig, revision, purpose string) (projectSourceSession, error) {
	if p == nil {
		return projectSourceSession{}, fmt.Errorf("project is required")
	}
	if strings.TrimSpace(p.RepoURL) == "" || strings.TrimSpace(p.RepoRef) == "" {
		return projectSourceSession{}, fmt.Errorf("project remote URL and ref are required")
	}
	if s.sourceSessions == nil {
		return projectSourceSession{}, fmt.Errorf("source session manager is not configured")
	}
	session, err := s.sourceSessions.Open(ctx, domain.SourceRequest{
		RepoName: p.Name, URL: p.RepoURL, RequestedRef: p.RepoRef,
		Revision: revision, CredentialRef: p.RepoCredentialRef,
		Purpose: purpose, Primary: true,
	})
	if err != nil {
		return projectSourceSession{}, err
	}
	cleanup := session.Cleanup
	if cleanup == nil {
		cleanup = func() {}
	}
	return projectSourceSession{Root: session.Source.Path, Source: session.Source, Cleanup: cleanup}, nil
}
