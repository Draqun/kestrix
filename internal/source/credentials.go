package source

import (
	"context"
	"fmt"

	"kestrix/internal/domain"
)

// CredentialStoreResolver bridges the domain credential store to source
// acquisition without consulting process environment variables.
type CredentialStoreResolver struct {
	Store domain.CredentialStore
}

var _ domain.CredentialResolver = (*CredentialStoreResolver)(nil)

func (r *CredentialStoreResolver) Resolve(ctx context.Context, ref string) (domain.Credential, error) {
	if r == nil || r.Store == nil {
		return domain.Credential{}, fmt.Errorf("credential store is not configured")
	}
	return r.Store.ResolveCredential(ctx, ref)
}
