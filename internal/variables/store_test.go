package variables

import (
	"os"
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
