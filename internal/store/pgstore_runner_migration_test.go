package store

import "testing"

// TestMigrateRunnerSchemaIsIdempotent verifies the runners/remote_jobs tables
// and their indexes are created, and that re-running migrate against the
// resulting database changes nothing and fails nothing - see
// remote-runner-registration tasks.md 1.1.
func TestMigrateRunnerSchemaIsIdempotent(t *testing.T) {
	pg := testPG(t)
	for i := 0; i < 2; i++ {
		if err := migrate(pg.db, "local"); err != nil {
			t.Fatalf("migrate #%d: %v", i+2, err)
		}
	}

	var n int
	if err := pg.db.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_name = 'runners'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("runners table count = %d, err = %v", n, err)
	}
	if err := pg.db.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_name = 'remote_jobs'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("remote_jobs table count = %d, err = %v", n, err)
	}
	if err := pg.db.QueryRow(`SELECT count(*) FROM pg_indexes WHERE indexname = 'idx_remote_jobs_status'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("idx_remote_jobs_status count = %d, err = %v", n, err)
	}
	if err := pg.db.QueryRow(`SELECT count(*) FROM pg_indexes WHERE indexname = 'idx_remote_jobs_run'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("idx_remote_jobs_run count = %d, err = %v", n, err)
	}
}

// TestRemoteJobs_CascadeDeletedWithRun verifies remote_jobs.run_id cascades:
// deleting a run's project (which cascades to its runs) must not leave an
// orphaned job row behind.
func TestRemoteJobs_CascadeDeletedWithRun(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-cascade", "")
	runID := mustCreateRun(t, pg, "proj-cascade", nil)
	if _, err := pg.db.Exec(`INSERT INTO remote_jobs (run_id, task_name, payload) VALUES ($1, 'build', '{}')`, runID); err != nil {
		t.Fatalf("insert remote_jobs: %v", err)
	}
	if _, err := pg.db.Exec(`DELETE FROM projects WHERE name = 'proj-cascade'`); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	var n int
	if err := pg.db.QueryRow(`SELECT count(*) FROM remote_jobs WHERE run_id = $1`, runID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("remote_jobs rows after cascade = %d, want 0", n)
	}
}
