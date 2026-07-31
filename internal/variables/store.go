// Package variables implements a GitLab-like variable store with
// hierarchical scoping: project → repo → job → environment.
//
// Variables are resolved by priority (most specific wins):
//
//	job-specific > repo-specific > project-global
//
// Each variable carries a scope, optional environment filter, and a
// "protected" flag that restricts usage to protected branches.
package variables

import (
	"fmt"
	"sort"
	"strings"

	"kestrix/internal/config"
)

// Variable represents a single named value with metadata.
type Variable struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	Scope     Scope  `json:"scope"`
	Protected bool   `json:"protected"`
	Masked    bool   `json:"masked"`
	Expanded  bool   `json:"expanded"` // true if value was expanded from ${VAR}
}

// Scope determines where a variable is visible.
type Scope string

const (
	ScopeProject Scope = "project" // available to all jobs in the project
	ScopeRepo    Scope = "repo"    // available only to jobs in a specific repo
	ScopeJob     Scope = "job"     // available only to a specific job
)

// Store holds variables at all scopes. It is safe for concurrent read access
// but not for concurrent writes.
type Store struct {
	// project-level variables (global to all repos and jobs)
	project map[string]Variable
	// repo-level variables, keyed by repo name
	repo map[string]map[string]Variable
	// job-level variables, keyed by "repo/job"
	job map[string]map[string]Variable
	// environment filter: if set, variable is only active in matching env
	envFilter map[string]string // key → environment name
}

// New creates an empty variable store.
func New() *Store {
	return &Store{
		project:   make(map[string]Variable),
		repo:      make(map[string]map[string]Variable),
		job:       make(map[string]map[string]Variable),
		envFilter: make(map[string]string),
	}
}

// SetProject adds or updates a project-level variable.
func (s *Store) SetProject(key, value string, opts ...Option) {
	v := Variable{Key: key, Value: value, Scope: ScopeProject}
	for _, o := range opts {
		o(&v)
	}
	s.project[key] = v
}

// SetRepo adds or updates a repo-level variable.
func (s *Store) SetRepo(repo, key, value string, opts ...Option) {
	v := Variable{Key: key, Value: value, Scope: ScopeRepo}
	for _, o := range opts {
		o(&v)
	}
	if s.repo[repo] == nil {
		s.repo[repo] = make(map[string]Variable)
	}
	s.repo[repo][key] = v
}

// SetJob adds or updates a job-level variable.
func (s *Store) SetJob(repo, job, key, value string, opts ...Option) {
	v := Variable{Key: key, Value: value, Scope: ScopeJob}
	for _, o := range opts {
		o(&v)
	}
	k := repo + "/" + job
	if s.job[k] == nil {
		s.job[k] = make(map[string]Variable)
	}
	s.job[k][key] = v
}

// SetEnvFilter marks a variable as environment-specific.
// Only variables whose env matches the current environment will be resolved.
func (s *Store) SetEnvFilter(key, environment string) {
	s.envFilter[key] = environment
}

// Get resolves a variable for a specific repo/job/env context.
// Resolution order: job → repo → project. Most specific wins.
// If envFilter is set, the variable must match the current environment.
func (s *Store) Get(repo, job, env string) map[string]string {
	merged := make(map[string]string)

	// 1. Project-level (base)
	for k, v := range s.project {
		if !s.envMatch(k, env) {
			continue
		}
		merged[k] = v.Value
	}

	// 2. Repo-level overrides
	if rm, ok := s.repo[repo]; ok {
		for k, v := range rm {
			if !s.envMatch(k, env) {
				continue
			}
			merged[k] = v.Value
		}
	}

	// 3. Job-level overrides
	if jm, ok := s.job[repo+"/"+job]; ok {
		for k, v := range jm {
			if !s.envMatch(k, env) {
				continue
			}
			merged[k] = v.Value
		}
	}

	return merged
}

// GetRaw returns the resolved variable map plus metadata (masked/protected flags).
func (s *Store) GetRaw(repo, job, env string) map[string]Variable {
	merged := make(map[string]Variable)

	for k, v := range s.project {
		if !s.envMatch(k, env) {
			continue
		}
		merged[k] = v
	}

	if rm, ok := s.repo[repo]; ok {
		for k, v := range rm {
			if !s.envMatch(k, env) {
				continue
			}
			merged[k] = v
		}
	}

	if jm, ok := s.job[repo+"/"+job]; ok {
		for k, v := range jm {
			if !s.envMatch(k, env) {
				continue
			}
			merged[k] = v
		}
	}

	return merged
}

// List returns all variables sorted by key, with scope annotation.
func (s *Store) List() []Variable {
	var all []Variable
	for _, v := range s.project {
		all = append(all, v)
	}
	for _, rm := range s.repo {
		for _, v := range rm {
			all = append(all, v)
		}
	}
	for _, jm := range s.job {
		for _, v := range jm {
			all = append(all, v)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].Key < all[j].Key
	})
	return all
}

// Expand performs ${VAR} substitution on a value using the resolved map.
func Expand(value string, vars map[string]string) string {
	result := value
	for k, v := range vars {
		result = strings.ReplaceAll(result, "${"+k+"}", v)
	}
	return result
}

// FromEnv builds a Store from the initialized environment snapshot matching a
// prefix.
func FromEnv(prefix string) *Store {
	return FromEntries(config.Current().Prefixed(prefix), prefix)
}

// FromKestrix builds a Store from the canonical Kestrix environment snapshot.
func FromKestrix() *Store {
	return FromEntries(config.Current().Prefixed(config.Prefix), config.Prefix)
}

// FromEntries builds a Store from normalized NAME=value entries.
func FromEntries(entries []string, prefix string) *Store {
	s := New()
	for _, e := range entries {
		if !strings.HasPrefix(e, prefix) {
			continue
		}
		name, value, _ := strings.Cut(e, "=")
		key := strings.TrimPrefix(name, prefix)
		s.SetProject(key, value)
	}
	return s
}

// envMatch returns true if the variable has no env filter or matches.
func (s *Store) envMatch(key, env string) bool {
	if env == "" {
		return true // no environment specified → accept all
	}
	filter, ok := s.envFilter[key]
	if !ok {
		return true // no filter → accept
	}
	return filter == env
}

// Option configures a variable during Set operations.
type Option func(*Variable)

// WithProtected marks a variable as protected (only on protected branches).
func WithProtected(p bool) Option {
	return func(v *Variable) { v.Protected = p }
}

// WithMasked marks a variable as masked in logs.
func WithMasked(m bool) Option {
	return func(v *Variable) { v.Masked = m }
}

// WithScope overrides the default scope.
func WithScope(sc Scope) Option {
	return func(v *Variable) { v.Scope = sc }
}

// Summary returns a human-readable summary of the store contents.
func (s *Store) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Project vars: %d\n", len(s.project))
	for repo, rm := range s.repo {
		fmt.Fprintf(&b, "  Repo %q vars: %d\n", repo, len(rm))
	}
	for key, jm := range s.job {
		fmt.Fprintf(&b, "  Job %q vars: %d\n", key, len(jm))
	}
	return b.String()
}
