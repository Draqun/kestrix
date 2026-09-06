package service

import (
	"errors"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
)

func TestWebhookService_Record_Happy(t *testing.T) {
	sink := &mocks.MockWebhookLogSink{}
	svc := NewWebhookService(sink)

	svc.Record(domain.WebhookEvent{
		Repo:           "org/service-a",
		Branch:         "main",
		Revision:       "0123456789abcdef",
		Commits:        1,
		TriggeredTasks: []string{"test_auth"},
		Status:         domain.WebhookStatusTriggered,
	})

	if len(sink.Entries) != 1 {
		t.Fatalf("expected 1 entry logged, got %d", len(sink.Entries))
	}
	if sink.Entries[0].Status != domain.WebhookStatusTriggered {
		t.Errorf("status = %q, want %q", sink.Entries[0].Status, domain.WebhookStatusTriggered)
	}
}

func TestWebhookService_Record_NilSink(t *testing.T) {
	svc := NewWebhookService(nil)
	svc.Record(domain.WebhookEvent{Status: domain.WebhookStatusSkipped})
}

// A failing history store must not surface to the webhook caller: GitLab
// retries on a non-2xx response, so a reporting outage would become a
// delivery outage.
func TestWebhookService_Record_SinkFailureIsSwallowed(t *testing.T) {
	sink := &mocks.MockWebhookLogSink{RecordErr: errors.New("database unavailable")}
	svc := NewWebhookService(sink)

	svc.Record(domain.WebhookEvent{Repo: "org/service-a", Status: domain.WebhookStatusTriggered})

	if len(sink.Entries) != 0 {
		t.Fatalf("failing sink stored %d entries, want 0", len(sink.Entries))
	}
}
