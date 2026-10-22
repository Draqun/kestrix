package store

import (
	"errors"
	"testing"
	"time"

	"kestrix/internal/domain"
)

// A run that is already finished keeps its status: a late result is
// recorded, but it does not reopen the run and it does not overwrite a
// cancel that won the race - the same rule FinishRunIfDone follows
// (remote-run-waits-for-dependents design.md D2).
func TestPG_ApplyTaskResult_AFinishedRunKeepsItsStatus(t *testing.T) {
	for _, tc := range []struct {
		name     string
		terminal string
		late     domain.TaskResult
	}{
		{"canceled", "canceled", domain.TaskResult{Name: "build", Status: "failed", Error: "exit 1"}},
		{"failed", "failed", domain.TaskResult{Name: "deploy", Status: "passed"}},
		{"passed", "passed", domain.TaskResult{Name: "deploy", Status: "failed", Error: "exit 1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pg := testPG(t)
			mustUpsert(t, pg, "proj-"+tc.name, "")
			id := mustCreateRun(t, pg, "proj-"+tc.name, nil)
			if _, err := pg.db.Exec(`UPDATE runs SET status = $1, error = 'the original error', finished_at = now() WHERE id = $2`, tc.terminal, id); err != nil {
				t.Fatal(err)
			}

			status, err := pg.ApplyTaskResult(id, tc.late, "late output\n")
			if err != nil {
				t.Fatalf("ApplyTaskResult: %v", err)
			}
			if status != tc.terminal {
				t.Fatalf("returned status = %q, want %q", status, tc.terminal)
			}

			run, err := pg.GetRun(id)
			if err != nil || run == nil {
				t.Fatalf("GetRun = %+v, %v", run, err)
			}
			if run.Status != tc.terminal {
				t.Fatalf("run status = %q, want %q", run.Status, tc.terminal)
			}
			if run.Error != "the original error" {
				t.Fatalf("run error = %q, want the original error", run.Error)
			}
			if run.FinishedAt == nil {
				t.Fatal("run finished_at is nil, want it kept")
			}
			// The late result is still recorded.
			recorded := false
			for _, tr := range run.TaskResults {
				if tr.Name == tc.late.Name && tr.Status == tc.late.Status {
					recorded = true
				}
			}
			if !recorded {
				t.Fatalf("the late result was not recorded: %+v", run.TaskResults)
			}
		})
	}
}

// A cancel is the one status a finished run still takes: it is a
// deliberate act, and the derivation ranks it above a failure.
func TestPG_ApplyTaskResult_ACancelStillCancelsAFailedRun(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-cancel-wins", "")
	id := mustCreateRun(t, pg, "proj-cancel-wins", nil)
	if _, err := pg.ApplyTaskResult(id, domain.TaskResult{Name: "build", Status: "failed", Error: "exit 1"}, ""); err != nil {
		t.Fatalf("ApplyTaskResult(failed): %v", err)
	}

	status, err := pg.ApplyTaskResult(id, domain.TaskResult{Name: "deploy", Status: "canceled", Error: "canceled"}, "")
	if err != nil {
		t.Fatalf("ApplyTaskResult(canceled): %v", err)
	}
	if status != "canceled" {
		t.Fatalf("returned status = %q, want canceled", status)
	}
	run, err := pg.GetRun(id)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	if run.Status != "canceled" {
		t.Fatalf("run status = %q, want canceled", run.Status)
	}
}

// ApplyTaskResult reads the run's results under the run's row lock, so a
// result committed by another writer while it waits is not lost: the run's
// results are the only source of its final status.
func TestPG_ApplyTaskResult_ReadsUnderTheRunRowLock(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-lock", "")
	id := mustCreateRun(t, pg, "proj-lock", nil)

	// A writer that holds the run's row lock and adds a result of its own.
	tx, err := pg.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(
		`UPDATE runs SET task_results = task_results::jsonb || '[{"name":"other","status":"passed"}]'::jsonb WHERE id = $1`,
		id,
	); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := pg.ApplyTaskResult(id, domain.TaskResult{Name: "build", Status: "passed"}, "")
		done <- err
	}()

	// The result waits for the lock, and reads what the other writer
	// committed.
	time.Sleep(200 * time.Millisecond)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("ApplyTaskResult: %v", err)
	}

	run, err := pg.GetRun(id)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	recorded := map[string]bool{}
	for _, tr := range run.TaskResults {
		recorded[tr.Name] = true
	}
	if !recorded["other"] || !recorded["build"] {
		t.Fatalf("a concurrently committed result was lost: %+v", run.TaskResults)
	}
}

// A task that already has an active job is not dispatched a second time:
// the store says so with domain.ErrRemoteJobExists, so the caller can tell
// "already dispatched" from "the job could not be created".
func TestPG_CreateRemoteJob_ASecondDispatchIsRejected(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-dup", "")
	runID := mustCreateRun(t, pg, "proj-dup", nil)
	mustCreateRemoteJob(t, pg, runID, "build")

	_, err := pg.CreateRemoteJob(runID, "build", domain.RemoteJobPayload{
		Task: domain.Task{Name: "build", Execute: domain.ExecuteSpec{Repo: "checkout", Script: "echo hi"}},
	})
	if !errors.Is(err, domain.ErrRemoteJobExists) {
		t.Fatalf("CreateRemoteJob = %v, want domain.ErrRemoteJobExists", err)
	}
}
