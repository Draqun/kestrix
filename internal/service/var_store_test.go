package service

import (
	"testing"

	"gitlab.com/lunaris-bot/metaci/internal/domain"
	"gitlab.com/lunaris-bot/metaci/internal/model"
)

func TestBuildVarStore_Happy(t *testing.T) {
	proj := &model.Project{
		Name: "myproject",
		Variables: []model.Variable{
			{Key: "DB_HOST", Value: "localhost", Scope: "project"},
			{Key: "SECRET", Value: "s3cret", Scope: "project", Masked: true},
			{Key: "PROTECTED", Value: "p", Scope: "project", Protected: true},
			{Key: "REPO_VAR", Value: "rv", Scope: "repo", Repo: "backend"},
			{Key: "JOB_VAR", Value: "jv", Scope: "job", Repo: "backend", Job: "test"},
		},
	}
	cfg := &domain.ProjectConfig{
		Name: "myproject",
	}

	vs := BuildVarStore(proj, cfg)
	if vs == nil {
		t.Fatal("expected non-nil var store")
	}

	// Project-level variables should be resolvable via Get("", "", "").
	vals := vs.Get("", "", "")
	if vals["DB_HOST"] != "localhost" {
		t.Errorf("expected DB_HOST=localhost, got %v", vals)
	}
	if vals["SECRET"] != "s3cret" {
		t.Errorf("expected SECRET=s3cret, got %v", vals)
	}

	// Repo-level variable.
	repoVals := vs.Get("backend", "", "")
	if repoVals["REPO_VAR"] != "rv" {
		t.Errorf("expected REPO_VAR=rv, got %v", repoVals)
	}

	// Job-level variable.
	jobVals := vs.Get("backend", "test", "")
	if jobVals["JOB_VAR"] != "jv" {
		t.Errorf("expected JOB_VAR=jv, got %v", jobVals)
	}
}

func TestBuildVarStore_Empty(t *testing.T) {
	proj := &model.Project{Name: "empty"}
	cfg := &domain.ProjectConfig{Name: "empty"}

	vs := BuildVarStore(proj, cfg)
	if vs == nil {
		t.Fatal("expected non-nil var store even with no variables")
	}
}

func TestBuildVarStore_DefaultValue(t *testing.T) {
	proj := &model.Project{
		Name: "proj",
		Variables: []model.Variable{
			{Key: "WITH_DEFAULT", Default: "fallback", Scope: "project"},
		},
	}
	vs := BuildVarStore(proj, &domain.ProjectConfig{Name: "proj"})
	vals := vs.Get("", "", "")
	if vals["WITH_DEFAULT"] != "fallback" {
		t.Errorf("expected default value 'fallback', got %q", vals["WITH_DEFAULT"])
	}
}

func TestBuildVarStore_NilProject(t *testing.T) {
	vs := BuildVarStore(nil, &domain.ProjectConfig{Name: "proj"})
	if vs == nil {
		t.Fatal("expected non-nil var store for nil project")
	}
}
