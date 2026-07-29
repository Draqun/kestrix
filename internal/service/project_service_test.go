package service

import (
	"errors"
	"testing"

	"gitlab.com/lunaris-bot/metaci/internal/domain"
	"gitlab.com/lunaris-bot/metaci/internal/mocks"
)

func TestProjectService_Create_MissingName(t *testing.T) {
	mock := &mocks.MockProjectStore{}
	svc := NewProjectService(mock)

	err := svc.Create(&domain.ProjectConfig{RepoPath: "/some/path"})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if len(mock.UpsertCalls) != 0 {
		t.Fatalf("expected Upsert not called, got %d calls", len(mock.UpsertCalls))
	}
}

func TestProjectService_Create_MissingRepo(t *testing.T) {
	mock := &mocks.MockProjectStore{}
	svc := NewProjectService(mock)

	err := svc.Create(&domain.ProjectConfig{Name: "myproject"})
	if err == nil {
		t.Fatal("expected error for missing repo")
	}
	if len(mock.UpsertCalls) != 0 {
		t.Fatalf("expected Upsert not called, got %d calls", len(mock.UpsertCalls))
	}
}

func TestProjectService_Create_Happy(t *testing.T) {
	mock := &mocks.MockProjectStore{}
	svc := NewProjectService(mock)

	cfg := &domain.ProjectConfig{Name: "myproject", RepoPath: "/some/path", PipelineDir: "ci", PipelineFile: "pipeline.tf", DefaultBranch: "main"}
	err := svc.Create(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.UpsertCalls) != 1 {
		t.Fatalf("expected 1 Upsert call, got %d", len(mock.UpsertCalls))
	}
	got := mock.UpsertCalls[0]
	if got.Name != "myproject" || got.RepoPath != "/some/path" {
		t.Errorf("Upsert called with wrong config: %+v", got)
	}
}

func TestProjectService_Update_Happy(t *testing.T) {
	mock := &mocks.MockProjectStore{
		GetResult: &domain.ProjectConfig{Name: "myproject", RepoPath: "/old"},
	}
	svc := NewProjectService(mock)

	cfg := &domain.ProjectConfig{Name: "myproject", RepoPath: "/new/path"}
	err := svc.Update("myproject", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.UpsertCalls) != 1 {
		t.Fatalf("expected 1 Upsert call, got %d", len(mock.UpsertCalls))
	}
}

func TestProjectService_Update_NotFound(t *testing.T) {
	mock := &mocks.MockProjectStore{GetResult: nil}
	svc := NewProjectService(mock)

	err := svc.Update("nope", &domain.ProjectConfig{Name: "nope"})
	if err == nil {
		t.Fatal("expected error for missing project")
	}
}

func TestProjectService_Update_MergesAllOmittedFields(t *testing.T) {
	existing := &domain.ProjectConfig{
		Name: "demo", RepoPath: "/repo", RepoURL: "https://example.test/repo.git",
		PipelineDir: "ci", PipelineFile: "pipeline.tf", GitLabProjectID: 42,
		DefaultBranch: "develop", WebhookSecret: "secret", CreatedAt: "yesterday",
		ExtraRepos: []domain.ExtraRepo{{Name: "docs", Path: "docs"}},
	}
	mock := &mocks.MockProjectStore{GetResult: existing}
	svc := NewProjectService(mock)
	update := &domain.ProjectConfig{}

	if err := svc.Update("demo", update); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if update.RepoURL != existing.RepoURL || update.DefaultBranch != existing.DefaultBranch ||
		update.WebhookSecret != existing.WebhookSecret || update.GitLabProjectID != existing.GitLabProjectID ||
		len(update.ExtraRepos) != 1 {
		t.Fatalf("omitted fields were not preserved: %+v", update)
	}
}

func TestProjectService_Update_ValidatesMergedConfig(t *testing.T) {
	mock := &mocks.MockProjectStore{GetResult: &domain.ProjectConfig{Name: "demo"}}
	err := NewProjectService(mock).Update("demo", &domain.ProjectConfig{})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

func TestProjectService_Delete_Happy(t *testing.T) {
	mock := &mocks.MockProjectStore{}
	svc := NewProjectService(mock)

	err := svc.Delete("myproject")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.DeleteCalls) != 1 || mock.DeleteCalls[0] != "myproject" {
		t.Errorf("expected Delete('myproject'), got %v", mock.DeleteCalls)
	}
}

func TestProjectService_Get(t *testing.T) {
	mock := &mocks.MockProjectStore{GetResult: &domain.ProjectConfig{Name: "proj"}}
	svc := NewProjectService(mock)

	p, err := svc.Get("proj")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil || p.Name != "proj" {
		t.Errorf("expected project 'proj', got %+v", p)
	}
}

func TestProjectService_List(t *testing.T) {
	mock := &mocks.MockProjectStore{ListResult: []string{"a", "b"}}
	svc := NewProjectService(mock)

	names, err := svc.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d", len(names))
	}
}

func TestProjectService_Load(t *testing.T) {
	mock := &mocks.MockProjectStore{LoadResult: map[string]*domain.ProjectConfig{"x": {Name: "x"}}}
	svc := NewProjectService(mock)

	projects, err := svc.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 1 {
		t.Errorf("expected 1 project, got %d", len(projects))
	}
}

func TestProjectService_FindByGitLabID(t *testing.T) {
	mock := &mocks.MockProjectStore{GetResult: &domain.ProjectConfig{Name: "g", GitLabProjectID: 42}}
	svc := NewProjectService(mock)

	// FindByGitLabID on MockProjectStore returns nil, nil — just verify no panic
	p, _ := svc.FindByGitLabID(42)
	_ = p
}

func TestProjectService_Create_DefaultsMissingFields(t *testing.T) {
	mock := &mocks.MockProjectStore{}
	svc := NewProjectService(mock)

	cfg := &domain.ProjectConfig{Name: "defaults", RepoPath: "/repo"}
	if err := svc.Create(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.UpsertCalls) != 1 {
		t.Fatalf("expected 1 Upsert call, got %d", len(mock.UpsertCalls))
	}
	got := mock.UpsertCalls[0]
	if got.DefaultBranch != "main" {
		t.Fatalf("DefaultBranch = %q, want main", got.DefaultBranch)
	}
	if got.PipelineFile != "pipeline.tf" {
		t.Fatalf("PipelineFile = %q, want pipeline.tf", got.PipelineFile)
	}
}
