package service

import (
	"slices"
	"strings"

	"kestrix/internal/domain"
	"kestrix/internal/model"
	"kestrix/internal/source"
)

// DeriveSources projects the repositories a project listens to from data
// Kestrix already owns: the registration's repo_url (primary) and every
// ci_repo in the parsed pipeline that declares a url. Path-only repos live
// inside the primary checkout and are not sources of their own. Two URLs for
// the same repository collapse to the one declared first in the pipeline - the
// file's order decides that, not the name, and it happens before the sort
// below; a URL that has no valid match key is skipped, because the store could
// not route on it anyway.
//
// The result is in the store's own order - primary first, then by name, then
// by URL - rather than in pipeline declaration order. That makes the derived
// set directly comparable with what the store returns, so a caller can tell an
// unchanged definition from a changed one (see the web package's
// refreshSources) and a create/update response lists sources the same way a
// later GET does.
func DeriveSources(cfg *domain.ProjectConfig, proj model.Project) []domain.ProjectSource {
	// primaryRank sorts the primary source ahead of every secondary one,
	// mirroring the store's "is_primary DESC".
	primaryRank := func(s domain.ProjectSource) int {
		if s.Primary {
			return 0
		}
		return 1
	}
	seen := make(map[string]bool)
	add := func(out []domain.ProjectSource, src domain.ProjectSource) []domain.ProjectSource {
		key, err := source.SourceMatchKey(src.URL)
		if err != nil || seen[key] {
			return out
		}
		seen[key] = true
		return append(out, src)
	}
	sources := add(nil, domain.ProjectSource{Name: cfg.Name, URL: cfg.RepoURL, Primary: true})
	for _, repo := range proj.Repos {
		if repo.URL == "" {
			continue
		}
		sources = add(sources, domain.ProjectSource{Name: repo.Name, URL: repo.URL})
	}
	slices.SortFunc(sources, func(a, b domain.ProjectSource) int {
		if c := primaryRank(a) - primaryRank(b); c != 0 {
			return c
		}
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.URL, b.URL)
	})
	return sources
}
