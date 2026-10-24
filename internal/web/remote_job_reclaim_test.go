package web

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/service"
)

// ── Server.remoteJobTimeout / remoteJobMaxReclaims defaults ─────────

func TestServer_RemoteJobTimeoutDefaults(t *testing.T) {
	srv := &Server{cfg: Config{ListenAddr: ":0"}}
	if got := srv.remoteJobTimeout(); got != defaultRemoteJobTimeout {
		t.Fatalf("remoteJobTimeout() = %v, want default %v", got, defaultRemoteJobTimeout)
	}
	if got := srv.remoteJobMaxReclaims(); got != defaultRemoteJobMaxReclaims {
		t.Fatalf("remoteJobMaxReclaims() = %d, want default %d", got, defaultRemoteJobMaxReclaims)
	}

	srv.cfg.Runner.RemoteJobTimeout = 5 * time.Minute
	srv.cfg.Runner.RemoteJobMaxReclaims = 9
	if got := srv.remoteJobTimeout(); got != 5*time.Minute {
		t.Fatalf("remoteJobTimeout() = %v, want 5m", got)
	}
	if got := srv.remoteJobMaxReclaims(); got != 9 {
		t.Fatalf("remoteJobMaxReclaims() = %d, want 9", got)
	}
}

// ── StartRemoteJobReclaimLoop (tasks.md 4.2) ────────────────────────

func TestStartRemoteJobReclaimLoop_CallsReclaimOnEachTick(t *testing.T) {
	old := remoteJobReclaimInterval
	remoteJobReclaimInterval = 5 * time.Millisecond
	defer func() { remoteJobReclaimInterval = old }()

	remoteJobs := &mocks.MockRemoteJobStore{ReclaimedCount: 1}
	runStore := &mocks.MockRunStore{}
	runService := service.NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	runService.SetRemoteJobStore(remoteJobs)
	srv := &Server{cfg: Config{ListenAddr: ":0"}, runService: runService}

	ctx, cancel := context.WithCancel(context.Background())
	srv.StartRemoteJobReclaimLoop(ctx)
	defer cancel()

	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		if remoteJobs.ReclaimCallCount() > 0 && remoteJobs.DeleteSecretsCallCount() > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for the reclaim loop to tick")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestStartRemoteJobReclaimLoop_NilRunServiceDoesNotPanic(t *testing.T) {
	old := remoteJobReclaimInterval
	remoteJobReclaimInterval = 5 * time.Millisecond
	defer func() { remoteJobReclaimInterval = old }()

	srv := &Server{cfg: Config{ListenAddr: ":0"}}
	ctx, cancel := context.WithCancel(context.Background())
	srv.StartRemoteJobReclaimLoop(ctx)
	time.Sleep(20 * time.Millisecond)
	cancel()
}

func TestStartRemoteJobReclaimLoop_ErrorDoesNotStopTheLoop(t *testing.T) {
	old := remoteJobReclaimInterval
	remoteJobReclaimInterval = 5 * time.Millisecond
	defer func() { remoteJobReclaimInterval = old }()

	remoteJobs := &mocks.MockRemoteJobStore{ReclaimErr: errGap}
	runStore := &mocks.MockRunStore{}
	runService := service.NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	runService.SetRemoteJobStore(remoteJobs)
	srv := &Server{cfg: Config{ListenAddr: ":0"}, runService: runService}

	ctx, cancel := context.WithCancel(context.Background())
	srv.StartRemoteJobReclaimLoop(ctx)
	time.Sleep(30 * time.Millisecond)
	cancel()
	if remoteJobs.ReclaimCallCount() < 2 {
		t.Fatalf("expected multiple reclaim attempts despite errors, got %d", remoteJobs.ReclaimCallCount())
	}
}

func TestStartRemoteJobReclaimLoop_StopsOnContextCancel(t *testing.T) {
	old := remoteJobReclaimInterval
	remoteJobReclaimInterval = 5 * time.Millisecond
	defer func() { remoteJobReclaimInterval = old }()

	remoteJobs := &mocks.MockRemoteJobStore{}
	runStore := &mocks.MockRunStore{}
	runService := service.NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	runService.SetRemoteJobStore(remoteJobs)
	srv := &Server{cfg: Config{ListenAddr: ":0"}, runService: runService}

	ctx, cancel := context.WithCancel(context.Background())
	srv.StartRemoteJobReclaimLoop(ctx)
	time.Sleep(15 * time.Millisecond)
	cancel()
	time.Sleep(10 * time.Millisecond)
	countAfterCancel := remoteJobs.ReclaimCallCount()
	time.Sleep(30 * time.Millisecond)
	if remoteJobs.ReclaimCallCount() != countAfterCancel {
		t.Fatal("expected no further reclaim attempts after the context was canceled")
	}
}

// remote-runner-fixes D2: a failing secret sweep is logged, never blocks
// silent-runner recovery.
func TestStartRemoteJobReclaimLoop_SecretSweepErrorStillReclaims(t *testing.T) {
	old := remoteJobReclaimInterval
	remoteJobReclaimInterval = 5 * time.Millisecond
	defer func() { remoteJobReclaimInterval = old }()

	remoteJobs := &mocks.MockRemoteJobStore{DeleteSecretsErr: errGap}
	runService := service.NewRunService(&mocks.MockRunStore{}, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	runService.SetRemoteJobStore(remoteJobs)
	srv := &Server{cfg: Config{ListenAddr: ":0"}, runService: runService}

	ctx, cancel := context.WithCancel(context.Background())
	srv.StartRemoteJobReclaimLoop(ctx)
	defer cancel()
	deadline := time.Now().Add(500 * time.Millisecond)
	for remoteJobs.ReclaimCallCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("reclaim never ran after a failing secret sweep")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestLogUnmatchedPendingJobs_LoggedOnceAndPruned(t *testing.T) {
	buf := captureSlog(t)
	remoteJobs := &mocks.MockRemoteJobStore{
		UnmatchedPendingJobsResult: []domain.UnmatchedPendingJob{
			{ID: 42, RunID: 10, TaskName: "build", RequiredTags: []string{"gpu"}},
		},
	}
	srv := &Server{remoteJobStore: remoteJobs}

	// Tick 1: first discovery -> logged once
	srv.logUnmatchedPendingJobs()
	out1 := buf.String()
	if !strings.Contains(out1, "remote job unmatched by any registered runner") ||
		!strings.Contains(out1, `"run_id":10`) ||
		!strings.Contains(out1, `"task":"build"`) {
		t.Fatalf("expected unmatched log line on tick 1, got:\n%s", out1)
	}

	// Tick 2: same job still unmatched -> NOT logged again
	buf.Reset()
	srv.logUnmatchedPendingJobs()
	if buf.Len() != 0 {
		t.Fatalf("expected no log on tick 2 for same job, got:\n%s", buf.String())
	}

	// Tick 3: job leaves the set -> pruned
	remoteJobs.UnmatchedPendingJobsResult = nil
	srv.logUnmatchedPendingJobs()
	if buf.Len() != 0 {
		t.Fatalf("expected no log on tick 3 when empty, got:\n%s", buf.String())
	}

	// Tick 4: job re-enters the set -> logged again
	remoteJobs.UnmatchedPendingJobsResult = []domain.UnmatchedPendingJob{
		{ID: 42, RunID: 10, TaskName: "build", RequiredTags: []string{"gpu"}},
	}
	srv.logUnmatchedPendingJobs()
	out4 := buf.String()
	if !strings.Contains(out4, "remote job unmatched by any registered runner") {
		t.Fatalf("expected unmatched log line on tick 4 after re-entering, got:\n%s", out4)
	}

	// Error handling
	buf.Reset()
	remoteJobs.UnmatchedPendingJobsErr = errors.New("query failed")
	srv.logUnmatchedPendingJobs()
	if !strings.Contains(buf.String(), "checking unmatched pending jobs failed") {
		t.Fatalf("expected error log when query fails, got:\n%s", buf.String())
	}

	// Nil store returns cleanly
	nilStoreSrv := &Server{}
	nilStoreSrv.logUnmatchedPendingJobs()
}
