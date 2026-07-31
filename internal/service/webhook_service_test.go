package service

import (
	"testing"

	"kestrix/internal/mocks"
)

func TestWebhookService_Process_Happy(t *testing.T) {
	sink := &mocks.MockWebhookLogSink{}
	svc := NewWebhookService(sink)

	event := map[string]interface{}{
		"object_kind":         "push",
		"project":             map[string]interface{}{"id": float64(123)},
		"ref":                 "refs/heads/main",
		"commits":             []interface{}{},
		"total_commits_count": float64(1),
	}
	err := svc.Process(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sink.Entries) != 1 {
		t.Fatalf("expected 1 entry logged, got %d", len(sink.Entries))
	}
}

func TestWebhookService_Process_NilSink(t *testing.T) {
	svc := NewWebhookService(nil)
	if err := svc.Process(map[string]interface{}{"event": "push"}); err != nil {
		t.Fatalf("nil sink returned error: %v", err)
	}
}
