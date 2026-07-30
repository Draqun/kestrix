# Kestrix Docker resources

The Docker Compose configuration uses `kestrix` for application, Redis, and
business-site container identities. PostgreSQL database and test-database
names remain `metaci` and `metaci_test` because they are part of the existing
application and test configuration contract. The existing `METACI_*`
environment variables also remain unchanged.

## Migration from the old names

Renaming the Compose resources does not automatically migrate or remove
containers or volumes created with the former `metaci` names.
Before switching an existing installation, stop the old stack and explicitly
recreate or migrate its resources as appropriate for the environment.

Do not run `make clean` unless removing the Compose volumes is intentional.
