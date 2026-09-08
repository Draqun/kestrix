package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/model"
	"kestrix/internal/variables"
)

func TestActorFrom_AuthenticatedRequest(t *testing.T) {
	r := requestWithClaims(domain.Claims{UserName: "alice", Role: domain.RoleOperator})
	if got := actorFrom(r); got != "user:alice" {
		t.Errorf("actorFrom = %q, want user:alice", got)
	}
}

func TestActorFrom_NoClaims(t *testing.T) {
	r := httptest.NewRequest("GET", "/x", nil)
	if got := actorFrom(r); got != "" {
		t.Errorf("actorFrom = %q, want empty string without claims", got)
	}
}

func TestBuildVarStore_NilProject(t *testing.T) {
	srv := &Server{cfg: Config{
		Docker: DockerConfig{
			Registry: "reg", Image: "img", Username: "user", Password: "pass",
		},
	}}
	vs := srv.buildVarStore(nil)
	if vs == nil {
		t.Fatal("expected non-nil var store")
	}
}

func TestAddEnvironmentVariables(t *testing.T) {
	vs := variables.New()
	addEnvironmentVariables(vs, []variables.Variable{{Key: "TEST_KEY", Value: "value"}})
	if got := vs.Get("", "", "")["TEST_KEY"]; got != "value" {
		t.Fatalf("environment variable = %q", got)
	}
}

func TestBuildVarStore_AllScopes(t *testing.T) {
	srv := &Server{cfg: Config{
		Docker: DockerConfig{
			Registry: "reg", Image: "img", Username: "user", Password: "pass",
		},
	}}
	proj := &model.Project{
		Variables: []model.Variable{
			{Key: "proj_var", Value: "val1", Scope: ""},
			{Key: "default_var", Value: "", Default: "def1", Scope: ""},
			{Key: "protected_var", Value: "sec", Protected: true, Scope: ""},
			{Key: "masked_var", Value: "hid", Masked: true, Scope: ""},
			{Key: "repo_var", Value: "rv", Repo: "app", Scope: "repo"},
			{Key: "job_var", Value: "jv", Repo: "app", Job: "test", Scope: "job"},
		},
	}
	vs := srv.buildVarStore(proj)
	if vs == nil {
		t.Fatal("expected non-nil var store")
	}
}

// TestBuildVarStore_DoesNotLeakDockerCredentialsOrKestrixEnvironment is a
// regression test: buildVarStore used to seed every KESTRIX_* environment
// variable (secrets included) and the configured Docker registry password
// into the store unconditionally, which flows straight into a task's
// environment as an always-included pipeline variable, bypassing the
// task-execution-isolation allowlist. See handler_helpers.go's buildVarStore
// doc comment.
func TestBuildVarStore_DoesNotLeakDockerCredentialsOrKestrixEnvironment(t *testing.T) {
	srv := &Server{cfg: Config{
		Docker: DockerConfig{
			Registry: "reg", Image: "img", Username: "user", Password: "super-secret-registry-password",
		},
	}}
	proj := &model.Project{Name: "proj"}
	resolved := srv.buildVarStore(proj).Get("", "", "")
	for _, leaked := range []string{"DOCKER_REGISTRY", "DOCKER_IMAGE", "DOCKER_USERNAME", "DOCKER_PASSWORD"} {
		if v, ok := resolved[leaked]; ok {
			t.Errorf("buildVarStore leaked %s=%q from Docker config", leaked, v)
		}
	}
	for name := range resolved {
		if strings.HasPrefix(name, "KESTRIX_") || strings.Contains(strings.ToUpper(name), "MASTER_KEY") || strings.Contains(strings.ToUpper(name), "WEBHOOK_SECRET") {
			t.Errorf("buildVarStore leaked environment-derived variable %q", name)
		}
	}
}
