package source

import (
	"context"
	"fmt"

	"kestrix/internal/credentials"
	"kestrix/internal/domain"
)

// CredentialStoreResolver bridges the domain credential store to source
// acquisition without consulting process environment variables.
type CredentialStoreResolver struct {
	Store domain.CredentialStore
}

var _ domain.CredentialResolver = (*CredentialStoreResolver)(nil)

// Resolve resolves ref for Git source acquisition. It refuses a `secret`
// credential before any Git process starts - see runtime-secrets design.md
// D4: a run-time secret is never usable for Git authentication, and the
// error names the type mismatch only, never a field of the credential.
func (r *CredentialStoreResolver) Resolve(ctx context.Context, ref string) (domain.Credential, error) {
	if r == nil || r.Store == nil {
		return domain.Credential{}, fmt.Errorf("credential store is not configured")
	}
	cred, err := r.Store.ResolveCredential(ctx, ref)
	if err != nil {
		return domain.Credential{}, err
	}
	if cred.Type == credentials.TypeSecret {
		return domain.Credential{}, fmt.Errorf("credential %q is a secret credential and cannot be used for Git access", ref)
	}
	return cred, nil
}
