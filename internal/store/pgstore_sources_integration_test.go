package store

import (
	"slices"
	"testing"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/model"
	"kestrix/internal/service"
)

func upsertWithURL(t *testing.T, pg *PGStore, name, url string) {
	t.Helper()
	if err := pg.Upsert(&domain.ProjectConfig{Name: name, RepoURL: url, RepoRef: "main"}); err != nil {
		t.Fatalf("Upsert(%q): %v", name, err)
	}
}

func sourceNames(sources []domain.ProjectSource) []string {
	names := make([]string, 0, len(sources))
	for _, s := range sources {
		names = append(names, s.Name)
	}
	return names
}

// The schema block and the ALTER statement are idempotent: a second migrate
// against the database the first one produced changes nothing and fails
// nothing. testPG has already run migrate once through NewPG.
func TestMigrateSourcesSchemaIsIdempotent(t *testing.T) {
	pg := testPG(t)
	for i := 0; i < 2; i++ {
		if err := migrate(pg.db); err != nil {
			t.Fatalf("migrate #%d: %v", i+2, err)
		}
	}
	var n int
	if err := pg.db.QueryRow(`SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'webhook_events' AND column_name = 'source_name'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("source_name column count = %d, err = %v", n, err)
	}
	if err := pg.db.QueryRow(`SELECT count(*) FROM pg_indexes WHERE indexname = 'idx_project_sources_key'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("idx_project_sources_key count = %d, err = %v", n, err)
	}
}

func TestReplaceSourcesReplacesTheWholeSet(t *testing.T) {
	pg := testPG(t)
	upsertWithURL(t, pg, "checkout", "https://git.example.test/team/checkout.git")

	first := []domain.ProjectSource{
		{Name: "checkout", URL: "https://git.example.test/team/checkout.git", Primary: true},
		{Name: "a", URL: "https://git.example.test/team/a.git"},
		{Name: "b", URL: "https://git.example.test/team/b.git"},
	}
	if err := pg.ReplaceSources("checkout", first); err != nil {
		t.Fatalf("ReplaceSources: %v", err)
	}
	// Drop "a"; the row must disappear rather than linger.
	if err := pg.ReplaceSources("checkout", first[:1]); err != nil {
		t.Fatalf("ReplaceSources: %v", err)
	}
	if err := pg.ReplaceSources("checkout", append(first[:1:1], first[2])); err != nil {
		t.Fatalf("ReplaceSources: %v", err)
	}
	p, err := pg.Get("checkout")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := sourceNames(p.Sources); len(got) != 2 || got[0] != "checkout" || got[1] != "b" {
		t.Fatalf("sources = %v, want [checkout b]", got)
	}
	if !p.Sources[0].Primary || p.Sources[1].Primary {
		t.Fatalf("primary flags = %+v", p.Sources)
	}

	// A bad URL rejects the whole replacement and keeps the previous set.
	if err := pg.ReplaceSources("checkout", []domain.ProjectSource{{Name: "x", URL: "https://user:pw@h/x.git"}}); err == nil {
		t.Fatal("ReplaceSources accepted a URL with embedded credentials")
	}
	p, _ = pg.Get("checkout")
	if len(p.Sources) != 2 {
		t.Fatalf("failed replacement changed sources: %+v", p.Sources)
	}
}

func TestReplaceSourcesCollapsesDuplicateKeys(t *testing.T) {
	pg := testPG(t)
	upsertWithURL(t, pg, "checkout", "https://git.example.test/team/checkout.git")
	if err := pg.ReplaceSources("checkout", []domain.ProjectSource{
		{Name: "checkout", URL: "https://git.example.test/team/checkout.git", Primary: true},
		{Name: "shared", URL: "https://git.example.test/team/shared.git"},
		{Name: "shared-ssh", URL: "git@git.example.test:team/shared.git"},
	}); err != nil {
		t.Fatalf("ReplaceSources: %v", err)
	}
	p, _ := pg.Get("checkout")
	if got := sourceNames(p.Sources); len(got) != 2 || got[1] != "shared" {
		t.Fatalf("sources = %v, want [checkout shared]", got)
	}
}

func TestFindBySourceKeyReturnsEveryDeclaringProject(t *testing.T) {
	pg := testPG(t)
	upsertWithURL(t, pg, "checkout", "https://git.example.test/team/checkout.git")
	upsertWithURL(t, pg, "billing", "https://git.example.test/team/billing.git")
	upsertWithURL(t, pg, "other", "https://git.example.test/team/other.git")
	shared := domain.ProjectSource{Name: "shared", URL: "https://git.example.test/team/shared.git"}
	for _, name := range []string{"checkout", "billing"} {
		if err := pg.ReplaceSources(name, []domain.ProjectSource{
			{Name: name, URL: "https://git.example.test/team/" + name + ".git", Primary: true}, shared,
		}); err != nil {
			t.Fatalf("ReplaceSources(%s): %v", name, err)
		}
	}

	// The SSH form of the shared URL and an unrelated key: both projects, by name.
	found, err := pg.FindBySourceKey([]string{"git.example.test/team/shared", "nowhere/x"})
	if err != nil {
		t.Fatalf("FindBySourceKey: %v", err)
	}
	if len(found) != 2 || found[0].Name != "billing" || found[1].Name != "checkout" {
		t.Fatalf("found = %v", found)
	}
	if got := sourceNames(found[0].Sources); len(got) != 2 || got[0] != "billing" || got[1] != "shared" {
		t.Fatalf("billing sources = %v, want primary first then shared", got)
	}

	// A primary key resolves its own project only.
	found, err = pg.FindBySourceKey([]string{"git.example.test/team/checkout"})
	if err != nil || len(found) != 1 || found[0].Name != "checkout" {
		t.Fatalf("primary lookup = %v, %v", found, err)
	}
	if found, err = pg.FindBySourceKey(nil); err != nil || len(found) != 0 {
		t.Fatalf("empty lookup = %v, %v", found, err)
	}
	if found, err = pg.FindBySourceKey([]string{"nowhere/x"}); err != nil || len(found) != 0 {
		t.Fatalf("unknown lookup = %v, %v", found, err)
	}
}

// insertLegacyProject writes a project row the way the previous release did:
// without any project_sources row.
func insertLegacyProject(t *testing.T, pg *PGStore, name, url string) {
	t.Helper()
	if _, err := pg.db.Exec(`INSERT INTO projects (name, repo_url, repo_ref) VALUES ($1, $2, 'main')`, name, url); err != nil {
		t.Fatalf("insert legacy project %s: %v", name, err)
	}
}

// A project row from before this release has no sources. Startup backfills
// exactly the primary row for such rows, once.
func TestMigrateBackfillsPrimarySources(t *testing.T) {
	pg := testPG(t)
	insertLegacyProject(t, pg, "legacy", "https://git.example.test/team/legacy.git")
	upsertWithURL(t, pg, "current", "https://git.example.test/team/current.git")
	if err := pg.ReplaceSources("current", []domain.ProjectSource{
		{Name: "current", URL: "https://git.example.test/team/current.git", Primary: true},
		{Name: "lib", URL: "https://git.example.test/team/lib.git"},
	}); err != nil {
		t.Fatalf("ReplaceSources: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := migrate(pg.db); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		legacy, _ := pg.Get("legacy")
		if len(legacy.Sources) != 1 || !legacy.Sources[0].Primary || legacy.Sources[0].Name != "legacy" ||
			legacy.Sources[0].URL != "https://git.example.test/team/legacy.git" {
			t.Fatalf("pass %d: legacy sources = %+v, want one primary row", i+1, legacy.Sources)
		}
		current, _ := pg.Get("current")
		if len(current.Sources) != 2 {
			t.Fatalf("pass %d: backfill touched a project that already had sources: %+v", i+1, current.Sources)
		}
	}
	found, err := pg.FindBySourceKey([]string{"git.example.test/team/legacy"})
	if err != nil || len(found) != 1 {
		t.Fatalf("backfilled project not routable: %v, %v", found, err)
	}
}

// Upsert keeps the primary source row in step with repo_url inside its own
// transaction, so a registration whose secondary refresh later fails still
// routes pushes to its new repository and never to the old one.
func TestUpsertMaintainsPrimarySourceRow(t *testing.T) {
	pg := testPG(t)
	upsertWithURL(t, pg, "checkout", "https://git.example.test/team/checkout.git")
	p, _ := pg.Get("checkout")
	if len(p.Sources) != 1 || !p.Sources[0].Primary || p.Sources[0].URL != "https://git.example.test/team/checkout.git" {
		t.Fatalf("sources after create = %+v, want the primary row", p.Sources)
	}
	if err := pg.ReplaceSources("checkout", append(p.Sources, domain.ProjectSource{Name: "shared", URL: "https://git.example.test/team/shared.git"})); err != nil {
		t.Fatal(err)
	}

	upsertWithURL(t, pg, "checkout", "https://git.example.test/team/moved.git")
	p, _ = pg.Get("checkout")
	if got := sourceNames(p.Sources); len(got) != 2 || got[0] != "checkout" || got[1] != "shared" {
		t.Fatalf("sources after move = %v, want [checkout shared]", got)
	}
	if p.Sources[0].URL != "https://git.example.test/team/moved.git" {
		t.Fatalf("primary URL = %q, want the new repo_url", p.Sources[0].URL)
	}
	if found, _ := pg.FindBySourceKey([]string{"git.example.test/team/checkout"}); len(found) != 0 {
		t.Fatalf("old repo_url still routes: %v", found)
	}
	if found, _ := pg.FindBySourceKey([]string{"git.example.test/team/moved"}); len(found) != 1 {
		t.Fatalf("new repo_url does not route: %v", found)
	}
}

func TestDeleteProjectCascadesSources(t *testing.T) {
	pg := testPG(t)
	upsertWithURL(t, pg, "gone", "https://git.example.test/team/gone.git")
	if err := pg.ReplaceSources("gone", []domain.ProjectSource{{Name: "gone", URL: "https://git.example.test/team/gone.git", Primary: true}}); err != nil {
		t.Fatal(err)
	}
	if err := pg.Delete("gone"); err != nil {
		t.Fatal(err)
	}
	found, err := pg.FindBySourceKey([]string{"git.example.test/team/gone"})
	if err != nil || len(found) != 0 {
		t.Fatalf("sources survived project deletion: %v, %v", found, err)
	}
}

func TestWebhookEventRoundTripsSourceName(t *testing.T) {
	pg := testPG(t)
	recordAt(t, pg, time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC), domain.WebhookEvent{
		ProjectName: "checkout", SourceName: "shared", Repo: "team/shared", Status: domain.WebhookStatusTriggered,
	})
	recordAt(t, pg, time.Date(2026, 9, 1, 10, 1, 0, 0, time.UTC), domain.WebhookEvent{
		Repo: "team/unknown", Status: domain.WebhookStatusIgnored, Error: "unknown project",
	})
	events, err := pg.ListWebhookEvents(domain.WebhookEventFilter{}, 10, 0)
	if err != nil || len(events) != 2 {
		t.Fatalf("ListWebhookEvents: %v, %v", events, err)
	}
	if events[0].SourceName != "" || events[1].SourceName != "shared" || events[1].ProjectName != "checkout" {
		t.Fatalf("source names = %q / %q", events[0].SourceName, events[1].SourceName)
	}
}

// A database written by the previous release has no project_sources table and
// no source_name column, but does have projects and webhook history. Starting
// the new binary must add both without touching existing rows.
func TestMigrateUpgradesSchemaFromPreviousRelease(t *testing.T) {
	pg := testPG(t)
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS project_sources`,
		`ALTER TABLE webhook_events DROP COLUMN IF EXISTS source_name`,
		`INSERT INTO projects (name, repo_url, repo_ref) VALUES ('legacy', 'https://git.example.test/team/legacy.git', 'main')`,
		`INSERT INTO webhook_events (project_name, repo, status) VALUES ('legacy', 'team/legacy', 'triggered')`,
	} {
		if _, err := pg.db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	if err := migrate(pg.db); err != nil {
		t.Fatalf("migrate from previous schema: %v", err)
	}

	legacy, err := pg.Get("legacy")
	if err != nil || legacy == nil {
		t.Fatalf("Get(legacy) = %v, %v", legacy, err)
	}
	if len(legacy.Sources) != 1 || !legacy.Sources[0].Primary {
		t.Fatalf("legacy sources after upgrade = %+v", legacy.Sources)
	}
	events, err := pg.ListWebhookEvents(domain.WebhookEventFilter{}, 10, 0)
	if err != nil || len(events) != 1 || events[0].ProjectName != "legacy" || events[0].SourceName != "" {
		t.Fatalf("history after upgrade = %+v, %v", events, err)
	}
}

// The web package's refreshSources compares the set the store returns with the
// one service.DeriveSources produces, and skips a write when they are equal.
// That only holds if both order names the same way: DeriveSources sorts by
// bytes, so the store's ORDER BY carries COLLATE "C". Without it the order
// follows the database collation, and a glibc-collated database sorts a name
// with mixed case or punctuation differently - the two sets then never compare
// equal and every delivery rewrites project_sources.
//
// A musl-collated image (the one both Compose files use) collates byte-wise
// anyway, so this test is only discriminating on a glibc-collated database -
// which is what a Debian image, RDS, or Cloud SQL gives you.
func TestSourceOrderIsByteWiseWhateverTheCollation(t *testing.T) {
	pg := testPG(t)
	const primaryURL = "https://git.example.test/team/checkout.git"
	upsertWithURL(t, pg, "checkout", primaryURL)

	// Names chosen so that byte order and en_US.utf8 order disagree: uppercase
	// sorts before lowercase by byte, after it under en_US, and '-' and '_'
	// are ignored at the primary level by en_US but not by byte.
	declared := []domain.ProjectSource{
		{Name: "checkout", URL: primaryURL, Primary: true},
		{Name: "alpha", URL: "https://git.example.test/team/alpha.git"},
		{Name: "Zebra", URL: "https://git.example.test/team/z.git"},
		{Name: "a-b", URL: "https://git.example.test/team/a-b.git"},
		{Name: "a_b", URL: "https://git.example.test/team/a-underscore-b.git"},
		{Name: "ab", URL: "https://git.example.test/team/ab.git"},
		{Name: "shared", URL: "https://git.example.test/team/s-lower.git"},
		{Name: "Shared", URL: "https://git.example.test/team/s-upper.git"},
	}
	if err := pg.ReplaceSources("checkout", declared); err != nil {
		t.Fatalf("ReplaceSources: %v", err)
	}

	got, err := pg.Get("checkout")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := []string{"checkout", "Shared", "Zebra", "a-b", "a_b", "ab", "alpha", "shared"}
	if names := sourceNames(got.Sources); !slices.Equal(names, want) {
		t.Fatalf("sources = %v, want %v", names, want)
	}

	// The invariant refreshSources actually relies on: the stored set and the
	// derived one are equal, element for element.
	repos := make([]model.Repo, 0, len(declared)-1)
	for _, src := range declared[1:] {
		repos = append(repos, model.Repo{Name: src.Name, URL: src.URL, Ref: "main"})
	}
	derived := service.DeriveSources(
		&domain.ProjectConfig{Name: "checkout", RepoURL: primaryURL},
		model.Project{Repos: repos},
	)
	if !slices.Equal(derived, got.Sources) {
		t.Fatalf("derived set differs from the stored one:\n derived = %+v\n stored  = %+v", derived, got.Sources)
	}
}

// A delivery resolved through the gitlab_project_id fallback goes down the same
// write-skip path, so its config needs its sources too.
func TestFindByGitLabIDPopulatesSources(t *testing.T) {
	pg := testPG(t)
	const primaryURL = "https://git.example.test/team/legacy.git"
	if err := pg.Upsert(&domain.ProjectConfig{
		Name: "legacy", RepoURL: primaryURL, RepoRef: "main", GitLabProjectID: 4242,
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := pg.ReplaceSources("legacy", []domain.ProjectSource{
		{Name: "legacy", URL: primaryURL, Primary: true},
		{Name: "shared", URL: "https://git.example.test/team/shared.git"},
	}); err != nil {
		t.Fatalf("ReplaceSources: %v", err)
	}

	found, err := pg.FindByGitLabID(4242)
	if err != nil || found == nil {
		t.Fatalf("FindByGitLabID = %v, %v", found, err)
	}
	if names := sourceNames(found.Sources); !slices.Equal(names, []string{"legacy", "shared"}) {
		t.Fatalf("sources = %v, want [legacy shared]", names)
	}
	if !found.Sources[0].Primary {
		t.Fatalf("first source is not the primary: %+v", found.Sources)
	}
}

// UpsertWithSources is one transaction: a source the store refuses (here a URL
// carrying credentials) must take the project row down with it, so a create
// that answered with an error leaves no project behind.
func TestUpsertWithSourcesIsAtomic(t *testing.T) {
	pg := testPG(t)
	const primaryURL = "https://git.example.test/team/atomic.git"
	cfg := &domain.ProjectConfig{Name: "atomic", RepoURL: primaryURL, RepoRef: "main"}

	err := pg.UpsertWithSources(cfg, []domain.ProjectSource{
		{Name: "atomic", URL: primaryURL, Primary: true},
		{Name: "bad", URL: "https://user:pw@git.example.test/team/bad.git"},
	})
	if err == nil {
		t.Fatal("UpsertWithSources accepted a URL with embedded credentials")
	}
	got, getErr := pg.Get("atomic")
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	if got != nil {
		t.Fatalf("the project row survived a rejected source set: %+v", got)
	}

	// The same call with a set the store accepts writes both halves.
	if err := pg.UpsertWithSources(cfg, []domain.ProjectSource{
		{Name: "atomic", URL: primaryURL, Primary: true},
		{Name: "shared", URL: "https://git.example.test/team/shared.git"},
	}); err != nil {
		t.Fatalf("UpsertWithSources: %v", err)
	}
	got, _ = pg.Get("atomic")
	if got == nil || !slices.Equal(sourceNames(got.Sources), []string{"atomic", "shared"}) {
		t.Fatalf("stored project = %+v", got)
	}

	// An update that fails leaves the previous row and set untouched.
	moved := &domain.ProjectConfig{Name: "atomic", RepoURL: "https://git.example.test/team/moved.git", RepoRef: "release"}
	if err := pg.UpsertWithSources(moved, []domain.ProjectSource{
		{Name: "atomic", URL: moved.RepoURL, Primary: true},
		{Name: "bad", URL: "https://user:pw@git.example.test/team/bad.git"},
	}); err == nil {
		t.Fatal("UpsertWithSources accepted a bad set on update")
	}
	got, _ = pg.Get("atomic")
	if got.RepoURL != primaryURL || got.RepoRef != "main" {
		t.Fatalf("a failed update changed the project row: %+v", got)
	}
	if !slices.Equal(sourceNames(got.Sources), []string{"atomic", "shared"}) {
		t.Fatalf("a failed update changed the source set: %+v", got.Sources)
	}
}
