package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"kestrix/internal/credentials"
	"kestrix/internal/domain"
	"kestrix/internal/redact"
	"kestrix/internal/variables"
)

// Errors BuildVarStore returns for an unresolvable secret variable; see
// design.md D6. Each is wrapped as `variable "<key>": <this>` and never
// carries the value, the credential name, or the wrapped resolver error.
var (
	ErrSecretNotFound   = errors.New("secret credential not found")
	ErrSecretRevoked    = errors.New("secret credential is revoked")
	ErrSecretWrongType  = errors.New("reference is not a secret credential")
	ErrSecretUnresolved = errors.New("secret credential could not be resolved")
)

// BuildVarStore constructs a variables.Store from a project's declared
// variables, resolving every secret_ref through resolver. It returns the
// resolved secret values (for the output redactor, see D5) and a resolution
// error - see design.md D3 - that MUST abort the run or task before it
// starts: a missing, revoked, wrong-type, or otherwise unresolvable
// reference fails closed rather than injecting an empty or stale value.
func BuildVarStore(ctx context.Context, proj *domain.Project, resolver domain.CredentialResolver) (*variables.Store, []string, error) {
	vs := variables.New()
	var resolvedValues []string
	if proj != nil {
		for _, v := range proj.Variables {
			val := v.Value
			if val == "" {
				val = v.Default
			}
			if v.SecretRef != "" {
				secretVal, err := resolveSecretValue(ctx, resolver, v.SecretRef)
				if err != nil {
					return nil, nil, fmt.Errorf("variable %q: %w", v.Key, err)
				}
				val = secretVal
				resolvedValues = append(resolvedValues, val)
			}
			opts := []variables.Option{}
			if v.Protected {
				opts = append(opts, variables.WithProtected(true))
			}
			if v.Masked {
				opts = append(opts, variables.WithMasked(true))
			}
			switch v.Scope {
			case "repo":
				vs.SetRepo(v.Repo, v.Key, val, opts...)
			case "job":
				vs.SetJob(v.Repo, v.Job, v.Key, val, opts...)
			default:
				vs.SetProject(v.Key, val, opts...)
			}
		}
	}
	return vs, resolvedValues, nil
}

// resolveSecretValue resolves ref through resolver and requires it to name a
// live `secret` credential. Every failure is one of the four D6 error shapes;
// the underlying resolver error is logged with the reference only, never
// returned to the caller.
func resolveSecretValue(ctx context.Context, resolver domain.CredentialResolver, ref string) (string, error) {
	if resolver == nil {
		slog.Warn("secret resolution: no credential resolver configured", "ref", ref)
		return "", ErrSecretUnresolved
	}
	cred, err := resolver.Resolve(ctx, ref)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrCredentialNotFound):
			return "", ErrSecretNotFound
		case errors.Is(err, domain.ErrCredentialRevoked):
			return "", ErrSecretRevoked
		default:
			slog.Warn("secret resolution failed", "ref", ref, "err", redact.Err(err, nil))
			return "", ErrSecretUnresolved
		}
	}
	if cred.Type != credentials.TypeSecret {
		return "", ErrSecretWrongType
	}
	return cred.Value, nil
}
