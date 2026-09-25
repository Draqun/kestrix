package store

import (
	"testing"

	"kestrix/internal/domain"
)

// TestPG_RemoteRunSnapshot_RoundTrips verifies SetRemoteRunSnapshot and
// GetRemoteRunSnapshot round-trip the archive and per-task variables a
// remote-mode run's initial dispatch computes - tasks.md 3.6's resumption
// walker relies on this. A run with none set returns a nil archive and nil
// map.
func TestPG_RemoteRunSnapshot_RoundTrips(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-snapshot", "")
	runID := mustCreateRun(t, pg, "proj-snapshot", nil)

	archive, vars, err := pg.GetRemoteRunSnapshot(runID)
	if err != nil {
		t.Fatal(err)
	}
	if archive != nil || vars != nil {
		t.Fatalf("expected nil archive/vars before any snapshot is set, got %v %v", archive, vars)
	}

	wantArchive := []byte("archive-bytes")
	wantVars := map[string]map[string]string{"build": {"KEY": "value"}}
	if err := pg.SetRemoteRunSnapshot(runID, wantArchive, wantVars); err != nil {
		t.Fatalf("SetRemoteRunSnapshot: %v", err)
	}

	gotArchive, gotVars, err := pg.GetRemoteRunSnapshot(runID)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotArchive) != string(wantArchive) {
		t.Fatalf("archive = %q, want %q", gotArchive, wantArchive)
	}
	if gotVars["build"]["KEY"] != "value" {
		t.Fatalf("vars = %v, want build.KEY=value", gotVars)
	}
}

func TestPG_RemoteRunSnapshot_UnknownRun(t *testing.T) {
	pg := testPG(t)
	archive, vars, err := pg.GetRemoteRunSnapshot(999999)
	if err != nil {
		t.Fatal(err)
	}
	if archive != nil || vars != nil {
		t.Fatalf("expected nil archive/vars for an unknown run, got %v %v", archive, vars)
	}
}

// TestPG_ModeDefaultsToProjectConfigMode verifies Upsert stores an explicit
// Mode and Get round-trips it - remote-runner-registration tasks.md 2.1.
func TestPG_ModeDefaultsToProjectConfigMode(t *testing.T) {
	pg := testPG(t)
	if err := pg.Upsert(&domain.ProjectConfig{Name: "proj-remote", RepoURL: "https://git.example/r.git", RepoRef: "main", Mode: "remote"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := pg.Get("proj-remote")
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != "remote" {
		t.Fatalf("Mode = %q, want remote", got.Mode)
	}
}

// TestPG_ModeDefaultsWhenUnset verifies an Upsert that leaves Mode empty
// picks up this store's instance-wide default (projectDefaults), the same
// default new rows get - tasks.md 2.1.
func TestPG_ModeDefaultsWhenUnset(t *testing.T) {
	pg := testPG(t)
	if err := pg.Upsert(&domain.ProjectConfig{Name: "proj-default-mode", RepoURL: "https://git.example/d.git", RepoRef: "main"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := pg.Get("proj-default-mode")
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != pg.defaultMode {
		t.Fatalf("Mode = %q, want instance default %q", got.Mode, pg.defaultMode)
	}
}

// TestMigrateBackfillsExistingProjectMode is the migration-default coverage
// tasks.md 2.1 requires: a project row that predates the mode column (or
// was otherwise left with mode=”) is backfilled to the instance's current
// config.RunnerConfig.Mode on the next migrate call, so behavior after the
// migration is identical to before it - design.md Migration Plan. A row
// that already has an explicit Mode is left untouched.
func TestMigrateBackfillsExistingProjectMode(t *testing.T) {
	pg := testPG(t)
	if err := pg.Upsert(&domain.ProjectConfig{Name: "proj-legacy", RepoURL: "https://git.example/legacy.git", RepoRef: "main"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := pg.Upsert(&domain.ProjectConfig{Name: "proj-explicit-docker", RepoURL: "https://git.example/explicit.git", RepoRef: "main", Mode: "docker"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	// Simulate a row that predates the mode column entirely: blank it out
	// directly, bypassing projectDefaults' own fallback.
	if _, err := pg.db.Exec(`UPDATE projects SET mode = '' WHERE name = 'proj-legacy'`); err != nil {
		t.Fatal(err)
	}

	if err := migrate(pg.db, "gitlab-runner"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	legacy, err := pg.Get("proj-legacy")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Mode != "gitlab-runner" {
		t.Fatalf("backfilled legacy project Mode = %q, want gitlab-runner", legacy.Mode)
	}
	explicit, err := pg.Get("proj-explicit-docker")
	if err != nil {
		t.Fatal(err)
	}
	if explicit.Mode != "docker" {
		t.Fatalf("explicit project Mode = %q, want unchanged docker", explicit.Mode)
	}
}
