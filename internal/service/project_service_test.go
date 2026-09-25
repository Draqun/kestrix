package service

import (
	"errors"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
)

func TestProjectService_Create_MissingName(t *testing.T) {
	mock := &mocks.MockProjectStore{}
	svc := NewProjectService(mock)

	err := svc.Create(&domain.ProjectConfig{RepoURL: "https://git.example/repo.git", RepoRef: "main"})
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

// TestProjectService_Create_RejectsUnrecognizedMode pins
// remote-runner-registration tasks.md 2.1: only local/docker/gitlab-runner/
// remote (or empty, deferred to the store's default) are accepted.
func TestProjectService_Create_RejectsUnrecognizedMode(t *testing.T) {
	mock := &mocks.MockProjectStore{}
	svc := NewProjectService(mock)

	err := svc.Create(&domain.ProjectConfig{Name: "x", RepoURL: "https://git.example/x.git", RepoRef: "main", Mode: "banana"})
	if err == nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation for an unrecognized mode, got %v", err)
	}
	if len(mock.UpsertCalls) != 0 {
		t.Fatalf("expected Upsert not called, got %d calls", len(mock.UpsertCalls))
	}
}

func TestProjectService_Create_AcceptsEveryRecognizedMode(t *testing.T) {
	for _, mode := range []string{"", "local", "docker", "gitlab-runner", "remote"} {
		mock := &mocks.MockProjectStore{}
		svc := NewProjectService(mock)
		err := svc.Create(&domain.ProjectConfig{Name: "x", RepoURL: "https://git.example/x.git", RepoRef: "main", Mode: mode})
		if err != nil {
			t.Fatalf("mode %q: unexpected error: %v", mode, err)
		}
	}
}

func TestProjectService_CreateRejectsInvalidRemoteForms(t *testing.T) {
	for _, cfg := range []*domain.ProjectConfig{
		{Name: "x", RepoURL: "https://git.example/x.git"},
		{Name: "x", RepoURL: "https://user:secret@git.example/x.git", RepoRef: "main"},
		{Name: "x", RepoRef: "main"},
	} {
		if err := NewProjectService(&mocks.MockProjectStore{}).Create(cfg); err == nil {
			t.Fatalf("expected invalid config rejection: %+v", cfg)
		}
	}
	if err := NewProjectService(&mocks.MockProjectStore{}).Create(&domain.ProjectConfig{Name: "x", RepoURL: "%gh&", RepoRef: "main"}); err == nil {
		t.Fatal("expected malformed URL rejection")
	}
}

// TestValidateRepoURL pins design.md D6: the three accepted forms, the
// rejected schemes, the rejected loopback/link-local/unspecified hosts, and
// an allowed RFC 1918 host.
func TestValidateRepoURL(t *testing.T) {
	accepted := []string{
		"https://git.example.test/team/app.git",
		"ssh://git@git.example.test/team/app.git",
		"git@git.example.test:team/app.git",
		"https://10.0.0.5/team/app.git", // private network host is allowed
	}
	for _, url := range accepted {
		t.Run("accept/"+url, func(t *testing.T) {
			cfg := &domain.ProjectConfig{Name: "x", RepoURL: url, RepoRef: "main"}
			if err := validateProject(cfg); err != nil {
				t.Fatalf("validateProject(%q) = %v, want nil", url, err)
			}
		})
	}

	rejectedSchemes := []string{
		"http://git.example.test/team/app.git",
		"file:///team/app.git",
		"git://git.example.test/team/app.git",
		"ext::sh -c id",
		"no-colon-no-scheme-at-all",        // no "://" and no ":": falls straight to url.Parse
		"path/before/any:colon-not-scpish", // "/" precedes the first ":": not scp-like either
	}
	for _, url := range rejectedSchemes {
		t.Run("reject-scheme/"+url, func(t *testing.T) {
			cfg := &domain.ProjectConfig{Name: "x", RepoURL: url, RepoRef: "main"}
			if err := validateProject(cfg); !errors.Is(err, ErrValidation) {
				t.Fatalf("validateProject(%q) = %v, want ErrValidation", url, err)
			}
		})
	}

	rejectedHosts := []string{
		"ssh://localhost/team/app.git",
		"https://127.0.0.1/team/app.git",
		"ssh://[::1]/team/app.git",
		"https://169.254.169.254/latest/meta-data/",
		"ssh://[fe80::1]/team/app.git",
		"git@localhost:team/app.git", // scp-like form with a disallowed host
	}
	for _, url := range rejectedHosts {
		t.Run("reject-host/"+url, func(t *testing.T) {
			cfg := &domain.ProjectConfig{Name: "x", RepoURL: url, RepoRef: "main"}
			if err := validateProject(cfg); !errors.Is(err, ErrValidation) {
				t.Fatalf("validateProject(%q) = %v, want ErrValidation", url, err)
			}
		})
	}
}

// TestParseLegacyIPv4Literal pins repo-url-literal-ip-hardening design.md D1:
// the inet_aton-style rule (1-4 components, decimal/octal/hex, last component
// absorbing the remaining bits).
func TestParseLegacyIPv4Literal(t *testing.T) {
	cases := []struct {
		host string
		want string
		ok   bool
	}{
		{"2130706433", "127.0.0.1", true},
		{"0x7f000001", "127.0.0.1", true},
		{"0177.0.0.1", "127.0.0.1", true},
		{"127.1", "127.0.0.1", true},
		{"10.0.0.5", "10.0.0.5", true},
		{"2852039166", "169.254.169.254", true},
		{"0xA9FEA9FE", "169.254.169.254", true},
		{"0251.0376.0251.0376", "169.254.169.254", true},
		{"169.254.43518", "169.254.169.254", true},
		{"169.16689662", "169.254.169.254", true},
		{"0", "0.0.0.0", true},
		{"0x0", "0.0.0.0", true},
		{"git.example.test", "", false},
		{"0x1.example.test", "", false},
		{"08", "", false},
		{"1..2.3", "", false},
		{".1.2.3", "", false},
		{"1.2.3.", "", false},
		{"0xZZ", "", false},
		{"256.1", "", false},
		{"256.1.1", "", false},
		{"256.1.1.1", "", false},
		{"127.0.0.1.5", "", false},
		{"99999999999", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			got, ok := parseLegacyIPv4Literal(tc.host)
			if ok != tc.ok {
				t.Fatalf("parseLegacyIPv4Literal(%q) ok = %v, want %v", tc.host, ok, tc.ok)
			}
			if !tc.ok {
				return
			}
			if got.String() != tc.want {
				t.Fatalf("parseLegacyIPv4Literal(%q) = %q, want %q", tc.host, got.String(), tc.want)
			}
		})
	}
}

// TestIsDisallowedRepoHostLegacyEncodings pins repo-url-literal-ip-hardening
// design.md D1: every alternate encoding of a disallowed literal is rejected,
// while a private address and ordinary hostnames stay allowed.
func TestIsDisallowedRepoHostLegacyEncodings(t *testing.T) {
	disallowed := []string{
		"127.0.0.1",
		"2130706433",
		"0x7f000001",
		"0177.0.0.1",
		"127.1",
		"169.254.169.254",
		"2852039166",
		"0xA9FEA9FE",
		"0251.0376.0251.0376",
		"169.254.43518",
		"169.16689662",
		"0",
		"0x0",
	}
	for _, host := range disallowed {
		t.Run("disallowed/"+host, func(t *testing.T) {
			if !isDisallowedRepoHost(host) {
				t.Fatalf("isDisallowedRepoHost(%q) = false, want true", host)
			}
		})
	}

	allowed := []string{
		"10.0.0.5",
		"git.example.test",
		"0x1.example.test",
		"8.8.8.8",
	}
	for _, host := range allowed {
		t.Run("allowed/"+host, func(t *testing.T) {
			if isDisallowedRepoHost(host) {
				t.Fatalf("isDisallowedRepoHost(%q) = true, want false", host)
			}
		})
	}
}

// TestValidateRepoURLRejectsLegacyIPEncodings pins the end-to-end requirement:
// a legacy-encoded loopback literal fails exactly like its canonical form.
func TestValidateRepoURLRejectsLegacyIPEncodings(t *testing.T) {
	canonical := &domain.ProjectConfig{Name: "x", RepoURL: "https://127.0.0.1/x.git", RepoRef: "main"}
	canonicalErr := validateProject(canonical)
	if !errors.Is(canonicalErr, ErrValidation) {
		t.Fatalf("validateProject(canonical) = %v, want ErrValidation", canonicalErr)
	}
	for _, raw := range []string{
		"https://2130706433/x.git",
		"https://0x7f000001/x.git",
	} {
		t.Run(raw, func(t *testing.T) {
			err := validateProject(&domain.ProjectConfig{Name: "x", RepoURL: raw, RepoRef: "main"})
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("validateProject(%q) = %v, want ErrValidation", raw, err)
			}
			if err.Error() != canonicalErr.Error() {
				t.Fatalf("validateProject(%q) = %q, want same text as canonical %q", raw, err.Error(), canonicalErr.Error())
			}
		})
	}
}

func TestProjectService_Create_Happy(t *testing.T) {
	mock := &mocks.MockProjectStore{}
	svc := NewProjectService(mock)

	cfg := &domain.ProjectConfig{Name: "myproject", RepoURL: "https://git.example/myproject.git", RepoRef: "main", PipelineDir: "ci", PipelineFile: "pipeline.tf", DefaultBranch: "main"}
	err := svc.Create(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.UpsertCalls) != 1 {
		t.Fatalf("expected 1 Upsert call, got %d", len(mock.UpsertCalls))
	}
	got := mock.UpsertCalls[0]
	if got.Name != "myproject" || got.RepoURL != "https://git.example/myproject.git" || got.RepoRef != "main" {
		t.Errorf("Upsert called with wrong config: %+v", got)
	}
}

func TestProjectService_Update_Happy(t *testing.T) {
	mock := &mocks.MockProjectStore{
		GetResult: &domain.ProjectConfig{Name: "myproject", RepoURL: "https://git.example/old.git", RepoRef: "main"},
	}
	svc := NewProjectService(mock)

	cfg := &domain.ProjectConfig{Name: "myproject", RepoURL: "https://git.example/new.git", RepoRef: "release"}
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

func TestProjectService_UpdateIsFullReplacementAndClearsCredential(t *testing.T) {
	existing := &domain.ProjectConfig{
		Name: "demo", RepoURL: "https://example.test/repo.git", RepoRef: "main",
		RepoCredentialRef: "old-credential",
		PipelineDir:       "ci", PipelineFile: "pipeline.tf", GitLabProjectID: 42,
		DefaultBranch: "develop", WebhookSecret: "secret", CreatedAt: "yesterday",
	}
	mock := &mocks.MockProjectStore{GetResult: existing}
	svc := NewProjectService(mock)
	update := &domain.ProjectConfig{
		RepoURL: "https://example.test/new.git", RepoRef: "release",
		PipelineDir: "pipelines", PipelineFile: "main.tf",
	}

	if err := svc.Update("demo", update); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if update.RepoURL != "https://example.test/new.git" || update.RepoRef != "release" ||
		update.RepoCredentialRef != "" || update.GitLabProjectID != 0 {
		t.Fatalf("update was not treated as a full replacement: %+v", update)
	}
	if update.Name != "demo" || update.CreatedAt != existing.CreatedAt {
		t.Fatalf("identity fields were not preserved: %+v", update)
	}
	// webhook_secret is the one field an omitted update keeps rather than
	// clears - see webhook-ingress-hardening design.md D4.
	if update.WebhookSecret != existing.WebhookSecret {
		t.Fatalf("omitted webhook_secret should keep the stored value: got %q, want %q", update.WebhookSecret, existing.WebhookSecret)
	}
}

// TestProjectService_UpdateWebhookSecretKeepOrReplace pins design.md D4: an
// update whose webhook_secret is omitted or empty keeps the stored secret;
// a non-empty value replaces it.
func TestProjectService_UpdateWebhookSecretKeepOrReplace(t *testing.T) {
	base := func() *domain.ProjectConfig {
		return &domain.ProjectConfig{
			Name: "demo", RepoURL: "https://example.test/repo.git", RepoRef: "main",
			WebhookSecret: "stored-secret",
		}
	}

	// Omitted (zero value) keeps the stored secret.
	mock := &mocks.MockProjectStore{GetResult: base()}
	update := &domain.ProjectConfig{RepoURL: "https://example.test/repo.git", RepoRef: "main"}
	if err := NewProjectService(mock).Update("demo", update); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if update.WebhookSecret != "stored-secret" {
		t.Fatalf("omit: WebhookSecret = %q, want stored-secret", update.WebhookSecret)
	}

	// Explicit empty string keeps the stored secret.
	mock = &mocks.MockProjectStore{GetResult: base()}
	update = &domain.ProjectConfig{RepoURL: "https://example.test/repo.git", RepoRef: "main", WebhookSecret: ""}
	if err := NewProjectService(mock).Update("demo", update); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if update.WebhookSecret != "stored-secret" {
		t.Fatalf("empty: WebhookSecret = %q, want stored-secret", update.WebhookSecret)
	}

	// A non-empty value replaces the stored secret.
	mock = &mocks.MockProjectStore{GetResult: base()}
	update = &domain.ProjectConfig{RepoURL: "https://example.test/repo.git", RepoRef: "main", WebhookSecret: "new-secret"}
	if err := NewProjectService(mock).Update("demo", update); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if update.WebhookSecret != "new-secret" {
		t.Fatalf("replace: WebhookSecret = %q, want new-secret", update.WebhookSecret)
	}
}

func TestProjectService_Update_ValidatesMergedConfig(t *testing.T) {
	mock := &mocks.MockProjectStore{GetResult: &domain.ProjectConfig{Name: "demo", RepoURL: "https://git.example/demo.git", RepoRef: "main"}}
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

// TestProjectService_CheckRepoCredential pins runtime-secrets design.md D4:
// a `secret` credential is rejected as a repo_credential_ref, a Git
// credential type is accepted, and an empty ref is unchanged behavior even
// when a lookup is configured.
func TestProjectService_CheckRepoCredential(t *testing.T) {
	lookup := func(ref string) (domain.CredentialMetadata, error) {
		switch ref {
		case "cred_git":
			return domain.CredentialMetadata{Ref: ref, Type: "token"}, nil
		case "cred_secret":
			return domain.CredentialMetadata{Ref: ref, Type: "secret"}, nil
		default:
			return domain.CredentialMetadata{}, errors.New("not found")
		}
	}

	t.Run("accepts token", func(t *testing.T) {
		mock := &mocks.MockProjectStore{}
		svc := NewProjectService(mock)
		svc.SetCredentialLookup(lookup)
		cfg := &domain.ProjectConfig{Name: "x", RepoURL: "https://git.example/x.git", RepoRef: "main", RepoCredentialRef: "cred_git"}
		if err := svc.Create(cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects secret", func(t *testing.T) {
		mock := &mocks.MockProjectStore{}
		svc := NewProjectService(mock)
		svc.SetCredentialLookup(lookup)
		cfg := &domain.ProjectConfig{Name: "x", RepoURL: "https://git.example/x.git", RepoRef: "main", RepoCredentialRef: "cred_secret"}
		err := svc.Create(cfg)
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("error = %v, want ErrValidation", err)
		}
		if len(mock.UpsertCalls) != 0 {
			t.Fatalf("expected Upsert not called, got %d calls", len(mock.UpsertCalls))
		}
	})

	t.Run("empty ref unaffected", func(t *testing.T) {
		mock := &mocks.MockProjectStore{}
		svc := NewProjectService(mock)
		svc.SetCredentialLookup(lookup)
		cfg := &domain.ProjectConfig{Name: "x", RepoURL: "https://git.example/x.git", RepoRef: "main"}
		if err := svc.Create(cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("lookup error is left for source acquisition", func(t *testing.T) {
		mock := &mocks.MockProjectStore{}
		svc := NewProjectService(mock)
		svc.SetCredentialLookup(lookup)
		cfg := &domain.ProjectConfig{Name: "x", RepoURL: "https://git.example/x.git", RepoRef: "main", RepoCredentialRef: "cred_unknown"}
		if err := svc.Create(cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("no lookup configured is unaffected", func(t *testing.T) {
		mock := &mocks.MockProjectStore{}
		svc := NewProjectService(mock)
		cfg := &domain.ProjectConfig{Name: "x", RepoURL: "https://git.example/x.git", RepoRef: "main", RepoCredentialRef: "cred_secret"}
		if err := svc.Create(cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("update rejects secret", func(t *testing.T) {
		mock := &mocks.MockProjectStore{GetResult: &domain.ProjectConfig{Name: "demo", RepoURL: "https://git.example/demo.git", RepoRef: "main"}}
		svc := NewProjectService(mock)
		svc.SetCredentialLookup(lookup)
		update := &domain.ProjectConfig{RepoURL: "https://git.example/demo.git", RepoRef: "main", RepoCredentialRef: "cred_secret"}
		if err := svc.Update("demo", update); !errors.Is(err, ErrValidation) {
			t.Fatalf("error = %v, want ErrValidation", err)
		}
	})
}

func TestProjectService_Create_DefaultsMissingFields(t *testing.T) {
	mock := &mocks.MockProjectStore{}
	svc := NewProjectService(mock)

	cfg := &domain.ProjectConfig{Name: "defaults", RepoURL: "https://git.example/defaults.git", RepoRef: "main"}
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
