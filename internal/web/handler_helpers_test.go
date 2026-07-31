package web

import (
	"testing"

	"kestrix/internal/model"
	"kestrix/internal/variables"
)

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
