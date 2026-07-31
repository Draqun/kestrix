## Context

The Go module path currently doubles as a GitLab repository URL: `gitlab.com/lunaris-bot/metaci`. Go source imports, coverage tooling, linker flags, Docker defaults, CI examples, and documentation therefore encode a provider, owner, and obsolete product name. Kestrix is an application rather than a reusable public library, so its internal package imports need a stable project identity while repository hosting remains deployment metadata.

## Goals / Non-Goals

**Goals:**

- Use `kestrix` as the canonical Go module and internal import prefix.
- Keep product identity (`Kestrix`) separate from source location and container registry.
- Make provider-specific source and image references configurable or explicitly supplied.
- Remove the old `gitlab.com/lunaris-bot/metaci` identity from active source, scripts, configuration, and user-facing docs.
- Provide automated guards against reintroducing the old coupling.

**Non-Goals:**

- Selecting a new Git provider, owner, repository URL, or registry.
- Rewriting historical OpenSpec archives.
- Changing runtime API behavior or stored project data.

## Decisions

### Use `kestrix` as the Go module path

Change `go.mod` to `module kestrix` and update internal imports accordingly. This path is intentionally not a URL owned by a Git provider. It is suitable for this application because consumers do not import its `internal/` packages; a future public library can receive a separately governed vanity module path.

Alternative considered: a vanity URL such as `go.example/kestrix`. It would be more suitable for a public library, but choosing a domain now would recreate an ownership decision that the change is meant to avoid.

### Add repository metadata separate from Go identity

Add a committed `project.yaml` containing the product name, module path, and optional source URL, for example:

```yaml
name: Kestrix
module: kestrix
source:
  url: ""
```

The source URL is informational and may be filled by the current deployment/owner. Tooling and docs use relative repository paths when it is empty. No credentials or tokens may be stored in this file.

### Make external references explicit

Container image names and publish/deploy URLs SHALL come from existing environment overrides or a required explicit value. A provider-owned default is not acceptable. Build/version checks derive the module prefix from `go list -m` or local paths rather than duplicating it in shell scripts.

### Rename the dogfood pipeline identity

Update the active `ci/pipeline.tf` project/repository identifier from `metaci` to `kestrix`, because it describes the current product and is not a Go module compatibility alias. Historical docs and persisted user configuration are outside the migration’s automatic scope.

### Architecture

```text
                 +----------------------+
                 | project.yaml         |
                 | name: Kestrix        |
                 | module: kestrix      |
                 | source.url: optional  |
                 +----------+-----------+
                            |
          +-----------------+------------------+
          v                                    v
   go.mod/imports                       docs + release tooling
   module: kestrix                      source URL only if set

   registry/provider-specific image URL --> env/config, never module imports
```

### Security considerations

- `project.yaml` must contain only public metadata; source URLs are not credentials.
- Existing registry credentials remain environment-only.
- Validation should report stale provider strings without printing secret environment values.

## Risks / Trade-offs

- [Risk] Downstream tooling imports the old module path. → Mitigation: this is an explicit breaking change; provide a migration note and update all in-repository consumers in one change.
- [Risk] `module kestrix` is not fetchable as a public module path. → Mitigation: Kestrix is currently an application; introduce a vanity path only when public library distribution is a goal.
- [Risk] A missing source URL leaves external links unavailable. → Mitigation: repository-relative links remain valid locally, and source URL is optional metadata that can be populated later.
- [Risk] Old names remain in historical or compatibility data. → Mitigation: scope validation to active code/config/docs and document exclusions.

## Migration Plan

1. Add `project.yaml` and change the Go module/import prefix.
2. Update build, coverage, CI, Docker, and documentation references.
3. Run formatting, vet, unit tests, and stale-reference checks.
4. Update any external consumers from `gitlab.com/lunaris-bot/metaci/...` to `kestrix/...` or use the project’s public interfaces.
5. Roll back by reverting the module/import and metadata changes together; do not partially retain mixed import prefixes.

## Open Questions

- What source URL should eventually be recorded in `project.yaml` once the hosting provider and owner are finalized?
- If Kestrix later publishes reusable Go libraries, which owned vanity domain should replace the application-only `kestrix` module path?
