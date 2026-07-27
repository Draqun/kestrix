package variables

import (
	"os"
	"strings"
	"testing"
)

func TestStoreProjectLevel(t *testing.T) {
	s := New()
	s.SetProject("API_KEY", "secret123")
	s.SetProject("DEBUG", "true")

	vars := s.Get("", "", "")
	if vars["API_KEY"] != "secret123" {
		t.Errorf("API_KEY = %q, want %q", vars["API_KEY"], "secret123")
	}
	if vars["DEBUG"] != "true" {
		t.Errorf("DEBUG = %q, want %q", vars["DEBUG"], "true")
	}
}

func TestStoreRepoOverride(t *testing.T) {
	s := New()
	s.SetProject("API_KEY", "global-key")
	s.SetRepo("auth", "API_KEY", "auth-key")

	vars := s.Get("auth", "", "")
	if vars["API_KEY"] != "auth-key" {
		t.Errorf("repo override: API_KEY = %q, want %q", vars["API_KEY"], "auth-key")
	}

	// Other repo gets project-level
	vars2 := s.Get("gateway", "", "")
	if vars2["API_KEY"] != "global-key" {
		t.Errorf("other repo: API_KEY = %q, want %q", vars2["API_KEY"], "global-key")
	}
}

func TestStoreJobOverride(t *testing.T) {
	s := New()
	s.SetProject("API_KEY", "global")
	s.SetRepo("auth", "API_KEY", "repo-key")
	s.SetJob("auth", "test", "API_KEY", "job-key")

	vars := s.Get("auth", "test", "")
	if vars["API_KEY"] != "job-key" {
		t.Errorf("job override: API_KEY = %q, want %q", vars["API_KEY"], "job-key")
	}

	// Same repo, different job → repo level
	vars2 := s.Get("auth", "deploy", "")
	if vars2["API_KEY"] != "repo-key" {
		t.Errorf("other job: API_KEY = %q, want %q", vars2["API_KEY"], "repo-key")
	}
}

func TestEnvFilter(t *testing.T) {
	s := New()
	s.SetProject("SECRET", "prod-value")
	s.SetProject("DEBUG", "false")
	s.SetEnvFilter("SECRET", "production")

	// Production env → gets SECRET
	vars := s.Get("", "", "production")
	if vars["SECRET"] != "prod-value" {
		t.Errorf("production: SECRET = %q, want %q", vars["SECRET"], "prod-value")
	}

	// Staging env → no SECRET (env filter blocks it)
	vars2 := s.Get("", "", "staging")
	if _, ok := vars2["SECRET"]; ok {
		t.Error("staging should not have SECRET")
	}

	// No env → gets SECRET (env filter only applies when env is specified)
	vars3 := s.Get("", "", "")
	if vars3["SECRET"] != "prod-value" {
		t.Errorf("no env: SECRET = %q, want %q", vars3["SECRET"], "prod-value")
	}
}

func TestExpand(t *testing.T) {
	vars := map[string]string{
		"HOST": "localhost",
		"PORT": "8080",
	}
	result := Expand("http://${HOST}:${PORT}/api", vars)
	if result != "http://localhost:8080/api" {
		t.Errorf("Expand = %q, want %q", result, "http://localhost:8080/api")
	}
}

func TestFromEnv(t *testing.T) {
	os.Setenv("TEST_PREFIX_FOO", "bar")
	os.Setenv("TEST_PREFIX_BAZ", "qux")
	defer os.Unsetenv("TEST_PREFIX_FOO")
	defer os.Unsetenv("TEST_PREFIX_BAZ")

	s := FromEnv("TEST_PREFIX_")
	vars := s.Get("", "", "")
	if vars["FOO"] != "bar" {
		t.Errorf("FOO = %q, want %q", vars["FOO"], "bar")
	}
	if vars["BAZ"] != "qux" {
		t.Errorf("BAZ = %q, want %q", vars["BAZ"], "qux")
	}
}

func TestSummary(t *testing.T) {
	s := New()
	s.SetProject("A", "1")
	s.SetRepo("auth", "B", "2")
	s.SetJob("auth", "test", "C", "3")

	summary := s.Summary()
	if summary == "" {
		t.Error("Summary should not be empty")
	}
}

func TestList(t *testing.T) {
	s := New()
	s.SetProject("Z", "1")
	s.SetProject("A", "2")
	s.SetRepo("auth", "M", "3")

	list := s.List()
	if len(list) != 3 {
		t.Fatalf("List = %d items, want 3", len(list))
	}
	// Should be sorted by key
	if list[0].Key != "A" {
		t.Errorf("list[0].Key = %q, want %q", list[0].Key, "A")
	}
}

func TestMaskedAndProtected(t *testing.T) {
	s := New()
	s.SetProject("SECRET", "hidden", WithMasked(true), WithProtected(true))

	raw := s.GetRaw("", "", "")
	v := raw["SECRET"]
	if !v.Masked {
		t.Error("expected masked=true")
	}
	if !v.Protected {
		t.Error("expected protected=true")
	}
}

// TestSetJobOptions exercises the options loop inside SetJob. Without
// options the `for _, o := range opts { o(&v) }` body never executes.
// Verifies WithProtected and WithMasked flow through.
func TestSetJobOptions(t *testing.T) {
	s := New()
	s.SetJob("auth", "test", "TOKEN", "v",
		WithProtected(true), WithMasked(true))

	raw := s.GetRaw("auth", "test", "")
	v := raw["TOKEN"]
	if v.Value != "v" {
		t.Errorf("value = %q, want %q", v.Value, "v")
	}
	if !v.Protected {
		t.Error("want Protected=true")
	}
	if !v.Masked {
		t.Error("want Masked=true")
	}
	if v.Scope != ScopeJob {
		t.Errorf("Scope = %q, want %q", v.Scope, ScopeJob)
	}
}

// TestGetEnvFilterPerScope verifies that env filters are honored at every
// level (project / repo / job). Hitting each continue branch ensures that a
// variable with a non-matching envFilter is dropped at each scope rather
// than only at the project level.
func TestGetEnvFilterPerScope(t *testing.T) {
	s := New()
	s.SetProject("PROJ_ONLY", "p")
	s.SetEnvFilter("PROJ_ONLY", "prod")
	s.SetRepo("auth", "REPO_ONLY", "r")
	s.SetEnvFilter("REPO_ONLY", "prod")
	s.SetJob("auth", "test", "JOB_ONLY", "j")
	s.SetEnvFilter("JOB_ONLY", "prod")

	// Get in non-matching env: each scope's variable should be filtered out.
	got := s.Get("auth", "test", "staging")
	if _, ok := got["PROJ_ONLY"]; ok {
		t.Error("PROJ_ONLY should be filtered at project scope in 'staging' env")
	}
	if _, ok := got["REPO_ONLY"]; ok {
		t.Error("REPO_ONLY should be filtered at repo scope in 'staging' env")
	}
	if _, ok := got["JOB_ONLY"]; ok {
		t.Error("JOB_ONLY should be filtered at job scope in 'staging' env")
	}

	// Same queries through GetRaw so the metadata-aware path also drops them.
	raw := s.GetRaw("auth", "test", "staging")
	if _, ok := raw["PROJ_ONLY"]; ok {
		t.Error("GetRaw PROJ_ONLY should drop in 'staging'")
	}
	if _, ok := raw["REPO_ONLY"]; ok {
		t.Error("GetRaw REPO_ONLY should drop in 'staging'")
	}
	if _, ok := raw["JOB_ONLY"]; ok {
		t.Error("GetRaw JOB_ONLY should drop in 'staging'")
	}

	// Matching env: all three variables should be visible again.
	got = s.Get("auth", "test", "prod")
	if got["PROJ_ONLY"] != "p" || got["REPO_ONLY"] != "r" || got["JOB_ONLY"] != "j" {
		t.Errorf("prod env: missing entries: %v", got)
	}
}

// TestListJobOnlyScope drops a job entry into an otherwise empty store to
// cover the `for _, v := range jm { all = append(...) }` branch of List,
// which only executes when there are job-scope entries.
func TestListJobOnlyScope(t *testing.T) {
	s := New()
	s.SetJob("auth", "test", "J_ONLY", "v")

	list := s.List()
	if len(list) != 1 {
		t.Fatalf("List = %d items, want 1 (job-only entry)", len(list))
	}
	if list[0].Key != "J_ONLY" {
		t.Errorf("list[0].Key = %q, want %q", list[0].Key, "J_ONLY")
	}
	if list[0].Scope != ScopeJob {
		t.Errorf("list[0].Scope = %q, want %q", list[0].Scope, ScopeJob)
	}
}

// TestSetRepoAndSetJobInitBranch covers the `s.repo[repo] == nil` initializer
// inside SetRepo and SetJob. The existing tests only ever set a single
// variable per repo/job, so the second call (where the inner map is already
// populated) was previously uncovered.
func TestSetRepoAndSetJobInitBranch(t *testing.T) {
	s := New()

	// First SetRepo creates the inner map (init branch covered).
	s.SetRepo("auth", "API_KEY", "v1")
	// Second SetRepo writes into the existing inner map (non-init branch).
	s.SetRepo("auth", "DB_URL", "v2")

	if got := s.Get("auth", "", ""); got["DB_URL"] != "v2" || got["API_KEY"] != "v1" {
		t.Errorf("after two SetRepo on same repo, got %v", got)
	}

	s.SetJob("auth", "test", "TOKEN", "t1")
	s.SetJob("auth", "test", "TRACE", "t2")

	if got := s.Get("auth", "test", ""); got["TRACE"] != "t2" || got["TOKEN"] != "t1" {
		t.Errorf("after two SetJob on same slot, got %v", got)
	}
}

// TestWithScopeOverride exercises the WithScope option which is otherwise
// never invoked in the existing test table. SetProject defaults to
// ScopeProject; the option lets callers override (e.g. push a project-level
// store value but tag it as ScopeJob for downstream filters).
func TestWithScopeOverride(t *testing.T) {
	s := New()
	s.SetProject("K", "v", WithScope(ScopeRepo))
	s.SetProject("J", "v", WithScope(ScopeJob))

	raw := s.GetRaw("", "", "")
	if raw["K"].Scope != ScopeRepo {
		t.Errorf("K.Scope = %q, want %q", raw["K"].Scope, ScopeRepo)
	}
	if raw["J"].Scope != ScopeJob {
		t.Errorf("J.Scope = %q, want %q", raw["J"].Scope, ScopeJob)
	}
}

// TestListEmptyStore covers the early-exit path of List when no scopes have
// entries. The existing test always sets at least one variable, so the
// zero-iteration case was uncovered.
func TestListEmptyStore(t *testing.T) {
	s := New()
	if list := s.List(); len(list) != 0 {
		t.Errorf("List on empty store = %d items, want 0", len(list))
	}
}

// TestGetRawMatchesGet confirms that GetRaw returns the same set of keys as
// Get but with full Variable metadata, including the env-filter path that
// was only exercised through Get.
func TestGetRawMatchesGet(t *testing.T) {
	s := New()
	s.SetProject("GLOBAL", "g", WithProtected(true), WithMasked(true))
	s.SetRepo("auth", "AUTH_ONLY", "a", WithProtected(true))
	s.SetEnvFilter("AUTH_ONLY", "prod")

	// Empty env → AUTH_ONLY visible because env filter only blocks when env is set.
	raw := s.GetRaw("auth", "", "")
	if len(raw) != 2 {
		t.Errorf("empty env raw = %d entries, want 2", len(raw))
	}
	if raw["GLOBAL"].Value != "g" {
		t.Errorf("GLOBAL = %q, want %q", raw["GLOBAL"].Value, "g")
	}

	// Non-prod env → AUTH_ONLY filtered out, but its Variable is skipped
	// entirely (not present in result).
	raw = s.GetRaw("auth", "", "staging")
	if _, ok := raw["AUTH_ONLY"]; ok {
		t.Error("staging should not have AUTH_ONLY due to envFilter")
	}
	if raw["GLOBAL"].Value != "g" {
		t.Error("GLOBAL should be environment-agnostic and still present")
	}

	// Job-level override visible via GetRaw.
	s.SetJob("auth", "ci", "JOB_ONLY", "j")
	raw = s.GetRaw("auth", "ci", "")
	if raw["JOB_ONLY"].Value != "j" {
		t.Errorf("JOB_ONLY = %q, want %q", raw["JOB_ONLY"].Value, "j")
	}

	// GetRaw for a repo that has no entries should still return the inherited
	// project-level map (no panic on missing repo/job inner map).
	raw = s.GetRaw("never-set", "ci", "")
	if raw["GLOBAL"].Value != "g" {
		t.Errorf("unknown repo: GLOBAL = %q, want %q (inherited)", raw["GLOBAL"].Value, "g")
	}
}

// TestFromEnvIgnoresUnprefixed verifies that environment variables lacking
// the requested prefix are silently dropped. FromEnv's only build branch in
// this case is `!strings.HasPrefix(e, prefix)` → continue.
//
// The `if len(parts) != 2` continue branch of FromEnv is unreachable on any
// supported platform: Go's os.Environ() only returns strings of the form
// "KEY=VALUE" — every entry has at least one '='. The remaining 10% in
// FromEnv is genuinely unreachable without OS-level surgery and is left
// documented here rather than instrumented to produce false coverage.
func TestFromEnvIgnoresUnprefixed(t *testing.T) {
	os.Setenv("UNRELATED_VAR", "noise")
	os.Setenv("PFX_KEY", "value")
	defer os.Unsetenv("UNRELATED_VAR")
	defer os.Unsetenv("PFX_KEY")

	s := FromEnv("PFX_")
	vars := s.Get("", "", "")
	if _, ok := vars["UNRELATED_VAR"]; ok {
		t.Error("FromEnv leaked UNRELATED_VAR despite missing prefix")
	}
	if vars["KEY"] != "value" {
		t.Errorf("KEY = %q, want %q", vars["KEY"], "value")
	}
}

// TestSummaryEmptyStore confirms that Summary on an empty store still
// returns a non-empty prefix (the project count line) without iterating any
// inner maps.
func TestSummaryEmptyStore(t *testing.T) {
	s := New()
	sum := s.Summary()
	if sum == "" {
		t.Error("Summary on empty store should still print project count")
	}
	if !strings.Contains(sum, "Project vars: 0") {
		t.Errorf("Summary on empty store = %q, want to mention 'Project vars: 0'", sum)
	}
}
