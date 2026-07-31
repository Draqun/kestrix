## 1. Remove implicit startup registration

- [x] 1.1 Remove the `cfg.PipelineDir` parse-and-upsert block from `internal/web.NewServerWithDeps`.
- [x] 1.2 Update development Docker startup configuration so the server does not use `examples/demo-mono` as an implicit project seed.

## 2. Verify explicit project persistence

- [x] 2.1 Add or update backend tests proving that startup with a configured pipeline directory does not create a project and that explicit CRUD registration still works.
- [x] 2.2 Update project-registration documentation/spec references and any affected development instructions.
- [x] 2.3 Run formatting, targeted tests, build, and the relevant full test suite; confirm the change status is complete.
