package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"kestrix/internal/credentials"
	"kestrix/internal/domain"
	"kestrix/internal/model"
)

// mockCredentialResolver resolves refs from an in-memory map, or returns Err
// for a ref present in Err instead.
type mockCredentialResolver struct {
	Credentials map[string]domain.Credential
	Err         map[string]error
	Calls       []string
}

func (m *mockCredentialResolver) Resolve(_ context.Context, ref string) (domain.Credential, error) {
	m.Calls = append(m.Calls, ref)
	if err, ok := m.Err[ref]; ok {
		return domain.Credential{}, err
	}
	if cred, ok := m.Credentials[ref]; ok {
		return cred, nil
	}
	return domain.Credential{}, fmt.Errorf("%w: %s", domain.ErrCredentialNotFound, ref)
}

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

	vs, resolved, err := BuildVarStore(context.Background(), proj, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vs == nil {
		t.Fatal("expected non-nil var store")
	}
	if len(resolved) != 0 {
		t.Fatalf("expected no resolved secrets, got %v", resolved)
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

	vs, resolved, err := BuildVarStore(context.Background(), proj, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vs == nil {
		t.Fatal("expected non-nil var store even with no variables")
	}
	if resolved == nil && len(resolved) != 0 {
		t.Fatalf("expected empty resolved slice, got %v", resolved)
	}
}

func TestBuildVarStore_DefaultValue(t *testing.T) {
	proj := &model.Project{
		Name: "proj",
		Variables: []model.Variable{
			{Key: "WITH_DEFAULT", Default: "fallback", Scope: "project"},
		},
	}
	vs, _, err := BuildVarStore(context.Background(), proj, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	vals := vs.Get("", "", "")
	if vals["WITH_DEFAULT"] != "fallback" {
		t.Errorf("expected default value 'fallback', got %q", vals["WITH_DEFAULT"])
	}
}

func TestBuildVarStore_NilProject(t *testing.T) {
	vs, resolved, err := BuildVarStore(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vs == nil {
		t.Fatal("expected non-nil var store for nil project")
	}
	if len(resolved) != 0 {
		t.Fatalf("expected no resolved secrets for nil project, got %v", resolved)
	}
}

// TestBuildVarStore_ResolvesSecretsPerScope pins design.md D3: a secret_ref
// is resolved through the given resolver and injected under the variable's
// own scope, exactly like a literal value would be.
func TestBuildVarStore_ResolvesSecretsPerScope(t *testing.T) {
	resolver := &mockCredentialResolver{Credentials: map[string]domain.Credential{
		"cred_project": {Type: credentials.TypeSecret, Value: "project-secret"},
		"cred_repo":    {Type: credentials.TypeSecret, Value: "repo-secret"},
		"cred_job":     {Type: credentials.TypeSecret, Value: "job-secret"},
	}}
	proj := &model.Project{
		Variables: []model.Variable{
			{Key: "PROJECT_SECRET", SecretRef: "cred_project", Scope: "project"},
			{Key: "REPO_SECRET", SecretRef: "cred_repo", Scope: "repo", Repo: "backend"},
			{Key: "JOB_SECRET", SecretRef: "cred_job", Scope: "job", Repo: "backend", Job: "deploy"},
			{Key: "LITERAL", Value: "untouched", Scope: "project"},
		},
	}

	vs, resolved, err := BuildVarStore(context.Background(), proj, resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vs.Get("", "", "")["PROJECT_SECRET"] != "project-secret" {
		t.Errorf("project secret = %v", vs.Get("", "", ""))
	}
	if vs.Get("backend", "", "")["REPO_SECRET"] != "repo-secret" {
		t.Errorf("repo secret = %v", vs.Get("backend", "", ""))
	}
	if vs.Get("backend", "deploy", "")["JOB_SECRET"] != "job-secret" {
		t.Errorf("job secret = %v", vs.Get("backend", "deploy", ""))
	}
	if vs.Get("", "", "")["LITERAL"] != "untouched" {
		t.Errorf("literal variable was touched: %v", vs.Get("", "", ""))
	}
	wantResolved := map[string]bool{"project-secret": true, "repo-secret": true, "job-secret": true}
	if len(resolved) != 3 {
		t.Fatalf("resolved = %v, want 3 values", resolved)
	}
	for _, v := range resolved {
		if !wantResolved[v] {
			t.Errorf("unexpected resolved value %q", v)
		}
	}
}

// TestBuildVarStore_SecretErrorShapes pins design.md D6: each failure kind
// names only the variable key, never the value, the credential name, or the
// wrapped resolver error text.
func TestBuildVarStore_SecretErrorShapes(t *testing.T) {
	resolver := &mockCredentialResolver{
		Credentials: map[string]domain.Credential{
			"cred_wrong_type": {Type: credentials.TypeToken, Token: "gh-token-should-not-leak"},
		},
		Err: map[string]error{
			"cred_revoked":    fmt.Errorf("%w: cred_revoked", domain.ErrCredentialRevoked),
			"cred_unresolved": errors.New("driver: connection reset by peer, decrypt key abcdef"),
		},
	}
	tests := []struct {
		name    string
		ref     string
		wantErr error
	}{
		{"not found", "cred_missing", ErrSecretNotFound},
		{"revoked", "cred_revoked", ErrSecretRevoked},
		{"wrong type", "cred_wrong_type", ErrSecretWrongType},
		{"unresolvable", "cred_unresolved", ErrSecretUnresolved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proj := &model.Project{Variables: []model.Variable{{Key: "REGISTRY_PASSWORD", SecretRef: tt.ref, Scope: "project"}}}
			_, _, err := BuildVarStore(context.Background(), proj, resolver)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if !strings.Contains(err.Error(), `"REGISTRY_PASSWORD"`) {
				t.Fatalf("error = %q, want it to name the variable", err.Error())
			}
			for _, leak := range []string{"gh-token-should-not-leak", "driver: connection reset", "decrypt key", "cred_revoked", "cred_wrong_type", "cred_unresolved"} {
				if strings.Contains(err.Error(), leak) {
					t.Fatalf("error = %q, leaked %q", err.Error(), leak)
				}
			}
		})
	}
}

// TestBuildVarStore_NoResolverIsUnresolved pins design.md D6's fourth shape
// for a project with no credential resolver configured at all.
func TestBuildVarStore_NoResolverIsUnresolved(t *testing.T) {
	proj := &model.Project{Variables: []model.Variable{{Key: "X", SecretRef: "cred_x", Scope: "project"}}}
	_, _, err := BuildVarStore(context.Background(), proj, nil)
	if !errors.Is(err, ErrSecretUnresolved) {
		t.Fatalf("error = %v, want ErrSecretUnresolved", err)
	}
}
