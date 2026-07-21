## 1. Documentation Content

- [x] 1.1 Create `docs/dag-engine.md` covering trigger expressions, dependency ordering, cycle detection, validation, and manual tasks
- [x] 1.2 Add links to the DAG engine docs from `docs/overview.md` and `docs/architecture.md`

## 2. Build Integration

- [x] 2.1 Run `bash scripts/build-docs.sh` to verify the page is generated in `ui/dist/docs`
- [x] 2.2 Confirm `ui/dist/docs/dag-engine.html` exists after the build

## 3. Spec Update

- [x] 3.1 Verify the delta spec at `openspec/changes/dag-engine-docs/specs/dag-engine/spec.md` reflects satisfied REQ-1

## 4. Verification

- [x] 4.1 Run `make test` to ensure no regressions
- [x] 4.2 Run `npm run build` to ensure the UI still builds
