// Shared type aliases for the store package. These point at the domain
// types so the rest of the codebase (web, runner, cmd) can keep using
// store.ProjectConfig / store.Run / store.TaskResult without needing a
// second migration pass.
package store

import (
	"kestrix/internal/domain"
)

// TaskResult is an alias for domain.TaskResult, the single type used
// across runner, store, and service layers.
type TaskResult = domain.TaskResult

// Run is an alias for domain.Run.
type Run = domain.Run

// ProjectConfig is an alias for domain.ProjectConfig so existing call
// sites in the web layer keep compiling while Store migrates to
// domain.ProjectStore.
type ProjectConfig = domain.ProjectConfig

// WebhookEvent is an alias for domain.WebhookEvent.
type WebhookEvent = domain.WebhookEvent

// WebhookEventFilter is an alias for domain.WebhookEventFilter.
type WebhookEventFilter = domain.WebhookEventFilter
