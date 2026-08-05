# Scenario verification

Verified on 2026-07-31. Test names below are automated unless explicitly
marked as an operational check.

## Pipeline definition

| Scenario | Verification |
|---|---|
| Declare a monorepo subpath | `TestParseRepositoryRejectsEscapingLocalPath`, `TestLocalAcquirerResolvesMonorepoAndRejectsEscape`, and runner `TestRepoDir_*` cases |
| Declare an additional remote repository | `TestParseRemoteRepositoryDeclaration`, `TestSourceSessionsAcquireEveryRemoteBeforeTasksAndCleanupOnFailure`, and `TestSourceSessionsUseIsolatedPathsAndPersistResolvedMetadata` |
| Reject an ambiguous source | `TestParseRepositoryRequiresExactlyOneSource` and `TestParseRemoteRepositoryRequiresRef` |
| View a remote pipeline | `TestOpenProjectSourceUsesRemoteSessionContract`, `TestParseProjectPipelineSuccessCleansSource`, and `TestRemoteProjectWorkflowE2E` |
| Attempt to save a remote pipeline | `TestHandleSaveProjectPipeline_Happy` verifies HTTP 405; `UI API surfaces the explicit read-only pipeline response` verifies the client error |

## Project registration

| Scenario | Verification |
|---|---|
| Register a public remote project | `TestPG_UpsertAndGet` and `TestRemoteProjectWorkflowE2E` |
| Register a private remote project | `TestPG_CredentialLifecycleStoresCiphertextOnly`, `TestCredentialCreateResponseIsMetadataOnly`, and cache/run JSON secret checks |
| Reject local project registration | `TestProjectService_CreateRejectsInvalidRemoteForms`, `TestHandleCreateProjectRejectsRepoPath`, and `TestRemoteProjectWorkflowE2E` |
| Create validates source and pipeline | `TestProjectServiceValidatedSourceFailuresDoNotPersist`, `TestParseProjectPipelineRejectsUnmaterializableProject`, and `TestRemoteProjectWorkflowE2E` |
| Invalid source update preserves the old project | `TestProjectServiceValidatedSourceFailuresDoNotPersist` and `TestProjectService_Update_ValidatesMergedConfig` |
| Clear an optional credential | `TestProjectService_UpdateIsFullReplacementAndClearsCredential` and `an empty credential reference explicitly selects public access` |
| Create project from the UI | `remote project payload contains only the remote source contract`, `project onboarding offers only active credential metadata`, and `make ui-build` |
| Incomplete form | `remote URL, ref, pipeline directory, and create name are required` and `UI API creates a project with the remote-only payload` |
| First private project needs a new credential | `UI can create and refresh credential metadata before a project exists`, `a credential created during onboarding becomes the selected reference`, and `TestCredentialHandlersLifecycleAndErrors` |
| View additional repositories | `parsed repository labels distinguish subpaths from remotes`; operational check: `make ui-build` compiles the read-only repository cards in `Projects.jsx` |
| Upgrade with a local-only project | `TestPG_MigrationPreflightNamesLegacyLocalProjects` and `TestPG_RemoteProjectSchemaConstraints` |

## Remote source acquisition

| Scenario | Verification |
|---|---|
| Read a project definition | `TestOpenProjectSourceUsesRemoteSessionContract`, `TestLoadProjectPipelineCleansSessionOnParseFailure`, and `TestParseProjectPipelineSuccessCleansSource` |
| Execute a project with additional remote repositories | `TestSourceSessionsAcquireEveryRemoteBeforeTasksAndCleanupOnFailure` and `TestOpenRecordedRunSourcesReturnsPrimaryAndAdditionalPaths` |
| Branch resolves successfully | `TestGitCacheSeedsRefreshesAndResolvesBranchTagCommit`, `TestRemoteProjectWorkflowE2E`, and `TestPG_UpdateRunSourcesPersistsRemoteRevisionWithoutCheckoutPath` |
| Webhook identifies an event commit | `TestWebhookLoadsPipelineAtImmutableEventRevision` |
| Manual task is approved after the branch moves | `TestManualApprovalUsesStoredSnapshotAndExactRecordedSources` |
| Revision does not exist | `TestGitAcquirerMissingRefCleansWorkspace`, `TestGitCacheRefreshFailureDoesNotUseStaleBranch`, and `TestSourceAcquisitionFailurePreventsTaskExecution` |
| Concurrent consumers | `TestGitCacheConcurrentSessionsAreIsolated` and `TestConcurrentRunsReceiveDistinctSourceSessionsAndCleanThem` |
| Acquisition timeout | `TestGitAcquirerCommandTimeout` and `TestSourceSessionTimeoutPreventsTasksAndCleansPartialAcquisition` |
| Private cached source credential is missing or revoked | `TestGitCacheHitStillResolvesCredentialAndFailsClosed` and deployment E2E credential revocation check |
| Embedded credentials in URL | `TestParseRepositoryRequiresExactlyOneSource`, `TestGitAcquirerRetriesCloneFailureAndRedactsURL`, and cache secret scans |
| Cache maintenance runs before manual approval | `TestGitCachePinsSurviveRestartAndProtectRetention` and `TestRunServicePersistsPinsAndRetainsSourcesForManualWork` |
| Run becomes terminal | `TestRunServiceReleasesSourcesWhenRunIsTerminal`, `TestRunServicePanicReleasesSourcePins`, and `TestGitCachePinsSurviveRestartAndProtectRetention` |

## Remote source cache

| Scenario | Verification |
|---|---|
| First acquisition seeds the cache | `TestGitCacheSeedsRefreshesAndResolvesBranchTagCommit` and `TestRemoteProjectWorkflowE2E` |
| Repeated acquisition reuses Git objects | `TestGitCacheSeedsRefreshesAndResolvesBranchTagCommit` and the single-entry assertion in `TestRemoteProjectWorkflowE2E` |
| Cache identity contains no secret | `TestGitCacheIdentityMetadataAndRunJSONContainNoSecretsOrPaths`, `TestCredentialMaterialDoesNotPutSecretInEnvironment`, and deployment E2E secret scan |
| Mutable branch advances | `TestGitCacheSeedsRefreshesAndResolvesBranchTagCommit` and `TestRemoteProjectWorkflowE2E` |
| Remote refresh fails | `TestGitCacheRefreshFailureDoesNotUseStaleBranch` and `TestGitCacheHitStillResolvesCredentialAndFailsClosed` |
| Concurrent acquisition of one source | `TestGitCacheConcurrentSessionsAreIsolated` and `TestCacheLockWaiterHonorsCancellation` |
| Session modifies source files | `TestGitCacheConcurrentSessionsAreIsolated` writes a session-only file and proves it is invisible to the other checkout |
| Credential is revoked after cache population | `TestGitCacheHitStillResolvesCredentialAndFailsClosed` and deployment E2E revocation check |
| Cache entry is corrupt | `TestGitCacheRebuildsCorruptEntryOnce` and `TestGitCacheCorruptEntryFailsCleanlyWhenRebuildFails` |
| Retention reaches a pinned entry | `TestGitCachePinsSurviveRestartAndProtectRetention` |

## Runtime configuration

| Scenario | Verification |
|---|---|
| Configure a persistent cache directory | `TestSourceCacheConfigPrecedenceAndValidation`, `TestSourceCacheValidationFilesystemFailures`, and deployment E2E volume setup |
| Invalid retention limit | `TestSourceCacheValidationRejectsInvalidValuesAndFilePath` and `TestLoadEnvironmentRejectsInvalidSourceCacheSettings` |
| Container deployment restarts | `make docker-e2e` restarts the Kestrix container and verifies the source-cache volume is reused |

## Webhook receiver

| Scenario | Verification |
|---|---|
| Load pipeline from webhook revision | `TestWebhookLoadsPipelineAtImmutableEventRevision` and `TestFullWebhookFlow` |
| Event revision is unavailable | `TestWebhookRejectsRegisteredPushWithoutImmutableRevision` and `TestRunHandlersRejectUnmaterializableProject` |

## Verification commands

The following Makefile targets completed successfully:

- `make quality`
- `make staticcheck`
- `make test-integration` with `KESTRIX_TEST_DATABASE_URL` pointing to the
  guarded `kestrix_test` database
- `make runner-integration`
- `make test-e2e`
- `make docker-e2e`
- `make ui-test`
- `make ui-build`
- `make build`
- `make swagger`

Unit coverage was 100.0%. The PostgreSQL integration profile was 96.2% overall,
including 82.1% for `internal/store`, above its 80% gate.
