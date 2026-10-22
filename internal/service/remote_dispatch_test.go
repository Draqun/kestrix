package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"kestrix/internal/dag"
	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/model"
	"kestrix/internal/variables"
)

// ── archiveWorkDir ──────────────────────────────────────────────────

func TestArchiveWorkDir_EmptyRepoCheckout(t *testing.T) {
	dir := t.TempDir()
	archive, err := archiveWorkDir(dir)
	if err != nil {
		t.Fatalf("archiveWorkDir: %v", err)
	}
	if len(archive) == 0 {
		t.Fatal("expected a valid (if minimal) gzip archive, got 0 bytes")
	}
}

func TestArchiveWorkDir_OversizedRepoCheckout(t *testing.T) {
	dir := t.TempDir()
	// "Oversized" here means large enough to exercise multiple io.Copy
	// buffer fills, not a specific byte threshold - design.md sets no cap.
	big := make([]byte, 5*1024*1024)
	for i := range big {
		big[i] = byte(i % 251)
	}
	if err := os.WriteFile(filepath.Join(dir, "large.bin"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "small.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive, err := archiveWorkDir(dir)
	if err != nil {
		t.Fatalf("archiveWorkDir: %v", err)
	}
	if len(archive) == 0 {
		t.Fatal("expected a non-empty archive")
	}
	if len(archive) >= len(big) {
		t.Errorf("archive (%d bytes) should be smaller than the raw input (%d bytes) thanks to gzip", len(archive), len(big))
	}
}

func TestArchiveWorkDir_IncludesSymlinks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", filepath.Join(dir, "link.txt")); err != nil {
		t.Skipf("symlinks not supported in this environment: %v", err)
	}
	archive, err := archiveWorkDir(dir)
	if err != nil {
		t.Fatalf("archiveWorkDir: %v", err)
	}
	if len(archive) == 0 {
		t.Fatal("expected a non-empty archive")
	}
}

// TestArchiveWorkDir_UnreadableFilePropagatesError exercises the open
// call's failure branch in archiveWorkDir's Walk callback, via
// archiveWorkDirWithOpener's injected-opener seam. A chmod-0o000 file is
// not a portable way to trigger this: root bypasses DAC permission checks
// entirely, so that approach silently skips under a root-run CI job
// (GitLab's Docker executor commonly runs as root), permanently excluding
// this line from coverage there while passing locally as a non-root user.
func TestArchiveWorkDir_UnreadableFilePropagatesError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	injectedErr := errors.New("injected open failure")
	_, err := archiveWorkDirWithOpener(dir, func(string) (*os.File, error) {
		return nil, injectedErr
	})
	if !errors.Is(err, injectedErr) {
		t.Fatalf("archiveWorkDirWithOpener error = %v, want it to wrap %v", err, injectedErr)
	}
}

func TestArchiveWorkDir_MissingDirectory(t *testing.T) {
	_, err := archiveWorkDir(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}

// ── buildRemoteJobPayload: isolation ───────────────────────────────

// TestBuildRemoteJobPayload_ExcludesControlPlaneVariables pins
// task-execution-isolation's allowlist extension to remote dispatch
// (tasks.md 2.2): the payload's Variables are exactly what
// domain.VariableResolver.Get returns - pipeline-declared values only -
// never DATABASE_URL, a KESTRIX_* name, or anything named by
// KESTRIX_RUNNER_ENV_PASSTHROUGH, because that passthrough tier lives in
// internal/config's TaskEnv (host-process construction), a layer this
// payload never touches.
func TestBuildRemoteJobPayload_ExcludesControlPlaneVariables(t *testing.T) {
	vs := variables.New()
	vs.SetProject("REGISTRY_PASSWORD", "resolved-secret-value")
	task := model.Task{Name: "build", Execute: domain.ExecuteSpec{Repo: "app", Script: "echo hi"}}

	payload := buildRemoteJobPayload(task, nil, "", vs, nil, nil)

	for _, forbidden := range []string{"DATABASE_URL", "KESTRIX_MASTER_KEY_PATH", "KESTRIX_JWT_SIGNING_KEY_PATH", "KESTRIX_WEBHOOK_SECRET", "KESTRIX_GITLAB_TOKEN", "DOCKER_REGISTRY", "DOCKER_PASSWORD", "PATH", "HOME"} {
		if _, present := payload.Variables[forbidden]; present {
			t.Fatalf("payload leaked control-plane/host variable %q", forbidden)
		}
	}
	if payload.Variables["REGISTRY_PASSWORD"] != "resolved-secret-value" {
		t.Fatalf("expected the pipeline-declared variable to be present, got %v", payload.Variables)
	}
}

func TestBuildRemoteJobPayload_CarriesSecretValues(t *testing.T) {
	task := model.Task{Name: "build", Execute: domain.ExecuteSpec{Repo: "app", Script: "echo hi"}}
	payload := buildRemoteJobPayload(task, nil, "", nil, nil, []string{"resolved-secret-value"})
	if len(payload.SecretValues) != 1 || payload.SecretValues[0] != "resolved-secret-value" {
		t.Fatalf("SecretValues = %q, want the argument", payload.SecretValues)
	}
}

func TestBuildRemoteJobPayload_ExcludesRepositoryCredentialValue(t *testing.T) {
	// The payload takes no credential parameter at all - archiveWorkDir
	// packages only the checkout's file contents, and buildRemoteJobPayload
	// has no field for a credential, so there is no way for one to appear.
	task := model.Task{Name: "build", Execute: domain.ExecuteSpec{Repo: "app", Script: "echo hi"}}
	payload := buildRemoteJobPayload(task, nil, "", nil, []byte("archive-bytes"), nil)
	if payload.Task.Execute.Repo != "app" {
		t.Fatalf("unexpected task in payload: %+v", payload.Task)
	}
	// Struct-level guarantee: domain.RemoteJobPayload's fields are Task,
	// Repos, DefaultImage, Variables, and Archive - none named for a
	// credential; Repos itself carries only Name/Path/Kind (see its doc),
	// never URL or CredentialRef.
	if payload.Archive == nil || string(payload.Archive) != "archive-bytes" {
		t.Fatalf("archive not carried through: %v", payload.Archive)
	}
}

func TestBuildRemoteJobPayload_CarriesReposForWorkdirResolution(t *testing.T) {
	task := model.Task{Name: "build", Execute: domain.ExecuteSpec{Repo: "app", Script: "echo hi"}}
	repos := []domain.Repo{{Name: "app", Path: "services/app"}}
	payload := buildRemoteJobPayload(task, repos, "golang:1.26", nil, nil, nil)
	if len(payload.Repos) != 1 || payload.Repos[0].Path != "services/app" {
		t.Fatalf("expected the project's repo definitions (with Path) carried through, got %+v", payload.Repos)
	}
	if payload.DefaultImage != "golang:1.26" {
		t.Fatalf("DefaultImage = %q, want golang:1.26", payload.DefaultImage)
	}
}

func TestBuildRemoteJobPayload_SanitizesRepoURLAndCredentialRef(t *testing.T) {
	task := model.Task{Name: "build", Execute: domain.ExecuteSpec{Repo: "app", Script: "echo hi"}}
	repos := []domain.Repo{{Name: "app", Path: ".", URL: "https://git.example/app.git", CredentialRef: "cred_secret"}}
	payload := buildRemoteJobPayload(task, repos, "", nil, nil, nil)
	if len(payload.Repos) != 1 {
		t.Fatalf("expected one repo in payload, got %+v", payload.Repos)
	}
	if payload.Repos[0].URL != "" || payload.Repos[0].CredentialRef != "" {
		t.Fatalf("payload leaked repo URL/CredentialRef: %+v", payload.Repos[0])
	}
	if payload.Repos[0].Name != "app" || payload.Repos[0].Path != "." {
		t.Fatalf("expected Name/Path preserved, got %+v", payload.Repos[0])
	}
}

func TestBuildRemoteJobPayload_NamesNoEngineOrImageSelector(t *testing.T) {
	task := model.Task{
		Name: "build",
		Execute: domain.ExecuteSpec{
			Repo:   "app",
			Script: "echo hi",
			Image:  "golang:1.26", // set on the TASK itself, as ordinary pipeline data
		},
	}
	payload := buildRemoteJobPayload(task, nil, "", nil, nil, nil)
	// design.md D3: Kestrix sends the task's script/variables/source and the
	// project's repo/image data as already declared in the pipeline;
	// nothing beyond that says "docker" or picks an engine on the runner's
	// behalf. RemoteJobPayload has no separate engine-selector field of its
	// own - DefaultImage/Task.Execute.Image are pipeline data the runner's
	// own local config still decides whether to honor at all.
	if payload.Task.Name != "build" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

// ── startRemote / RunService.Start remote branch ───────────────────

func TestRunService_Start_RemoteMode_DispatchesEligibleTasksNoInProcessCall(t *testing.T) {
	proj := &model.Project{
		Name:  "proj",
		Repos: []domain.Repo{{Name: "app"}},
		Tasks: []model.Task{
			{Name: "a", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "echo a"}},
			{Name: "b", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "echo b"}},
		},
	}
	runStore := &mocks.MockRunStore{CreateRunID: 42}
	remoteJobs := &mocks.MockRemoteJobStore{}
	pipeline := &mocks.MockPipelineRunner{}
	svc := NewRunService(runStore, pipeline, pipeline)
	svc.SetRemoteJobStore(remoteJobs)
	cleanedUp := false

	runID, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(), Variables: variables.New(),
		WorkspaceCleanup: func() { cleanedUp = true },
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !cleanedUp {
		t.Fatal("expected WorkspaceCleanup to run once the archive was built")
	}
	if runID != 42 {
		t.Fatalf("runID = %d, want 42", runID)
	}
	if len(remoteJobs.CreateRemoteJobCalls) != 2 {
		t.Fatalf("CreateRemoteJob calls = %d, want 2 (one per task)", len(remoteJobs.CreateRemoteJobCalls))
	}
	if len(pipeline.RunCalls) != 0 {
		t.Fatalf("expected no in-process Executor.Run call, got %d", len(pipeline.RunCalls))
	}
	if len(pipeline.TaskRunCalls) != 0 {
		t.Fatalf("expected no in-process Executor.RunTask call, got %d", len(pipeline.TaskRunCalls))
	}
}

func TestRunService_Start_RemoteMode_DependentTaskNotDispatchedYet(t *testing.T) {
	proj := &model.Project{
		Name:  "proj",
		Repos: []domain.Repo{{Name: "app"}},
		Tasks: []model.Task{
			{Name: "build", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "echo build"}},
			{Name: "deploy", Trigger: "repo.app.changed", DependsOn: []string{"build"}, Execute: domain.ExecuteSpec{Repo: "app", Script: "echo deploy"}},
		},
	}
	runStore := &mocks.MockRunStore{CreateRunID: 1}
	remoteJobs := &mocks.MockRemoteJobStore{}
	pipeline := &mocks.MockPipelineRunner{}
	svc := NewRunService(runStore, pipeline, pipeline)
	svc.SetRemoteJobStore(remoteJobs)

	if _, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(), Variables: variables.New(),
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(remoteJobs.CreateRemoteJobCalls) != 1 || remoteJobs.CreateRemoteJobCalls[0].TaskName != "build" {
		t.Fatalf("expected exactly one dispatched job (build), got %+v", remoteJobs.CreateRemoteJobCalls)
	}
}

// TestRunService_Start_RemoteMode_BypassesConcurrencySlots pins
// remote-runner-registration tasks.md 2.3: a remote-mode run's tasks
// dispatch immediately even with every in-process concurrency slot
// occupied - remote dispatch never touches s.slots at all.
func TestRunService_Start_RemoteMode_BypassesConcurrencySlots(t *testing.T) {
	proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
		{Name: "a", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
	}}
	runStore := &mocks.MockRunStore{CreateRunID: 9}
	remoteJobs := &mocks.MockRemoteJobStore{}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(remoteJobs)
	svc.SetMaxConcurrentRuns(1)
	if !svc.tryAcquireSlot() {
		t.Fatal("setup: could not occupy the only slot")
	}
	defer svc.releaseSlot()

	runID, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(), Variables: variables.New(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if runID != 9 {
		t.Fatalf("runID = %d, want 9", runID)
	}
	if len(remoteJobs.CreateRemoteJobCalls) != 1 {
		t.Fatalf("expected immediate dispatch despite the occupied slot, got %d CreateRemoteJob calls", len(remoteJobs.CreateRemoteJobCalls))
	}
	if len(runStore.SetRunQueuedCalls) != 0 {
		t.Fatalf("expected the run never marked queued, got %d SetRunQueued calls", len(runStore.SetRunQueuedCalls))
	}
}

// ── RunTask / StartTask / ApproveManualTask remote branch (tasks.md 2.4) ──

func TestRunService_RunTask_RemoteMode_DispatchesJobReturnsDispatchedTrue(t *testing.T) {
	proj := &model.Project{Name: "proj", Tasks: []model.Task{
		{Name: "deploy", Manual: true, Execute: domain.ExecuteSpec{Repo: "app", Script: "echo deploy"}},
	}}
	runStore := &mocks.MockRunStore{}
	remoteJobs := &mocks.MockRemoteJobStore{}
	pipeline := &mocks.MockPipelineRunner{}
	svc := NewRunService(runStore, pipeline, pipeline)
	svc.SetRemoteJobStore(remoteJobs)
	cleanedUp := false

	tr, dispatched, err := svc.RunTask(context.Background(), 3, "proj", proj, "deploy", domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(), Variables: variables.New(), ResolvedSecretValues: []string{"S3cr3t-Remote-Value"},
		WorkspaceCleanup: func() { cleanedUp = true },
	})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if !cleanedUp {
		t.Fatal("expected WorkspaceCleanup to run for a dispatched task")
	}
	if !dispatched {
		t.Fatal("expected Dispatched=true for a remote-mode project")
	}
	if tr.Name != "deploy" || tr.Status != "pending" {
		t.Fatalf("unexpected task result: %+v", tr)
	}
	if len(remoteJobs.CreateRemoteJobCalls) != 1 || remoteJobs.CreateRemoteJobCalls[0].TaskName != "deploy" {
		t.Fatalf("expected one CreateRemoteJob call for deploy, got %+v", remoteJobs.CreateRemoteJobCalls)
	}
	if got := remoteJobs.CreateRemoteJobCalls[0].Payload.SecretValues; len(got) != 1 || got[0] != "S3cr3t-Remote-Value" {
		t.Fatalf("single-task job secret values = %q, want the run's resolved secret", got)
	}
	if len(pipeline.TaskRunCalls) != 0 {
		t.Fatalf("expected no in-process TaskRunner.RunTask call, got %d", len(pipeline.TaskRunCalls))
	}
	if ids := activeRunIDs(t, svc, "proj"); len(ids) != 0 {
		t.Fatalf("expected no active entry registered, got %v", ids)
	}
}

func TestRunService_RunTask_RemoteMode_LocalNonRemoteStillReturnsDispatchedFalse(t *testing.T) {
	proj := &model.Project{Name: "proj", Tasks: []model.Task{{Name: "t", Execute: domain.ExecuteSpec{Repo: "app", Script: "echo hi"}}}}
	runStore := &mocks.MockRunStore{}
	pipeline := &mocks.MockPipelineRunner{}
	svc := NewRunService(runStore, pipeline, pipeline)
	svc.SetRemoteJobStore(&mocks.MockRemoteJobStore{})

	tr, dispatched, err := svc.RunTask(context.Background(), 1, "proj", proj, "t", domain.RunnerOptions{})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if dispatched {
		t.Fatal("expected Dispatched=false for a non-remote project even with a remoteJobStore configured")
	}
	if len(pipeline.TaskRunCalls) != 1 {
		t.Fatalf("expected the in-process task runner invoked, got %d calls", len(pipeline.TaskRunCalls))
	}
	_ = tr
}

func TestRunService_RunTask_RemoteMode_UnknownTaskErrors(t *testing.T) {
	proj := &model.Project{Name: "proj", Tasks: []model.Task{}}
	svc := NewRunService(&mocks.MockRunStore{}, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&mocks.MockRemoteJobStore{})
	_, dispatched, err := svc.RunTask(context.Background(), 1, "proj", proj, "missing", domain.RunnerOptions{Mode: "remote", WorkDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected an error for an unknown task name")
	}
	if dispatched {
		t.Fatal("expected Dispatched=false when dispatch failed")
	}
}

func TestRunService_RunTask_RemoteMode_ArchiveErrorPropagates(t *testing.T) {
	proj := &model.Project{Name: "proj", Tasks: []model.Task{{Name: "t"}}}
	svc := NewRunService(&mocks.MockRunStore{}, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&mocks.MockRemoteJobStore{})
	_, dispatched, err := svc.RunTask(context.Background(), 1, "proj", proj, "t", domain.RunnerOptions{
		Mode: "remote", WorkDir: filepath.Join(t.TempDir(), "does-not-exist"),
	})
	if err == nil {
		t.Fatal("expected an error when the archive cannot be built")
	}
	if dispatched {
		t.Fatal("expected Dispatched=false on failure")
	}
}

func TestRunService_RunTask_RemoteMode_CreateRemoteJobErrorPropagates(t *testing.T) {
	proj := &model.Project{Name: "proj", Tasks: []model.Task{{Name: "t"}}}
	svc := NewRunService(&mocks.MockRunStore{}, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&mocks.MockRemoteJobStore{CreateRemoteJobErr: context.DeadlineExceeded})
	_, dispatched, err := svc.RunTask(context.Background(), 1, "proj", proj, "t", domain.RunnerOptions{Mode: "remote", WorkDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected CreateRemoteJob's error to propagate")
	}
	if dispatched {
		t.Fatal("expected Dispatched=false on failure")
	}
}

func TestRunService_StartTask_RemoteMode_DispatchesSynchronouslyNoQueueing(t *testing.T) {
	proj := &model.Project{Name: "proj", Tasks: []model.Task{{Name: "t", Execute: domain.ExecuteSpec{Repo: "app", Script: "echo hi"}}}}
	runStore := &mocks.MockRunStore{}
	remoteJobs := &mocks.MockRemoteJobStore{}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(remoteJobs)

	queued, err := svc.StartTask(context.Background(), 4, "proj", proj, "t", domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(), Variables: variables.New(),
	})
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if queued {
		t.Fatal("expected queued=false: remote dispatch is immediate, never queued")
	}
	if len(remoteJobs.CreateRemoteJobCalls) != 1 {
		t.Fatalf("expected one CreateRemoteJob call, got %d", len(remoteJobs.CreateRemoteJobCalls))
	}
	if ids := activeRunIDs(t, svc, "proj"); len(ids) != 0 {
		t.Fatalf("expected no active entry, got %v", ids)
	}
}

func TestRunService_StartTask_RemoteMode_DispatchErrorPropagates(t *testing.T) {
	proj := &model.Project{Name: "proj", Tasks: []model.Task{{Name: "t"}}}
	svc := NewRunService(&mocks.MockRunStore{}, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&mocks.MockRemoteJobStore{CreateRemoteJobErr: context.DeadlineExceeded})
	if _, err := svc.StartTask(context.Background(), 1, "proj", proj, "t", domain.RunnerOptions{Mode: "remote", WorkDir: t.TempDir()}); err == nil {
		t.Fatal("expected the dispatch error to propagate")
	}
}

func TestRunService_ApproveManualTask_RemoteMode_DispatchesInsteadOfExecuting(t *testing.T) {
	proj := &model.Project{Name: "proj", Tasks: []model.Task{
		{Name: "deploy", Manual: true, Execute: domain.ExecuteSpec{Repo: "app", Script: "echo deploy"}},
	}}
	run := &domain.Run{ID: 5, ProjectName: "proj", PipelineDef: proj, TaskResults: []domain.TaskResult{{Name: "deploy", Status: "manual"}}}
	runStore := &mocks.MockRunStore{GetRunResult: run}
	remoteJobs := &mocks.MockRemoteJobStore{}
	pipeline := &mocks.MockPipelineRunner{}
	svc := NewRunService(runStore, pipeline, pipeline)
	svc.SetRemoteJobStore(remoteJobs)

	tr, err := svc.ApproveManualTask(context.Background(), 5, "proj", proj, "deploy", domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(), Variables: variables.New(), Actor: "user:erin", ResolvedSecretValues: []string{"S3cr3t-Remote-Value"},
	})
	if err != nil {
		t.Fatalf("ApproveManualTask: %v", err)
	}
	if tr.Status != "pending" {
		t.Fatalf("tr.Status = %q, want pending", tr.Status)
	}
	if len(remoteJobs.CreateRemoteJobCalls) != 1 {
		t.Fatalf("expected one dispatched job, got %d", len(remoteJobs.CreateRemoteJobCalls))
	}
	if got := remoteJobs.CreateRemoteJobCalls[0].Payload.SecretValues; len(got) != 1 || got[0] != "S3cr3t-Remote-Value" {
		t.Fatalf("approved manual job secret values = %q, want the run's resolved secret", got)
	}
	if len(pipeline.TaskRunCalls) != 0 {
		t.Fatalf("expected no in-process execution, got %d calls", len(pipeline.TaskRunCalls))
	}
}

// ── ActiveRunIDsForProject remote-job union (tasks.md 2.4) ─────────

func TestRunService_ActiveRunIDsForProject_UnionsRemoteJobs(t *testing.T) {
	svc := NewRunService(&mocks.MockRunStore{}, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&mocks.MockRemoteJobStore{OutstandingJobRunIDsResult: []int{11, 12}})
	ids, err := svc.ActiveRunIDsForProject("proj")
	if err != nil {
		t.Fatalf("ActiveRunIDsForProject: %v", err)
	}
	if len(ids) != 2 || ids[0] != 11 || ids[1] != 12 {
		t.Fatalf("ids = %v, want [11 12]", ids)
	}
}

func TestRunService_ActiveRunIDsForProject_RemoteJobStoreErrorFailsClosed(t *testing.T) {
	svc := NewRunService(&mocks.MockRunStore{}, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&mocks.MockRemoteJobStore{OutstandingJobRunIDsErr: context.DeadlineExceeded})
	ids, err := svc.ActiveRunIDsForProject("proj")
	if err == nil {
		t.Fatal("expected the remote job store's error to propagate (fail closed)")
	}
	if ids != nil {
		t.Fatalf("expected nil ids on error, got %v", ids)
	}
}

func TestRunService_ActiveRunIDsForProject_NoDuplicateWhenBothSourcesAgree(t *testing.T) {
	proj := &model.Project{Name: "proj", Tasks: []model.Task{{Name: "t"}}}
	runStore := &mocks.MockRunStore{CreateRunID: 20}
	remoteJobs := &mocks.MockRemoteJobStore{OutstandingJobRunIDsResult: []int{20}}
	pipeline := &blockingPipelineRunner{startedCh: make(chan struct{}), releaseCh: make(chan struct{})}
	svc := NewRunService(runStore, pipeline, pipeline)
	svc.SetRemoteJobStore(remoteJobs)
	if _, err := svc.Start(context.Background(), "proj", proj, nil, domain.RunnerOptions{}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-pipeline.startedCh
	defer close(pipeline.releaseCh)

	ids, err := svc.ActiveRunIDsForProject("proj")
	if err != nil {
		t.Fatalf("ActiveRunIDsForProject: %v", err)
	}
	if len(ids) != 1 || ids[0] != 20 {
		t.Fatalf("ids = %v, want [20] (deduped, not [20 20])", ids)
	}
}

func TestRunService_Start_RemoteMode_RegistersNoActiveEntry(t *testing.T) {
	proj := &model.Project{Name: "proj", Tasks: []model.Task{}}
	runStore := &mocks.MockRunStore{CreateRunID: 1}
	remoteJobs := &mocks.MockRemoteJobStore{}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(remoteJobs)

	runID, err := svc.Start(context.Background(), "proj", proj, nil, domain.RunnerOptions{Mode: "remote", WorkDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if ids := activeRunIDs(t, svc, "proj"); len(ids) != 0 {
		t.Fatalf("expected no active entry for a remote-mode run, got %v", ids)
	}
	_ = runID
}

func TestRunService_Start_RemoteMode_InvalidTriggerExpressionErrors(t *testing.T) {
	proj := &model.Project{Name: "proj", Tasks: []model.Task{
		{Name: "a", Trigger: "repo.(unclosed", Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
	}}
	svc := NewRunService(&mocks.MockRunStore{}, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&mocks.MockRemoteJobStore{})
	if _, err := svc.Start(context.Background(), "proj", proj, nil, domain.RunnerOptions{Mode: "remote"}); err == nil {
		t.Fatal("expected an error for an unparseable trigger expression")
	}
}

func TestRunService_Start_RemoteMode_CreateRunErrorPropagates(t *testing.T) {
	proj := &model.Project{Name: "proj", Tasks: []model.Task{}}
	svc := NewRunService(&mocks.MockRunStore{CreateRunErr: context.DeadlineExceeded}, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&mocks.MockRemoteJobStore{})
	if _, err := svc.Start(context.Background(), "proj", proj, nil, domain.RunnerOptions{Mode: "remote"}); err == nil {
		t.Fatal("expected CreateRun's error to propagate")
	}
}

func TestRunService_Start_RemoteMode_ArchiveErrorPropagatesButKeepsRunID(t *testing.T) {
	proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
		{Name: "a", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
	}}
	svc := NewRunService(&mocks.MockRunStore{CreateRunID: 7}, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&mocks.MockRemoteJobStore{})
	runID, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: filepath.Join(t.TempDir(), "does-not-exist"),
	})
	if err == nil {
		t.Fatal("expected an error when the source archive cannot be built")
	}
	if runID != 7 {
		t.Fatalf("runID = %d, want 7 (the run row was already created)", runID)
	}
}

func TestRunService_Start_RemoteMode_CreateRemoteJobErrorPropagates(t *testing.T) {
	proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
		{Name: "a", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
	}}
	svc := NewRunService(&mocks.MockRunStore{CreateRunID: 1}, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&mocks.MockRemoteJobStore{CreateRemoteJobErr: context.DeadlineExceeded})
	if _, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(),
	}); err == nil {
		t.Fatal("expected CreateRemoteJob's error to propagate")
	}
}

func TestRunService_Start_RemoteMode_NoRemoteJobStoreFallsThroughToInProcess(t *testing.T) {
	proj := &model.Project{Name: "proj", Tasks: []model.Task{}}
	finishCh := make(chan struct{})
	runStore := &mocks.MockRunStore{CreateRunID: 1, FinishRunCh: finishCh}
	pipeline := &mocks.MockPipelineRunner{}
	svc := NewRunService(runStore, pipeline, pipeline)
	// No SetRemoteJobStore call: Mode="remote" must not panic or silently
	// no-op - it falls through to the ordinary in-process path, which will
	// itself execute (and, since PipelineExecutor has no idea what "remote"
	// means, that path belongs to the executor to reject - this test only
	// pins that RunService itself doesn't special-case Mode without a
	// configured remoteJobStore).
	runID, err := svc.Start(context.Background(), "proj", proj, nil, domain.RunnerOptions{Mode: "remote"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if runID != 1 {
		t.Fatalf("runID = %d, want 1", runID)
	}
	select {
	case <-finishCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for the in-process run to finish")
	}
	if len(pipeline.RunCalls) != 1 {
		t.Fatalf("expected the in-process pipeline to be invoked when no remoteJobStore is configured, got %d calls", len(pipeline.RunCalls))
	}
}

func TestEligibleForDispatch_DiamondBlockedBranchSkipsInOnePass(t *testing.T) {
	nodes := []dag.Node{
		{Task: model.Task{Name: "root"}},
		{Task: model.Task{Name: "left", DependsOn: []string{"root"}}},
		{Task: model.Task{Name: "right", DependsOn: []string{"root"}}},
		{Task: model.Task{Name: "join", DependsOn: []string{"left", "right"}}},
	}
	// Only root has been decided (failed); left/right/join have no entry at
	// all yet - "not seeded" is what "not yet dispatched" means under the
	// current semantics, since a seeded "pending" entry now unambiguously
	// means "already dispatched, awaiting its runner".
	latest := map[string]domain.TaskResult{
		"root": {Name: "root", Status: "failed"},
	}
	eligible, skipped := eligibleForDispatch(nodes, latest)
	if len(eligible) != 0 {
		t.Fatalf("expected nothing eligible, got %+v", eligible)
	}
	names := map[string]bool{}
	for _, s := range skipped {
		names[s.Name] = true
		if s.Status != "skipped" || s.Error != "dependency failed" {
			t.Fatalf("unexpected skip result: %+v", s)
		}
	}
	if !names["left"] || !names["right"] || !names["join"] {
		t.Fatalf("expected left, right, and join all skipped in one pass, got %v", skipped)
	}
}

// A task with a failed dependency and an undecided one waits: the
// undecided dependency wins, whatever the order of DependsOn. Before the
// change the loop broke on the first dependency it saw, so the outcome
// depended on that order - a task that listed the failed dependency first
// was skipped at once, while the same task listed the other way round
// waited. The undecided dependency is either a task still in flight
// ("pending") or one that is never decided at all - a manual gate, which no
// walker ever records.
func TestEligibleForDispatch_AnUndecidedDependencyBeatsAFailedOne(t *testing.T) {
	cases := []struct {
		name      string
		dependsOn []string
		latest    map[string]domain.TaskResult
	}{
		{
			"failed first, gate second",
			[]string{"failed", "gate"},
			map[string]domain.TaskResult{"failed": {Name: "failed", Status: "failed"}},
		},
		{
			"gate first, failed second",
			[]string{"gate", "failed"},
			map[string]domain.TaskResult{"failed": {Name: "failed", Status: "failed"}},
		},
		{
			"failed first, pending second",
			[]string{"failed", "waiting"},
			map[string]domain.TaskResult{
				"failed":  {Name: "failed", Status: "failed"},
				"waiting": {Name: "waiting", Status: "pending"},
			},
		},
		{
			"pending first, failed second",
			[]string{"waiting", "failed"},
			map[string]domain.TaskResult{
				"failed":  {Name: "failed", Status: "failed"},
				"waiting": {Name: "waiting", Status: "pending"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nodes := []dag.Node{{Task: model.Task{Name: "task", DependsOn: tc.dependsOn}}}
			eligible, skipped := eligibleForDispatch(nodes, tc.latest)
			if len(eligible) != 0 || len(skipped) != 0 {
				t.Fatalf("eligible=%v skipped=%v, want the task to wait for its undecided dependency", eligible, skipped)
			}
			if _, seeded := tc.latest["task"]; seeded {
				t.Fatal("the task was seeded, want it left undecided")
			}
		})
	}
}

func TestEligibleForDispatch_ManualTaskNeverAutoDispatched(t *testing.T) {
	nodes := []dag.Node{{Task: model.Task{Name: "gate", Manual: true}}}
	latest := map[string]domain.TaskResult{"gate": {Name: "gate", Status: "pending"}}
	eligible, skipped := eligibleForDispatch(nodes, latest)
	if len(eligible) != 0 || len(skipped) != 0 {
		t.Fatalf("expected a manual task to be left alone, got eligible=%v skipped=%v", eligible, skipped)
	}
}

// remote-runner-fixes D1: every job a remote run dispatches - initial and
// resumed - carries the run's resolved secret values, so the runner and the
// server can redact them.
func TestRunService_RemoteMode_SecretValuesReachEveryJob(t *testing.T) {
	proj := &model.Project{
		Name:  "proj",
		Repos: []domain.Repo{{Name: "app"}},
		Tasks: []model.Task{
			{Name: "a", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "echo a"}},
			{Name: "b", Trigger: "repo.app.changed", DependsOn: []string{"a"}, Execute: domain.ExecuteSpec{Repo: "app", Script: "echo b"}},
		},
	}
	secrets := []string{"S3cr3t-Remote-Value"}
	runStore := &mocks.MockRunStore{CreateRunID: 42}
	remoteJobs := &mocks.MockRemoteJobStore{}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(remoteJobs)

	if _, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(), Variables: variables.New(), ResolvedSecretValues: secrets,
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := remoteJobs.CreateRemoteJobCalls[0].Payload.SecretValues; len(got) != 1 || got[0] != secrets[0] {
		t.Fatalf("initial job secret values = %q, want %q", got, secrets)
	}
	if _, _, got, _ := remoteJobs.GetRemoteRunSnapshot(42); len(got) != 1 || got[0] != secrets[0] {
		t.Fatalf("snapshot secret values = %q, want %q", got, secrets)
	}

	runStore.GetRunResult = &domain.Run{ID: 42, Changed: []string{"app"}, PipelineDef: proj,
		TaskResults: []domain.TaskResult{{Name: "a", Status: "passed"}}}
	if _, err := svc.resumeRemoteRun(42); err != nil {
		t.Fatalf("resumeRemoteRun: %v", err)
	}
	last := remoteJobs.CreateRemoteJobCalls[len(remoteJobs.CreateRemoteJobCalls)-1]
	if last.TaskName != "b" || len(last.Payload.SecretValues) != 1 || last.Payload.SecretValues[0] != secrets[0] {
		t.Fatalf("resumed job = %s with secret values %q, want b with %q", last.TaskName, last.Payload.SecretValues, secrets)
	}
}
