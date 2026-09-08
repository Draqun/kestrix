package source

import (
	"context"
	"strings"
	"testing"

	"kestrix/internal/domain"
)

// mockCredentialStore returns a fixed credential (or error) for every ref,
// and records whether resolution was even attempted - so a test can assert
// that Resolve refuses a `secret` credential before any Git process would
// start, not merely that Git later fails to use it.
type mockCredentialStore struct {
	domain.CredentialStore
	cred domain.Credential
	err  error
}

func (m *mockCredentialStore) ResolveCredential(context.Context, string) (domain.Credential, error) {
	return m.cred, m.err
}

// TestCredentialStoreResolver_RefusesSecretType pins runtime-secrets
// design.md D4 and spec.md "Secret credential refused for Git": a `secret`
// credential is refused before any Git process starts, with an error naming
// the type mismatch and never a field of the credential.
func TestCredentialStoreResolver_RefusesSecretType(t *testing.T) {
	store := &mockCredentialStore{cred: domain.Credential{Type: "secret", Value: "must-not-leak-into-git"}}
	resolver := &CredentialStoreResolver{Store: store}

	_, err := resolver.Resolve(context.Background(), "cred_x")
	if err == nil {
		t.Fatal("expected an error refusing the secret credential")
	}
	if !strings.Contains(err.Error(), "secret") {
		t.Fatalf("error = %q, want it to name the type mismatch", err.Error())
	}
	if strings.Contains(err.Error(), "must-not-leak-into-git") {
		t.Fatalf("error = %q, leaked the credential value", err.Error())
	}
}

// TestCredentialStoreResolver_AcceptsGitTypes confirms every Git credential
// type still resolves unchanged.
func TestCredentialStoreResolver_AcceptsGitTypes(t *testing.T) {
	for _, typ := range []string{"token", "basic", "ssh"} {
		t.Run(typ, func(t *testing.T) {
			store := &mockCredentialStore{cred: domain.Credential{Type: typ, Token: "tok"}}
			resolver := &CredentialStoreResolver{Store: store}
			cred, err := resolver.Resolve(context.Background(), "cred_x")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cred.Type != typ {
				t.Fatalf("cred.Type = %q, want %q", cred.Type, typ)
			}
		})
	}
}
