# MetaCI

> Repo-agnostic CI/CD Control Plane with HCL-based DAG orchestration.

MetaCI is a decentralized CI/CD orchestrator that treats repositories as data
sources rather than pipeline owners. Pipelines are defined in HCL (via a custom
Terraform/OpenTofu provider), evaluated against incoming webhooks, and dispatched
to user-supplied GitLab Runners.

## Status

Proof of Concept — early stage.

## License

See [LICENSE](./LICENSE). All rights reserved. No usage without explicit permission.
