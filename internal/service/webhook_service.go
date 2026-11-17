package service

import (
	"log/slog"

	"kestrix/internal/domain"
	"kestrix/internal/redact"
)

// WebhookService handles webhook event processing.
type WebhookService struct {
	logSink domain.WebhookLogSink
}

// NewWebhookService creates a WebhookService.
func NewWebhookService(logSink domain.WebhookLogSink) *WebhookService {
	return &WebhookService{logSink: logSink}
}

// Record stores a processed webhook event in history. A storage failure is
// logged and swallowed: webhook delivery must not fail because history is
// unavailable, or GitLab would retry a push that Kestrix already handled.
func (s *WebhookService) Record(event domain.WebhookEvent) {
	if s == nil || s.logSink == nil {
		return
	}
	if err := s.logSink.Record(event); err != nil {
		slog.Warn("webhook history: record event failed", "repo", event.Repo, "status", event.Status, "err", redact.Err(err, nil))
	}
}
