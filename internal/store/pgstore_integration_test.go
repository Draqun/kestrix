// PGStore integration tests. These run against a real PostgreSQL instance.
// They auto-skip when the database is unreachable so `go test ./...` works
// in CI environments without PostgreSQL.
//
// Tests connect via KESTRIX_TEST_DATABASE_URL (NOT DATABASE_URL) to prevent
// accidental data loss on the development database. The test helper refuses
// to connect to any database whose name does not end with "_test".
package store

import (
	"net/url"
	"os"
	"strings"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/model"
)

func testPG(t *testing.T) *PGStore {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test")
	}
	dbURL := os.Getenv("KESTRIX_TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://kestrix:kestrix@localhost:5433/kestrix_test?sslmode=disable&connect_timeout=2"
	}

	// Safety guard: refuse to connect to a database that doesn't end with _test.
	// This prevents accidental data loss if someone points KESTRIX_TEST_DATABASE_URL
	// at the dev/production database.
	parsed, err := url.Parse(dbURL)
	if err == nil {
		dbName := strings.TrimPrefix(parsed.Path, "/")
		if !strings.HasSuffix(dbName, "_test") {
			t.Fatalf("tests must use a database ending with _test (got %q); set KESTRIX_TEST_DATABASE_URL", dbName)
		}
	}

	pg, err := NewPG(dbURL)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	t.Cleanup(func() {
		pg.db.Exec(`DELETE FROM runs`)
		pg.db.Exec(`DELETE FROM projects`)
		pg.db.Close()
	})
	return pg
}

// mustUpsert inserts a project and fails the test on error.
func mustUpsert(t *testing.T, pg *PGStore, name, repoPath string) {
	t.Helper()
	if err := pg.Upsert(&domain.ProjectConfig{Name: name, RepoPath: repoPath}); err != nil {
		t.Fatalf("Upsert(%q): %v", name, err)
	}
}

// mustCreateRun creates a run and fails the test on error.
func mustCreateRun(t *testing.T, pg *PGStore, projectName string, snap *model.Project) int {
	t.Helper()
	id, err := pg.CreateRun(projectName, nil, false, snap)
	if err != nil {
		t.Fatalf("CreateRun(%q): %v", projectName, err)
	}
	return id
}

// ── ProjectStore ───────────────────────────────────────────────────

func TestPG_UpsertAndGet(t *testing.T) {
	pg := testPG(t)
	cfg := &domain.ProjectConfig{
		Name:     "proj-a",
		RepoPath: "/src/a",
		RepoURL:  "https://gitlab.com/a.git",
	}
	if err := pg.Upsert(cfg); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := pg.Get("proj-a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil after Upsert")
	}
	if got.RepoPath != "/src/a" {
		t.Errorf("RepoPath = %q, want /src/a", got.RepoPath)
	}
	if got.CreatedAt == "" {
		t.Error("CreatedAt not set")
	}
	if got.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %q, want main", got.DefaultBranch)
	}
}

func TestPG_UpsertOverwrite(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "p", "/old")
	mustUpsert(t, pg, "p", "/new")
	got, err := pg.Get("p")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RepoPath != "/new" {
		t.Errorf("RepoPath = %q, want /new", got.RepoPath)
	}
}

func TestPG_GetNotFound(t *testing.T) {
	pg := testPG(t)
	got, err := pg.Get("nonexistent")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for not found")
	}
}

func TestPG_FindByGitLabID(t *testing.T) {
	pg := testPG(t)
	if err := pg.Upsert(&domain.ProjectConfig{Name: "gl", RepoPath: "/gl", GitLabProjectID: 999}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := pg.FindByGitLabID(999)
	if err != nil {
		t.Fatalf("FindByGitLabID: %v", err)
	}
	if got == nil {
		t.Fatal("expected project with GitLabProjectID 999")
	}
	if got.Name != "gl" {
		t.Errorf("Name = %q, want gl", got.Name)
	}
}

func TestPG_FindByGitLabIDNotFound(t *testing.T) {
	pg := testPG(t)
	got, err := pg.FindByGitLabID(888888)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for not found")
	}
}

func TestPG_Delete(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "del", "/d")
	if err := pg.Delete("del"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, _ := pg.Get("del")
	if got != nil {
		t.Error("project still exists after Delete")
	}
}

func TestPG_DeleteNotFound(t *testing.T) {
	pg := testPG(t)
	if err := pg.Delete("nope"); err == nil {
		t.Fatal("expected error for deleting nonexistent project")
	}
}

func TestPG_List(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "c", "/c")
	mustUpsert(t, pg, "a", "/a")
	mustUpsert(t, pg, "b", "/b")

	names, err := pg.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	if names[0] != "a" {
		t.Errorf("expected sorted, first = %q", names[0])
	}
}

func TestPG_Load(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "lp1", "/l1")
	mustUpsert(t, pg, "lp2", "/l2")

	loaded, err := pg.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(loaded))
	}
	if loaded["lp1"] == nil || loaded["lp2"] == nil {
		t.Error("missing project in Load result")
	}
}

func TestPG_Save(t *testing.T) {
	pg := testPG(t)
	projects := map[string]*domain.ProjectConfig{
		"sv1": {Name: "sv1", RepoPath: "/sv1", CreatedAt: "2025-01-01T00:00:00Z"},
		"sv2": {Name: "sv2", RepoPath: "/sv2", CreatedAt: "2025-01-01T00:00:00Z"},
	}
	if err := pg.Save(projects); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := pg.Get("sv1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Save did not persist sv1")
	}
	names, err := pg.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 2 {
		t.Errorf("expected 2 after Save, got %d", len(names))
	}
}

// ── RunStore ───────────────────────────────────────────────────────

func TestPG_CreateRunAndGetRun(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "rp", "/rp")
	snap := &model.Project{Name: "rp"}
	id := mustCreateRun(t, pg, "rp", snap)
	if id <= 0 {
		t.Fatalf("invalid run id %d", id)
	}

	run, err := pg.GetRun(id)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run == nil {
		t.Fatal("GetRun returned nil")
	}
	if run.ProjectName != "rp" {
		t.Errorf("ProjectName = %q, want rp", run.ProjectName)
	}
	if run.Status != "running" {
		t.Errorf("Status = %q, want running", run.Status)
	}
	if len(run.Changed) != 0 {
		t.Errorf("Changed = %+v, want empty", run.Changed)
	}
}

func TestPG_CreateRunWithChangedAndGetRun(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "rp2", "/rp2")
	snap := &model.Project{Name: "rp2"}
	id, err := pg.CreateRun("rp2", []string{"main"}, false, snap)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	run, err := pg.GetRun(id)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if len(run.Changed) != 1 || run.Changed[0] != "main" {
		t.Errorf("Changed = %+v, want [main]", run.Changed)
	}
	if run.PipelineDef == nil || run.PipelineDef.Name != "rp2" {
		t.Errorf("PipelineDef not set correctly: %+v", run.PipelineDef)
	}
}

func TestPG_GetRunNotFound(t *testing.T) {
	pg := testPG(t)
	got, err := pg.GetRun(999999)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for not found")
	}
}

func TestPG_FinishRun(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "fr", "/fr")
	id := mustCreateRun(t, pg, "fr", nil)

	results := []domain.TaskResult{
		{Name: "build", Status: "passed"},
		{Name: "test", Status: "passed"},
	}
	if err := pg.FinishRun(id, "passed", "all done", "", results); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	run, err := pg.GetRun(id)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status != "passed" {
		t.Errorf("Status = %q, want passed", run.Status)
	}
	if run.Output != "all done" {
		t.Errorf("Output = %q, want 'all done'", run.Output)
	}
	if len(run.TaskResults) != 2 {
		t.Errorf("TaskResults len = %d, want 2", len(run.TaskResults))
	}
	if run.FinishedAt == nil {
		t.Error("FinishedAt not set")
	}
}

func TestPG_AppendTaskResult(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "atr", "/atr")
	id := mustCreateRun(t, pg, "atr", nil)

	if err := pg.AppendTaskResult(id, domain.TaskResult{Name: "task1", Status: "running"}); err != nil {
		t.Fatalf("AppendTaskResult: %v", err)
	}
	if err := pg.AppendTaskResult(id, domain.TaskResult{Name: "task1", Status: "passed"}); err != nil {
		t.Fatalf("AppendTaskResult: %v", err)
	}
	if err := pg.AppendTaskResult(id, domain.TaskResult{Name: "task2", Status: "running"}); err != nil {
		t.Fatalf("AppendTaskResult: %v", err)
	}

	run, err := pg.GetRun(id)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if len(run.TaskResults) != 3 {
		t.Fatalf("expected 3 task results, got %d", len(run.TaskResults))
	}
	if run.TaskResults[0].Name != "task1" {
		t.Errorf("first result Name = %q", run.TaskResults[0].Name)
	}
}

func TestPG_FlushRunOutput(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "fro", "/fro")
	id := mustCreateRun(t, pg, "fro", nil)

	if err := pg.FlushRunOutput(id, "partial output 1\n"); err != nil {
		t.Fatalf("FlushRunOutput: %v", err)
	}
	if err := pg.FlushRunOutput(id, "partial output 2\n"); err != nil {
		t.Fatalf("FlushRunOutput: %v", err)
	}

	run, err := pg.GetRun(id)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Output != "partial output 2\n" {
		t.Errorf("Output = %q, want 'partial output 2\\n'", run.Output)
	}
}

func TestPG_ApplyTaskResult_Passed(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "ap", "/ap")
	id := mustCreateRun(t, pg, "ap", nil)

	if err := pg.ApplyTaskResult(id, domain.TaskResult{Name: "build", Status: "running"}, "building...\n"); err != nil {
		t.Fatalf("ApplyTaskResult(running): %v", err)
	}

	run1, err := pg.GetRun(id)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run1.Status != "running" {
		t.Errorf("Status after running = %q, want running", run1.Status)
	}

	if err := pg.ApplyTaskResult(id, domain.TaskResult{Name: "build", Status: "passed", Output: "ok"}, "done\n"); err != nil {
		t.Fatalf("ApplyTaskResult(passed): %v", err)
	}

	run2, err := pg.GetRun(id)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run2.Status != "passed" {
		t.Errorf("Status after passed = %q, want passed", run2.Status)
	}
	if run2.FinishedAt == nil {
		t.Error("FinishedAt not set for passed run")
	}
	if len(run2.TaskResults) != 2 {
		t.Fatalf("expected 2 results, got %d", len(run2.TaskResults))
	}
}

func TestPG_ApplyTaskResult_Failed(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "af", "/af")
	id := mustCreateRun(t, pg, "af", nil)

	if err := pg.ApplyTaskResult(id, domain.TaskResult{Name: "build", Status: "running"}, ""); err != nil {
		t.Fatalf("ApplyTaskResult(running): %v", err)
	}
	if err := pg.ApplyTaskResult(id, domain.TaskResult{Name: "build", Status: "failed", Error: "exit 1"}, ""); err != nil {
		t.Fatalf("ApplyTaskResult(failed): %v", err)
	}

	run, err := pg.GetRun(id)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status != "failed" {
		t.Errorf("Status = %q, want failed", run.Status)
	}
	if run.FinishedAt == nil {
		t.Error("FinishedAt not set for failed run")
	}
}

func TestPG_ApplyTaskResult_RunNotFound(t *testing.T) {
	pg := testPG(t)
	err := pg.ApplyTaskResult(999999, domain.TaskResult{Name: "x", Status: "passed"}, "")
	if err == nil {
		t.Fatal("expected error for non-existent run")
	}
}

func TestPG_CountRuns(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "cr", "/cr")
	mustCreateRun(t, pg, "cr", nil)
	mustCreateRun(t, pg, "cr", nil)

	count, err := pg.CountRuns("cr")
	if err != nil {
		t.Fatalf("CountRuns: %v", err)
	}
	if count != 2 {
		t.Errorf("CountRuns = %d, want 2", count)
	}
}

func TestPG_ListRuns(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "lr", "/lr")
	mustCreateRun(t, pg, "lr", nil)
	mustCreateRun(t, pg, "lr", nil)

	runs, err := pg.ListRuns("lr", 50, 0)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Errorf("expected 2 runs, got %d", len(runs))
	}
	if runs[0].ID <= runs[1].ID {
		t.Error("expected most recent first")
	}
}

func TestPG_ListRuns_Pagination(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "lp", "/lp")
	for i := 0; i < 5; i++ {
		mustCreateRun(t, pg, "lp", nil)
	}
	runs, err := pg.ListRuns("lp", 2, 0)
	if err != nil {
		t.Fatalf("ListRuns page 1: %v", err)
	}
	if len(runs) != 2 {
		t.Errorf("page 1: expected 2, got %d", len(runs))
	}
	runs2, err := pg.ListRuns("lp", 2, 2)
	if err != nil {
		t.Fatalf("ListRuns page 2: %v", err)
	}
	if len(runs2) != 2 {
		t.Errorf("page 2: expected 2, got %d", len(runs2))
	}
}

func TestPG_ListAllRuns(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "la1", "/")
	mustUpsert(t, pg, "la2", "/")
	mustCreateRun(t, pg, "la1", nil)
	mustCreateRun(t, pg, "la2", nil)

	runs, err := pg.ListAllRuns(10)
	if err != nil {
		t.Fatalf("ListAllRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Errorf("expected 2 runs, got %d", len(runs))
	}
}

func TestPG_CountRunsByStatus(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "cbs", "/")
	id1 := mustCreateRun(t, pg, "cbs", nil)
	id2 := mustCreateRun(t, pg, "cbs", nil)
	if err := pg.FinishRun(id1, "passed", "", "", nil); err != nil {
		t.Fatalf("FinishRun(passed): %v", err)
	}
	if err := pg.FinishRun(id2, "failed", "", "err", nil); err != nil {
		t.Fatalf("FinishRun(failed): %v", err)
	}

	result, err := pg.CountRunsByStatus()
	if err != nil {
		t.Fatalf("CountRunsByStatus: %v", err)
	}
	if result["passed"] != 1 {
		t.Errorf("passed count = %d, want 1", result["passed"])
	}
	if result["failed"] != 1 {
		t.Errorf("failed count = %d, want 1", result["failed"])
	}
}

func TestPG_CancelRun_Running(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "cancel", "/")
	snap := &model.Project{
		Name: "cancel",
		Tasks: []model.Task{
			{Name: "deploy", DependsOn: []string{"build"}},
		},
	}
	id := mustCreateRun(t, pg, "cancel", snap)
	if err := pg.AppendTaskResult(id, domain.TaskResult{Name: "deploy", Status: "running"}); err != nil {
		t.Fatalf("AppendTaskResult: %v", err)
	}
	if err := pg.AppendTaskResult(id, domain.TaskResult{Name: "build", Status: "passed"}); err != nil {
		t.Fatalf("AppendTaskResult: %v", err)
	}

	if err := pg.CancelRun(id); err != nil {
		t.Fatalf("CancelRun: %v", err)
	}
	run, err := pg.GetRun(id)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status != "canceled" {
		t.Errorf("Status = %q, want canceled", run.Status)
	}
}

func TestPG_CancelRun_AlreadyFinished(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "cf", "/")
	id := mustCreateRun(t, pg, "cf", nil)
	if err := pg.FinishRun(id, "passed", "", "", nil); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	if err := pg.CancelRun(id); err != nil {
		t.Fatalf("CancelRun on finished: %v", err)
	}
	run, err := pg.GetRun(id)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status != "passed" {
		t.Errorf("Status = %q, finished run should not be canceled", run.Status)
	}
}

func TestPG_CancelRun_PendingSkipped(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "cps", "/")
	snap := &model.Project{
		Name: "cps",
		Tasks: []model.Task{
			{Name: "deploy", DependsOn: []string{"build"}},
			{Name: "build"},
		},
	}
	id := mustCreateRun(t, pg, "cps", snap)
	if err := pg.AppendTaskResult(id, domain.TaskResult{Name: "build", Status: "running"}); err != nil {
		t.Fatalf("AppendTaskResult: %v", err)
	}
	if err := pg.AppendTaskResult(id, domain.TaskResult{Name: "deploy", Status: "pending"}); err != nil {
		t.Fatalf("AppendTaskResult: %v", err)
	}

	if err := pg.CancelRun(id); err != nil {
		t.Fatalf("CancelRun: %v", err)
	}

	run, err := pg.GetRun(id)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status != "canceled" {
		t.Errorf("Status = %q, want canceled", run.Status)
	}
	for _, tr := range run.TaskResults {
		if tr.Name == "deploy" && tr.Status != "skipped" {
			t.Errorf("deploy should be skipped (dep running→canceled), got %q", tr.Status)
		}
	}
}

// ── NewPG error paths ──────────────────────────────────────────────

func TestPG_NewPG_BadURL(t *testing.T) {
	_, err := NewPG("not-a-valid-url")
	if err == nil {
		t.Fatal("expected error for invalid database URL")
	}
}

func TestPG_NewPG_ConnectionFailed(t *testing.T) {
	_, err := NewPG("postgres://kestrix:kestrix@localhost:54309/nonexistent?sslmode=disable&connect_timeout=2")
	if err == nil {
		t.Fatal("expected error for unreachable database")
	}
}
