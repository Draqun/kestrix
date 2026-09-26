package web

import (
	"context"
	"testing"
	"time"

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
