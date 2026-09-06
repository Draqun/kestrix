package store

import (
	"testing"
	"time"

	"kestrix/internal/domain"
)

// recordAt stores an event with an explicit receive time so ordering and
// range filters are deterministic.
func recordAt(t *testing.T, pg *PGStore, at time.Time, event domain.WebhookEvent) {
	t.Helper()
	event.ReceivedAt = at
	if err := pg.RecordWebhookEvent(event); err != nil {
		t.Fatalf("RecordWebhookEvent: %v", err)
	}
}

func TestRecordAndListWebhookEvents(t *testing.T) {
	pg := testPG(t)
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	recordAt(t, pg, base, domain.WebhookEvent{
		ProjectName: "web", Repo: "org/web", Branch: "main", Revision: "abc123",
		Commits: 2, TriggeredTasks: []string{"build", "test"},
		Status: domain.WebhookStatusTriggered, DispatchedID: 42,
		DispatchedURL: "https://gitlab.example/org/web/-/pipelines/42",
	})
	recordAt(t, pg, base.Add(time.Minute), domain.WebhookEvent{
		Repo: "org/unknown", Branch: "main", Status: domain.WebhookStatusIgnored,
		Error: "unknown project",
	})

	events, err := pg.ListWebhookEvents(domain.WebhookEventFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("ListWebhookEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Repo != "org/unknown" {
		t.Errorf("events are not newest first: %+v", events)
	}
	if events[0].ProjectName != "" {
		t.Errorf("unmatched event project = %q, want empty", events[0].ProjectName)
	}

	dispatched := events[1]
	if dispatched.ProjectName != "web" || dispatched.Commits != 2 {
		t.Errorf("event = %+v", dispatched)
	}
	if len(dispatched.TriggeredTasks) != 2 || dispatched.TriggeredTasks[0] != "build" {
		t.Errorf("triggered tasks = %v", dispatched.TriggeredTasks)
	}
	if dispatched.DispatchedID != 42 || dispatched.DispatchedURL == "" {
		t.Errorf("dispatch result lost: id=%d url=%q", dispatched.DispatchedID, dispatched.DispatchedURL)
	}
	if !dispatched.ReceivedAt.UTC().Equal(base) {
		t.Errorf("received_at = %s, want %s", dispatched.ReceivedAt.UTC(), base)
	}
}

func TestListWebhookEventsFilters(t *testing.T) {
	pg := testPG(t)
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	recordAt(t, pg, base, domain.WebhookEvent{
		ProjectName: "web", Repo: "org/web", Status: domain.WebhookStatusTriggered})
	recordAt(t, pg, base.Add(time.Minute), domain.WebhookEvent{
		ProjectName: "web", Repo: "org/web", Status: domain.WebhookStatusError, Error: "parse failed"})
	recordAt(t, pg, base.Add(2*time.Minute), domain.WebhookEvent{
		ProjectName: "api", Repo: "org/api", Status: domain.WebhookStatusError, Error: "parse failed"})
	recordAt(t, pg, base.Add(3*time.Minute), domain.WebhookEvent{
		ProjectName: "web", Repo: "org/web", Status: domain.WebhookStatusSkipped})

	since := base.Add(time.Minute)
	until := base.Add(2 * time.Minute)
	cases := []struct {
		name   string
		filter domain.WebhookEventFilter
		want   int
	}{
		{"project", domain.WebhookEventFilter{ProjectName: "web"}, 3},
		{"status", domain.WebhookEventFilter{Status: domain.WebhookStatusError}, 2},
		{"since", domain.WebhookEventFilter{Since: &since}, 3},
		{"until", domain.WebhookEventFilter{Until: &until}, 3},
		{"range", domain.WebhookEventFilter{Since: &since, Until: &until}, 2},
		{"conjunction", domain.WebhookEventFilter{
			ProjectName: "web", Status: domain.WebhookStatusError, Since: &since, Until: &until}, 1},
		{"no match", domain.WebhookEventFilter{ProjectName: "missing"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			count, err := pg.CountWebhookEvents(tc.filter)
			if err != nil {
				t.Fatalf("CountWebhookEvents: %v", err)
			}
			if count != tc.want {
				t.Errorf("count = %d, want %d", count, tc.want)
			}
			events, err := pg.ListWebhookEvents(tc.filter, 100, 0)
			if err != nil {
				t.Fatalf("ListWebhookEvents: %v", err)
			}
			if len(events) != tc.want {
				t.Errorf("listed %d events, want %d", len(events), tc.want)
			}
		})
	}
}

func TestListWebhookEventsPagination(t *testing.T) {
	pg := testPG(t)
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		recordAt(t, pg, base.Add(time.Duration(i)*time.Minute), domain.WebhookEvent{
			ProjectName: "web", Repo: "org/web", Branch: "main",
			Status: domain.WebhookStatusTriggered,
		})
	}

	first, err := pg.ListWebhookEvents(domain.WebhookEventFilter{}, 2, 0)
	if err != nil {
		t.Fatalf("ListWebhookEvents: %v", err)
	}
	second, err := pg.ListWebhookEvents(domain.WebhookEventFilter{}, 2, 2)
	if err != nil {
		t.Fatalf("ListWebhookEvents: %v", err)
	}
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("pages = %d and %d, want 2 and 2", len(first), len(second))
	}
	if !first[1].ReceivedAt.After(second[0].ReceivedAt) {
		t.Errorf("pages overlap or are misordered: %s then %s", first[1].ReceivedAt, second[0].ReceivedAt)
	}

	past, err := pg.ListWebhookEvents(domain.WebhookEventFilter{}, 2, 99)
	if err != nil {
		t.Fatalf("ListWebhookEvents: %v", err)
	}
	if len(past) != 0 {
		t.Errorf("offset past end returned %d events, want 0", len(past))
	}
}

// Retention keeps history bounded: the oldest events fall off once the limit
// is exceeded, and recording never fails because of accumulated history.
func TestRecordWebhookEventEnforcesRetention(t *testing.T) {
	pg := testPG(t)
	const limit = 5
	pg.SetWebhookHistoryLimit(limit)
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	for i := 0; i < limit+5; i++ {
		recordAt(t, pg, base.Add(time.Duration(i)*time.Minute), domain.WebhookEvent{
			ProjectName: "web", Repo: "org/web", Branch: "main",
			Revision: "rev" + time.Duration(i).String(),
			Status:   domain.WebhookStatusTriggered,
		})
	}

	total, err := pg.CountWebhookEvents(domain.WebhookEventFilter{})
	if err != nil {
		t.Fatalf("CountWebhookEvents: %v", err)
	}
	if total != limit {
		t.Fatalf("history holds %d events, want %d", total, limit)
	}

	events, err := pg.ListWebhookEvents(domain.WebhookEventFilter{}, 100, 0)
	if err != nil {
		t.Fatalf("ListWebhookEvents: %v", err)
	}
	oldestKept := events[len(events)-1].ReceivedAt.UTC()
	wantOldest := base.Add(5 * time.Minute)
	if !oldestKept.Equal(wantOldest) {
		t.Errorf("oldest kept event = %s, want %s", oldestKept, wantOldest)
	}
	newestKept := events[0].ReceivedAt.UTC()
	if !newestKept.Equal(base.Add(9 * time.Minute)) {
		t.Errorf("newest kept event = %s, want %s", newestKept, base.Add(9*time.Minute))
	}
}

// A non-positive limit must fall back to the default rather than trimming
// history to nothing.
func TestSetWebhookHistoryLimitRejectsNonPositive(t *testing.T) {
	pg := testPG(t)
	pg.SetWebhookHistoryLimit(0)
	if pg.webhookHistoryLimit != DefaultWebhookHistoryLimit {
		t.Fatalf("limit = %d, want %d", pg.webhookHistoryLimit, DefaultWebhookHistoryLimit)
	}
	pg.SetWebhookHistoryLimit(-3)
	if pg.webhookHistoryLimit != DefaultWebhookHistoryLimit {
		t.Fatalf("limit = %d, want %d", pg.webhookHistoryLimit, DefaultWebhookHistoryLimit)
	}
}

// The migration is idempotent: a second NewPG against the same database must
// leave the table and its rows intact.
func TestWebhookEventsMigrationIsIdempotent(t *testing.T) {
	pg := testPG(t)
	recordAt(t, pg, time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC), domain.WebhookEvent{
		ProjectName: "web", Repo: "org/web", Status: domain.WebhookStatusTriggered})

	if err := migrate(pg.db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	total, err := pg.CountWebhookEvents(domain.WebhookEventFilter{})
	if err != nil {
		t.Fatalf("CountWebhookEvents: %v", err)
	}
	if total != 1 {
		t.Fatalf("history holds %d events after re-migration, want 1", total)
	}
}
