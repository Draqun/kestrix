package credentials

import (
	"testing"

	"kestrix/internal/domain"
)

func TestNormalizeProviderNeutralForms(t *testing.T) {
	tests := []struct {
		name, kind string
		input      domain.CredentialInput
		check      func(domain.Credential) bool
	}{
		{"token", TypeToken, domain.CredentialInput{Type: TypeToken, Name: "gh", Token: "tok"}, func(c domain.Credential) bool { return c.Token == "tok" }},
		{"basic", TypeBasic, domain.CredentialInput{Type: TypeBasic, Name: "bb", Username: "user", Password: "app-pass"}, func(c domain.Credential) bool { return c.Username == "user" && c.Password == "app-pass" }},
		{"ssh", TypeSSH, domain.CredentialInput{Type: TypeSSH, Name: "ssh", PrivateKey: "PRIVATE", KnownHosts: "KNOWN"}, func(c domain.Credential) bool { return c.PrivateKey == "PRIVATE" && c.KnownHosts == "KNOWN" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Normalize("cred_ref", tt.input)
			if err != nil || !tt.check(got.Secret) {
				t.Fatalf("got=%+v err=%v", got.Secret, err)
			}
		})
	}
}

func TestNormalizeRejectsMissingSecrets(t *testing.T) {
	for _, input := range []domain.CredentialInput{{Type: TypeToken, Name: "x"}, {Type: TypeBasic, Name: "x", Username: "u"}, {Type: TypeSSH, Name: "x"}} {
		if _, err := Normalize("ref", input); err == nil {
			t.Fatalf("Normalize(%+v) unexpectedly succeeded", input)
		}
	}
	if _, err := Normalize("ref", domain.CredentialInput{Type: "unknown", Name: "x"}); err == nil {
		t.Fatal("expected unsupported type error")
	}
	if _, err := Normalize("", domain.CredentialInput{Type: TypeToken, Name: "x", Token: "t"}); err == nil {
		t.Fatal("expected missing ref error")
	}
	if _, err := Normalize("ref", domain.CredentialInput{Type: TypeToken, Token: "t"}); err == nil {
		t.Fatal("expected missing name error")
	}
}
