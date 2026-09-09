package service

import (
	"errors"
	"strings"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/model"
)

func names(sources []domain.ProjectSource) []string {
	out := make([]string, 0, len(sources))
	for _, s := range sources {
		out = append(out, s.Name)
	}
	return out
}

func TestDeriveSources(t *testing.T) {
	cfg := &domain.ProjectConfig{Name: "checkout", RepoURL: "https://git.example.test/team/checkout.git"}
	cases := []struct {
		name  string
		repos []model.Repo
		want  []string
	}{
		{"no remote repos", []model.Repo{{Name: "app", Path: "services/app"}}, []string{"checkout"}},
		{"two remote repos", []model.Repo{
			{Name: "a", URL: "https://git.example.test/team/a.git", Ref: "main"},
			{Name: "b", Path: "services/b"},
			{Name: "c", URL: "ssh://git@git.example.test/team/c.git", Ref: "main"},
		}, []string{"checkout", "a", "c"}},
		{"duplicate URL collapses", []model.Repo{
			{Name: "shared", URL: "https://git.example.test/team/shared.git"},
			{Name: "shared-ssh", URL: "git@git.example.test:team/shared.git"},
		}, []string{"checkout", "shared"}},
		{"primary declared again is not duplicated", []model.Repo{
			{Name: "self", URL: "git@git.example.test:team/checkout.git"},
		}, []string{"checkout"}},
		{"unroutable URL is skipped", []model.Repo{
			{Name: "bad", URL: "https://user:pw@git.example.test/team/bad.git"},
		}, []string{"checkout"}},
		// Declaration order must not leak into the derived set: the store
		// returns primary first then by name, and refreshSources compares the
		// two directly.
		{"declaration order is canonicalized", []model.Repo{
			{Name: "zebra", URL: "https://git.example.test/team/z.git"},
			{Name: "alpha", URL: "https://git.example.test/team/a.git"},
		}, []string{"checkout", "alpha", "zebra"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DeriveSources(cfg, model.Project{Repos: tc.repos})
			if len(got) != len(tc.want) {
				t.Fatalf("sources = %v, want %v", names(got), tc.want)
			}
			for i := range tc.want {
				if got[i].Name != tc.want[i] {
					t.Errorf("source %d = %q, want %q", i, got[i].Name, tc.want[i])
				}
				if got[i].Primary != (i == 0) {
					t.Errorf("source %d primary = %v", i, got[i].Primary)
				}
			}
		})
	}
}

// The order is total, so a derived set and the store's rows compare equal
// only when they really hold the same sources. Two ci_repo blocks sharing a
// name (the parser's labels normally prevent it) still order deterministically.
func TestDeriveSourcesOrdersSameNameByURL(t *testing.T) {
	got := DeriveSources(
		&domain.ProjectConfig{Name: "checkout", RepoURL: "https://git.example.test/team/checkout.git"},
		model.Project{Repos: []model.Repo{
			{Name: "dup", URL: "https://git.example.test/team/b.git"},
			{Name: "dup", URL: "https://git.example.test/team/a.git"},
		}})

	if len(got) != 3 || !got[0].Primary ||
		got[1].URL != "https://git.example.test/team/a.git" ||
		got[2].URL != "https://git.example.test/team/b.git" {
		t.Fatalf("sources = %+v, want primary then a.git then b.git", got)
	}
}

func TestProjectServiceRecordsDerivedSourcesOnCreateAndUpdate(t *testing.T) {
	store := &mocks.MockProjectStore{}
	svc := NewProjectService(store)
	parsed := model.Project{Repos: []model.Repo{{Name: "shared", URL: "https://git.test/shared.git", Ref: "main"}}}
	validate := func(*domain.ProjectConfig) (model.Project, error) { return parsed, nil }

	// A client-supplied sources list is ignored in favour of the derived set.
	cfg := &domain.ProjectConfig{Name: "demo", RepoURL: "https://git.test/demo.git", RepoRef: "main",
		Sources: []domain.ProjectSource{{Name: "smuggled", URL: "https://git.test/smuggled.git"}}}
	if err := svc.CreateValidated(cfg, validate); err != nil {
		t.Fatalf("CreateValidated: %v", err)
	}
	if len(store.ReplaceSourcesCalls) != 1 || store.ReplaceSourcesCalls[0].Name != "demo" {
		t.Fatalf("ReplaceSources calls = %+v", store.ReplaceSourcesCalls)
	}
	if got := names(store.ReplaceSourcesCalls[0].Sources); len(got) != 2 || got[0] != "demo" || got[1] != "shared" {
		t.Fatalf("derived sources = %v, want [demo shared]", got)
	}
	if got := names(cfg.Sources); len(got) != 2 || got[0] != "demo" {
		t.Fatalf("returned config sources = %v", got)
	}

	// Update without a validator knows only the primary.
	if err := svc.Update("demo", &domain.ProjectConfig{RepoURL: "https://git.test/moved.git", RepoRef: "main"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	last := store.ReplaceSourcesCalls[len(store.ReplaceSourcesCalls)-1]
	if len(last.Sources) != 1 || last.Sources[0].URL != "https://git.test/moved.git" || !last.Sources[0].Primary {
		t.Fatalf("update sources = %+v", last.Sources)
	}

	// A failed validation leaves sources untouched.
	calls := len(store.ReplaceSourcesCalls)
	failing := func(*domain.ProjectConfig) (model.Project, error) { return model.Project{}, errors.New("denied") }
	if err := svc.UpdateValidated("demo", &domain.ProjectConfig{RepoURL: "https://git.test/x.git", RepoRef: "main"}, failing); !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
	if len(store.ReplaceSourcesCalls) != calls {
		t.Fatal("failed validation replaced sources")
	}
}

// The project row and its source set are written in one store operation, so a
// failure leaves nothing behind: a client that reads back after the error sees
// the state it had before, not a half-applied create or update.
func TestProjectServiceWriteFailureLeavesNothingPersisted(t *testing.T) {
	failure := errors.New("database unavailable")
	for _, tc := range []struct {
		name  string
		store *mocks.MockProjectStore
	}{
		{"project row rejected", &mocks.MockProjectStore{UpsertErr: failure}},
		{"source set rejected", &mocks.MockProjectStore{ReplaceSourcesErr: failure}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := NewProjectService(tc.store).Create(&domain.ProjectConfig{
				Name: "demo", RepoURL: "https://git.test/demo.git", RepoRef: "main",
			})
			if !errors.Is(err, failure) {
				t.Fatalf("error = %v, want the store's own error", err)
			}
			if _, stored := tc.store.Projects["demo"]; stored {
				t.Error("the project was persisted even though the write failed")
			}
		})
	}
}

// A repo_url that passes the scheme and host checks but names no repository
// path has no match key: it would register and then never route, and would
// leave the project without a primary source. It is rejected up front.
func TestProjectServiceRejectsRepoURLWithoutRepositoryPath(t *testing.T) {
	for _, raw := range []string{
		"https://git.example.test",
		"https://git.example.test/",
		"ssh://git@git.example.test",
		"https://git.example.test/.git",
	} {
		err := NewProjectService(&mocks.MockProjectStore{}).Create(&domain.ProjectConfig{Name: "x", RepoURL: raw, RepoRef: "main"})
		if !errors.Is(err, ErrValidation) || err == nil || !strings.Contains(err.Error(), "repository path") {
			t.Errorf("Create(%q) = %v, want a repository-path validation error", raw, err)
		}
	}
	if err := NewProjectService(&mocks.MockProjectStore{}).Create(&domain.ProjectConfig{Name: "x", RepoURL: "git@git.example.test:team/x.git", RepoRef: "main"}); err != nil {
		t.Errorf("scp-like URL with a path rejected: %v", err)
	}
}
