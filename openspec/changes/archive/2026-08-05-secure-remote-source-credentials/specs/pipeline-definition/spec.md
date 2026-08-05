## MODIFIED Requirements

### Requirement: Repository Declaration (implemented)

A project SHALL declare one or more **ci_repo** blocks, each representing a
source repository or subdirectory in a monorepo. A local repository SHALL
define exactly one `path`. A remote repository SHALL define exactly one `url`
and SHALL define a non-empty `ref` identifying a branch, tag, or commit. A
repository MAY define `credential_ref` for private Git transport. Credential
values SHALL NOT be embedded in URLs or stored in HCL.

#### Scenario: Declare a local monorepo subpath

```
WHEN a user writes ci_repo "auth" { path = "src/auth" kind = "subpath" }
THEN the system SHALL register "auth" as a local repository with path
"src/auth"
AND the declaration SHALL remain compatible with local execution
```

#### Scenario: Declare a remote repository

```
WHEN a user writes ci_repo "auth" { url = "ssh://git@example.test/team/auth.git" ref = "v2.1.0" }
THEN the system SHALL accept the declaration
AND the source acquisition SHALL use the requested ref
AND the run SHALL record the resolved commit SHA
```

#### Scenario: Missing remote ref

```
WHEN a remote ci_repo defines url but omits ref or sets it to an empty value
THEN validation SHALL fail with an actionable error
AND Kestrix SHALL not start the pipeline
```

#### Scenario: Embedded credentials

```
WHEN a repository URL contains a password, token, or unsupported userinfo
THEN validation SHALL reject the declaration
AND the error SHALL redact sensitive URL components
```
