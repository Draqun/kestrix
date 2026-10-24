package store

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/model"
	"kestrix/internal/runner"
	"kestrix/internal/service"
)

func TestPG_RemoteArtifacts_PutAndList(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-art-list", "")
	runID := mustCreateRun(t, pg, "proj-art-list", nil)

	dataBuild := []byte("tar-gz-build-artifact-bytes")
	dataTest := []byte("tar-gz-test-artifact-bytes")

	if err := pg.PutRemoteArtifact(runID, "build", dataBuild); err != nil {
		t.Fatalf("PutRemoteArtifact(build) failed: %v", err)
	}
	if err := pg.PutRemoteArtifact(runID, "test", dataTest); err != nil {
		t.Fatalf("PutRemoteArtifact(test) failed: %v", err)
	}

	// List empty tasks
	empty, err := pg.ListRemoteArtifactsByTasks(runID, nil)
	if err != nil {
		t.Fatalf("ListRemoteArtifactsByTasks(nil): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty map for nil tasks, got %v", empty)
	}

	// List single task
	gotBuild, err := pg.ListRemoteArtifactsByTasks(runID, []string{"build"})
	if err != nil {
		t.Fatalf("ListRemoteArtifactsByTasks(build): %v", err)
	}
	if len(gotBuild) != 1 || !bytes.Equal(gotBuild["build"], dataBuild) {
		t.Fatalf("got %v, want build artifact %q", gotBuild, string(dataBuild))
	}

	// List multiple tasks including one non-existent
	gotBoth, err := pg.ListRemoteArtifactsByTasks(runID, []string{"build", "test", "deploy"})
	if err != nil {
		t.Fatalf("ListRemoteArtifactsByTasks(build, test, deploy): %v", err)
	}
	if len(gotBoth) != 2 {
		t.Fatalf("expected 2 items, got %d: %v", len(gotBoth), gotBoth)
	}
	if !bytes.Equal(gotBoth["build"], dataBuild) || !bytes.Equal(gotBoth["test"], dataTest) {
		t.Fatalf("unexpected content: %v", gotBoth)
	}
}

func TestPG_RemoteArtifacts_ReplacementOnRetry(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-art-retry", "")
	runID := mustCreateRun(t, pg, "proj-art-retry", nil)

	initialData := []byte("initial-archive")
	retriedData := []byte("retried-archive-content")

	if err := pg.PutRemoteArtifact(runID, "build", initialData); err != nil {
		t.Fatalf("initial PutRemoteArtifact failed: %v", err)
	}
	// A repeated upload for the same claim/task replaces the stored archive
	if err := pg.PutRemoteArtifact(runID, "build", retriedData); err != nil {
		t.Fatalf("retried PutRemoteArtifact failed: %v", err)
	}

	got, err := pg.ListRemoteArtifactsByTasks(runID, []string{"build"})
	if err != nil {
		t.Fatalf("ListRemoteArtifactsByTasks: %v", err)
	}
	if len(got) != 1 || !bytes.Equal(got["build"], retriedData) {
		t.Fatalf("expected replaced content %q, got %q", string(retriedData), string(got["build"]))
	}
}

func TestPG_RemoteArtifacts_DeleteTerminalRuns(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-art-term", "")

	runTerminal := mustCreateRun(t, pg, "proj-art-term", nil)
	runRunning := mustCreateRun(t, pg, "proj-art-term", nil)

	if err := pg.PutRemoteArtifact(runTerminal, "build", []byte("terminal-artifact")); err != nil {
		t.Fatal(err)
	}
	if err := pg.PutRemoteArtifact(runRunning, "build", []byte("active-artifact")); err != nil {
		t.Fatal(err)
	}

	// Mark runTerminal as passed
	if err := pg.FinishRun(runTerminal, "passed", "", "", nil); err != nil {
		t.Fatalf("FinishRun failed: %v", err)
	}

	// Sweep terminal artifacts
	if err := pg.DeleteTerminalRemoteArtifacts(); err != nil {
		t.Fatalf("DeleteTerminalRemoteArtifacts failed: %v", err)
	}

	// Check that terminal run's artifact is deleted
	termGot, err := pg.ListRemoteArtifactsByTasks(runTerminal, []string{"build"})
	if err != nil {
		t.Fatal(err)
	}
	if len(termGot) != 0 {
		t.Fatalf("expected terminal run artifacts to be deleted, got %v", termGot)
	}

	// Check that running run's artifact is preserved
	activeGot, err := pg.ListRemoteArtifactsByTasks(runRunning, []string{"build"})
	if err != nil {
		t.Fatal(err)
	}
	if len(activeGot) != 1 || !bytes.Equal(activeGot["build"], []byte("active-artifact")) {
		t.Fatalf("expected active run artifact preserved, got %v", activeGot)
	}
}

func TestPG_RemoteArtifacts_CascadeOnRunPrune(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-art-cascade", "")

	runID := mustCreateRun(t, pg, "proj-art-cascade", nil)
	if err := pg.PutRemoteArtifact(runID, "build", []byte("cascade-test-artifact")); err != nil {
		t.Fatal(err)
	}

	// Delete run directly from database to test ON DELETE CASCADE foreign key
	res, err := pg.db.Exec(`DELETE FROM runs WHERE id = $1`, runID)
	if err != nil {
		t.Fatalf("delete run: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("expected 1 run deleted, got %d", n)
	}

	// Check that remote_artifacts row was deleted by ON DELETE CASCADE
	var count int
	if err := pg.db.QueryRow(`SELECT count(*) FROM remote_artifacts WHERE run_id = $1`, runID).Scan(&count); err != nil {
		t.Fatalf("query remote_artifacts count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 artifacts after cascade delete, got %d", count)
	}
}

func TestPG_RemoteArtifacts_TerminalSweepDeletesArtifacts(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-art-sweep", "")

	runTerminal := mustCreateRun(t, pg, "proj-art-sweep", nil)
	runRunning := mustCreateRun(t, pg, "proj-art-sweep", nil)

	if err := pg.PutRemoteArtifact(runTerminal, "build", []byte("terminal-artifact-data")); err != nil {
		t.Fatal(err)
	}
	if err := pg.PutRemoteArtifact(runRunning, "build", []byte("active-artifact-data")); err != nil {
		t.Fatal(err)
	}

	// Terminal status on runTerminal
	if err := pg.FinishRun(runTerminal, "passed", "", "", nil); err != nil {
		t.Fatalf("FinishRun failed: %v", err)
	}

	// The periodic sweep method: DeleteTerminalRemoteSecrets (D5)
	if err := pg.DeleteTerminalRemoteSecrets(); err != nil {
		t.Fatalf("DeleteTerminalRemoteSecrets failed: %v", err)
	}

	// Verify terminal run's artifact is deleted
	var termCount int
	if err := pg.db.QueryRow(`SELECT count(*) FROM remote_artifacts WHERE run_id = $1`, runTerminal).Scan(&termCount); err != nil {
		t.Fatal(err)
	}
	if termCount != 0 {
		t.Fatalf("expected terminal run artifacts to be 0, got %d", termCount)
	}

	// Verify running run's artifact is still present
	var runCount int
	if err := pg.db.QueryRow(`SELECT count(*) FROM remote_artifacts WHERE run_id = $1`, runRunning).Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 1 {
		t.Fatalf("expected running run artifacts to be 1, got %d", runCount)
	}
}

func createTarGzArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader(%s): %v", name, err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatalf("Write(%s): %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tw.Close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gw.Close: %v", err)
	}
	return buf.Bytes()
}

func readTarGzArchive(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	res := make(map[string][]byte)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tr.Next: %v", err)
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("io.ReadAll(%s): %v", hdr.Name, err)
		}
		res[hdr.Name] = b
	}
	return res
}

// 3.3 Integration test through CompleteRemoteJob against Postgres:
// build writes dist/app.txt and declares dist/; deploy depends on it and cats the file.
// Verified in remote mode with a fake runner.
func TestPG_RemoteArtifacts_RemoteMode_BuildDeployEndToEnd(t *testing.T) {
	pg := testPG(t)
	projName := "proj-art-remote-e2e"
	mustUpsert(t, pg, projName, "")

	specBuild := domain.ExecuteSpec{Repo: "app", Script: "mkdir -p dist && echo 'built-app-v1' > dist/app.txt"}
	specDeploy := domain.ExecuteSpec{Repo: "app", Script: "cat dist/app.txt"}
	proj := &model.Project{
		Name:  projName,
		Repos: []domain.Repo{{Name: "app"}},
		Tasks: []model.Task{
			{Name: "build", Trigger: "repo.app.changed", Artifacts: []string{"dist/"}, Execute: specBuild},
			{Name: "deploy", Trigger: "repo.app.changed", DependsOn: []string{"build"}, Execute: specDeploy},
		},
	}

	svc := service.NewRunService(pg, nil, nil)
	svc.SetRemoteJobStore(pg)

	runID, err := svc.Start(context.Background(), projName, proj, []string{"app"}, domain.RunnerOptions{
		Mode:    "remote",
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	runnerID := mustCreateRunner(t, pg, "art-runner-remote")

	// 1. Claim build job
	buildJob := claimNextJob(t, pg, runnerID, "build")

	// Simulate fake runner creating artifact archive for dist/app.txt and uploading to store
	artArchive := createTarGzArchive(t, map[string][]byte{
		"dist/app.txt": []byte("built-app-v1\n"),
	})
	if err := pg.PutRemoteArtifact(buildJob.RunID, "build", artArchive); err != nil {
		t.Fatalf("PutRemoteArtifact: %v", err)
	}

	// Complete build job
	if ok, err := svc.CompleteRemoteJob(buildJob.ID, buildJob.ClaimEpoch, "passed", "build output", "", &runnerID); err != nil || !ok {
		t.Fatalf("CompleteRemoteJob(build) = %v, %v", ok, err)
	}

	// 2. Claim deploy job
	deployJob := claimNextJob(t, pg, runnerID, "deploy")

	// Verify deploy payload has build's artifact archive
	if len(deployJob.Payload.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact in deploy job payload, got %d", len(deployJob.Payload.Artifacts))
	}
	extracted := readTarGzArchive(t, deployJob.Payload.Artifacts[0])
	if string(extracted["dist/app.txt"]) != "built-app-v1\n" {
		t.Fatalf("expected 'built-app-v1\n', got %q", string(extracted["dist/app.txt"]))
	}

	// Complete deploy job
	if ok, err := svc.CompleteRemoteJob(deployJob.ID, deployJob.ClaimEpoch, "passed", "deploy output", "", &runnerID); err != nil || !ok {
		t.Fatalf("CompleteRemoteJob(deploy) = %v, %v", ok, err)
	}

	// Verify run passed
	run, err := pg.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status != "passed" {
		t.Fatalf("run status = %q, want passed", run.Status)
	}
}

// 3.3 Integration test in local mode:
// build writes dist/app.txt and declares dist/; deploy depends on it and cats the file.
// The run passes end-to-end locally.
func TestPG_RemoteArtifacts_LocalMode_BuildDeployEndToEnd(t *testing.T) {
	pg := testPG(t)
	projName := "proj-art-local-e2e"
	mustUpsert(t, pg, projName, "")

	exec := &runner.PipelineExecutor{Mode: "local"}
	svc := service.NewRunService(pg, exec, exec)
	svc.SetLocalExecutionPolicy(true, "local")

	workDir := t.TempDir()
	appDir := filepath.Join(workDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatal(err)
	}

	specBuild := domain.ExecuteSpec{Repo: "app", Script: "mkdir -p dist && echo 'built-local-app-v1' > dist/app.txt"}
	specDeploy := domain.ExecuteSpec{Repo: "app", Script: "cat dist/app.txt"}
	proj := &model.Project{
		Name:  projName,
		Repos: []domain.Repo{{Name: "app", Path: "app"}},
		Tasks: []model.Task{
			{Name: "build", Trigger: "repo.app.changed", Artifacts: []string{"dist/"}, Execute: specBuild},
			{Name: "deploy", Trigger: "repo.app.changed", DependsOn: []string{"build"}, Execute: specDeploy},
		},
	}

	runID, err := svc.Start(context.Background(), projName, proj, []string{"app"}, domain.RunnerOptions{
		Mode:    "local",
		WorkDir: workDir,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Poll until terminal status
	deadline := time.Now().Add(5 * time.Second)
	var finalRun *Run
	for time.Now().Before(deadline) {
		r, err := pg.GetRun(runID)
		if err == nil && r != nil && domain.IsTerminalRunStatus(r.Status) {
			finalRun = r
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalRun == nil {
		r, _ := pg.GetRun(runID)
		t.Fatalf("run %d did not reach terminal status; current status: %v", runID, r.Status)
	}
	if finalRun.Status != "passed" {
		t.Fatalf("run status = %q, want passed (error: %s, output: %s, tasks: %+v)", finalRun.Status, finalRun.Error, finalRun.Output, finalRun.TaskResults)
	}

	if len(finalRun.TaskResults) == 0 {
		t.Fatalf("expected task results in run")
	}
	deployFound := false
	for _, task := range finalRun.TaskResults {
		if task.Status != "passed" {
			t.Errorf("task %s status = %q, want passed", task.Name, task.Status)
		}
		if task.Name == "deploy" {
			deployFound = true
			if !strings.Contains(task.Output, "built-local-app-v1") {
				t.Errorf("deploy task output = %q, want 'built-local-app-v1'", task.Output)
			}
		}
	}
	if !deployFound {
		t.Errorf("deploy task not found in results: %+v", finalRun.TaskResults)
	}
}
