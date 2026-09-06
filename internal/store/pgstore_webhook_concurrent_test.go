package store

import (
	"strconv"
	"sync"
	"testing"

	"kestrix/internal/domain"
)

// TestRecordWebhookEvent_Concurrent replaces the former in-memory webhook-log
// concurrency test. Recording and reading history now cross a transaction
// boundary, so the race that matters is concurrent writers against readers on
// the same table, with retention trimming running inside every insert.
func TestRecordWebhookEvent_Concurrent(t *testing.T) {
	s := testPG(t)
	const limit = 25
	s.SetWebhookHistoryLimit(limit)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				err := s.RecordWebhookEvent(domain.WebhookEvent{
					Repo:           "org/service-" + strconv.Itoa(worker),
					Branch:         "main",
					Status:         domain.WebhookStatusTriggered,
					TriggeredTasks: []string{"build"},
				})
				if err != nil {
					t.Errorf("RecordWebhookEvent: %v", err)
					return
				}
			}
		}(i)
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				if _, err := s.ListWebhookEvents(domain.WebhookEventFilter{}, 10, 0); err != nil {
					t.Errorf("ListWebhookEvents: %v", err)
					return
				}
				if _, err := s.CountWebhookEvents(domain.WebhookEventFilter{}); err != nil {
					t.Errorf("CountWebhookEvents: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	total, err := s.CountWebhookEvents(domain.WebhookEventFilter{})
	if err != nil {
		t.Fatalf("CountWebhookEvents: %v", err)
	}
	if total > limit {
		t.Fatalf("history holds %d events, want at most %d", total, limit)
	}
}
