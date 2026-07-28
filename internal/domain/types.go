// Package domain defines the ports (interfaces) and shared types for the
// Kestrix CI/CD control plane. It is the innermost layer of the hexagonal
// architecture: it imports no other internal package and depends only on
// the Go standard library.
package domain

import (
	"time"
)

// TaskResult captures the outcome of a single task execution.
// It is the single source of truth for task results across the runner,
// store, and service layers.
type TaskResult struct {
	Name     string        `json:"name"`
	Status   string        `json:"status"` // passed, failed, skipped, running, pending, manual
	Output   string        `json:"output"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration" swaggertype:"primitive,integer"`
}
