## 1. Canonical identity

- [x] 1.1 Add `project.yaml` with `name: Kestrix`, `module: kestrix`, and an empty optional source URL; document that it contains public metadata only.
- [x] 1.2 Change `go.mod` to `module kestrix` and update all active Go imports, version linker comments, and module-specific test commands to the neutral path.
- [x] 1.3 Rename the active dogfood pipeline project/repository identity and changed-repository triggers from `metaci` to `kestrix`, preserving unrelated database/config compatibility names where they are not project identity.

## 2. Provider-neutral tooling and runtime defaults

- [x] 2.1 Update coverage and helper scripts to derive the module prefix from `go list -m` or local package paths instead of a hardcoded provider URL.
- [x] 2.2 Remove the old provider-owned image default from Docker build, push, deploy, and Compose configuration; use a neutral local tag or require explicit `DOCKER_IMAGE` for registry operations.
- [x] 2.3 Update build/version linker flags and any active generated metadata so they use the canonical Kestrix module identity.

## 3. Documentation and source location

- [x] 3.1 Replace stale provider/owner/repository links in active README, UI business content, and project documentation with relative links or the optional `project.yaml` source URL.
- [x] 3.2 Add a concise project-identity section explaining the difference between Kestrix product name, Go module path, and replaceable repository location.

## 4. Verification and migration guardrails

- [x] 4.1 Add an automated check that fails when `gitlab.com/lunaris-bot/metaci` appears in active code, scripts, Docker configuration, or user-facing documentation, with explicit exclusions for historical OpenSpec artifacts.
- [x] 4.2 Run `make fmt-check`, `make vet`, `make test`, `make build`, and the identity check; verify `go list -m` returns `kestrix` and the repository compiles with updated imports.
- [x] 4.3 Validate the OpenSpec change and document the downstream migration from the former module path.
