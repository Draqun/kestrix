# Kestrix Docker resources

The Docker Compose configuration uses `kestrix` for application, Redis,
business-site, PostgreSQL database, and test-database identities. Runtime
configuration uses only the canonical `KESTRIX_*` environment variables.

## Migration from the old names

Renaming the Compose resources does not automatically migrate or remove
containers or volumes created with previous names. Existing PostgreSQL data
must be migrated by an administrator before switching the connection URL.
Before switching an existing installation, stop the old stack and explicitly
recreate or migrate its resources as appropriate for the environment.

Do not run `make clean` unless removing the Compose volumes is intentional.
