package service

import (
	"errors"
	"fmt"
	"strings"

	"kestrix/internal/credentials"
	"kestrix/internal/domain"
)

// ErrValidation identifies invalid project configuration supplied by a client.
var ErrValidation = errors.New("project validation failed")

// ProjectService handles project CRUD with validation.
type ProjectService struct {
	store domain.ProjectStore
	// credentialLookup resolves a repo_credential_ref's metadata so
	// CreateValidated/UpdateValidated can reject a `secret` credential as a
	// repository credential (see runtime-secrets design.md D4). Nil (the
	// default) performs no type check, so callers that never wire a
	// credential store keep their prior behavior.
	credentialLookup func(ref string) (domain.CredentialMetadata, error)
	// allowLocal mirrors runner.allow_local: false (the default) rejects a
	// project explicitly registered with mode `local` - see
	// safe-production-execution design.md D1.
	allowLocal bool
}

// SetAllowLocal configures whether CreateValidated/UpdateValidated accept a
// project registered with mode `local`. See runner.allow_local.
func (s *ProjectService) SetAllowLocal(allow bool) {
	s.allowLocal = allow
}

// SetCredentialLookup configures how CreateValidated and UpdateValidated
// look up a repo_credential_ref's type. A domain.CredentialStore's
// GetCredentialMetadata method satisfies this signature directly.
func (s *ProjectService) SetCredentialLookup(lookup func(ref string) (domain.CredentialMetadata, error)) {
	s.credentialLookup = lookup
}

// credentialRefIsSecret reports whether ref names a `secret`-typed
// credential, and returns ref trimmed for callers to reuse in error
// messages. A lookup error (unknown ref, store unavailable) is left for
// source acquisition to report later, as before this check existed; only a
// confirmed `secret` type is rejected by callers.
func (s *ProjectService) credentialRefIsSecret(ref string) (trimmed string, isSecret bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" || s.credentialLookup == nil {
		return ref, false
	}
	meta, err := s.credentialLookup(ref)
	if err != nil {
		return ref, false
	}
	return ref, meta.Type == credentials.TypeSecret
}

// checkRepoCredential rejects a repo_credential_ref that names a `secret`
// credential - see runtime-secrets design.md D4 and repository-credentials
// spec.md "Secret rejected for source acquisition".
func (s *ProjectService) checkRepoCredential(cfg *domain.ProjectConfig) error {
	if ref, isSecret := s.credentialRefIsSecret(cfg.RepoCredentialRef); isSecret {
		return fmt.Errorf("%w: repo_credential_ref %q is a secret credential and cannot be used for Git access", ErrValidation, ref)
	}
	return nil
}

// checkCIRepoCredentials rejects a secondary ci_repo whose
// credential_ref names a `secret` credential, the same rule
// checkRepoCredential applies to the primary repo. Without it, a
// secret-typed credential_ref on a secondary ci_repo was only caught later
// at git fetch time instead of at project create/update - a known gap
// tracked in the project wiki.
func (s *ProjectService) checkCIRepoCredentials(proj domain.Project) error {
	for _, repo := range proj.Repos {
		if ref, isSecret := s.credentialRefIsSecret(repo.CredentialRef); isSecret {
			return fmt.Errorf("%w: ci_repo %q credential_ref %q is a secret credential and cannot be used for Git access", ErrValidation, repo.Name, ref)
		}
	}
	return nil
}

func validateProject(cfg *domain.ProjectConfig, allowLocal bool) error {
	if strings.TrimSpace(cfg.Name) == "" {
		return fmt.Errorf("%w: project name is required", ErrValidation)
	}
	raw := strings.TrimSpace(cfg.RepoURL)
	ref := strings.TrimSpace(cfg.RepoRef)
	if raw == "" {
		return fmt.Errorf("%w: repo_url is required", ErrValidation)
	}
	if ref == "" {
		return fmt.Errorf("%w: repo_ref is required", ErrValidation)
	}
	// An empty Mode defers to the store's instance-wide default
	// (projectDefaults); see remote-runner-registration design.md D2.
	if !domain.IsValidMode(cfg.Mode) {
		return fmt.Errorf("%w: mode %q is not a recognized execution backend", ErrValidation, cfg.Mode)
	}
	// An empty mode is always fine here: if the server is running at all
	// with allowLocal false, its own startup validation already guarantees
	// the instance mode it would resolve to isn't `local` - see
	// safe-production-execution design.md D1.
	if cfg.Mode == "local" && !allowLocal {
		return fmt.Errorf("%w: mode local: %s", ErrValidation, domain.LocalExecutionDisabledMessage())
	}
	return validateRepoURL(raw)
}

// NewProjectService creates a ProjectService backed by the given store.
func NewProjectService(store domain.ProjectStore) *ProjectService {
	return &ProjectService{store: store}
}

// Create validates and persists a new project.
func (s *ProjectService) Create(cfg *domain.ProjectConfig) error {
	return s.CreateValidated(cfg, nil)
}

// SourceValidator checks a candidate registration against its remote and
// returns the pipeline it parsed there, so the service can derive the
// project's sources from the same parse instead of a second Git operation.
type SourceValidator func(*domain.ProjectConfig) (domain.Project, error)

// CreateValidated validates the complete remote source before persistence.
func (s *ProjectService) CreateValidated(cfg *domain.ProjectConfig, validateSource SourceValidator) error {
	if err := validateProject(cfg, s.allowLocal); err != nil {
		return err
	}
	if cfg.DefaultBranch == "" {
		cfg.DefaultBranch = "main"
	}
	if cfg.PipelineFile == "" {
		cfg.PipelineFile = "pipeline.tf"
	}
	if cfg.PipelineDir == "" {
		cfg.PipelineDir = "ci"
	}
	if err := s.checkRepoCredential(cfg); err != nil {
		return err
	}
	return s.persistValidated(cfg, validateSource)
}

// persistValidated runs the remote validation, then stores the project and its
// derived sources in one store operation, so a failure leaves the registration
// untouched instead of half applied behind an error response. A client-supplied
// Sources value is overwritten here, so no caller can register a source by
// hand. Without a validator only the primary source is known; the next load of
// the configured ref adds the rest.
func (s *ProjectService) persistValidated(cfg *domain.ProjectConfig, validateSource SourceValidator) error {
	var proj domain.Project
	if validateSource != nil {
		var err error
		if proj, err = validateSource(cfg); err != nil {
			return fmt.Errorf("%w: validate project source: %v", ErrValidation, err)
		}
	}
	if err := s.checkCIRepoCredentials(proj); err != nil {
		return err
	}
	cfg.Sources = DeriveSources(cfg, proj)
	return s.store.UpsertWithSources(cfg, cfg.Sources)
}

// Update validates and updates an existing project.
func (s *ProjectService) Update(name string, cfg *domain.ProjectConfig) error {
	return s.UpdateValidated(name, cfg, nil)
}

// UpdateValidated replaces a project only after the candidate source and
// pipeline definition have been validated successfully.
func (s *ProjectService) UpdateValidated(name string, cfg *domain.ProjectConfig, validateSource SourceValidator) error {
	existing, err := s.store.Get(name)
	if err != nil {
		return fmt.Errorf("check project: %w", err)
	}
	if existing == nil {
		return fmt.Errorf("project %q not found", name)
	}
	cfg.Name = name
	if cfg.PipelineFile == "" {
		cfg.PipelineFile = "pipeline.tf"
	}
	if cfg.DefaultBranch == "" {
		cfg.DefaultBranch = "main"
	}
	if cfg.CreatedAt == "" {
		cfg.CreatedAt = existing.CreatedAt
	}
	if cfg.PipelineDir == "" {
		cfg.PipelineDir = "ci"
	}
	// An update whose webhook_secret is absent or empty keeps the stored
	// secret unchanged; rotation requires a non-empty value. See
	// webhook-ingress-hardening design.md D4.
	if cfg.WebhookSecret == "" {
		cfg.WebhookSecret = existing.WebhookSecret
	}
	if err := validateProject(cfg, s.allowLocal); err != nil {
		return err
	}
	if err := s.checkRepoCredential(cfg); err != nil {
		return err
	}
	return s.persistValidated(cfg, validateSource)
}

// Delete removes a project by name.
func (s *ProjectService) Delete(name string) error {
	return s.store.Delete(name)
}

// Get returns a project by name.
func (s *ProjectService) Get(name string) (*domain.ProjectConfig, error) {
	return s.store.Get(name)
}

// List returns all project names.
func (s *ProjectService) List() ([]string, error) {
	return s.store.List()
}

// Load returns all projects.
func (s *ProjectService) Load() (map[string]*domain.ProjectConfig, error) {
	return s.store.Load()
}

// FindByGitLabID returns a project by its GitLab project ID.
func (s *ProjectService) FindByGitLabID(id int) (*domain.ProjectConfig, error) {
	return s.store.FindByGitLabID(id)
}
