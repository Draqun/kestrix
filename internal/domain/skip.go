package domain

import (
	"fmt"
	"strings"
)

// NewTriggerUnsatisfiedSkipResult creates a TaskResult for a task whose trigger was not satisfied.
func NewTriggerUnsatisfiedSkipResult(name string, eval *TriggerEvaluation) TaskResult {
	return TaskResult{
		Name:   name,
		Status: TaskStatusSkipped,
		SkipReason: &SkipReason{
			Category:          SkipCategoryTriggerUnsatisfied,
			Summary:           "Trigger expression not satisfied",
			TriggerEvaluation: eval,
		},
	}
}

// NewDependencySkipReason creates a SkipReason for a task blocked by upstream dependencies.
func NewDependencySkipReason(blockedDeps []string, anyCanceled bool) *SkipReason {
	cat := SkipCategoryDependencyFailed
	action := "failed"
	if anyCanceled {
		cat = SkipCategoryDependencyCanceled
		action = "was canceled"
	}
	return &SkipReason{
		Category:           cat,
		Summary:            fmt.Sprintf("Dependency %s %s", strings.Join(blockedDeps, ", "), action),
		FailedDependencies: blockedDeps,
	}
}

// NewDependencySkipResult creates a TaskResult for a task blocked by upstream dependencies.
func NewDependencySkipResult(name string, blockedDeps []string, anyCanceled bool) TaskResult {
	sr := NewDependencySkipReason(blockedDeps, anyCanceled)
	errMsg := "dependency failed"
	if anyCanceled {
		errMsg = "dependency canceled"
	}
	return TaskResult{
		Name:       name,
		Status:     TaskStatusSkipped,
		Error:      errMsg,
		SkipReason: sr,
	}
}

// NewRunCanceledSkipReason creates a SkipReason for a canceled run.
func NewRunCanceledSkipReason() *SkipReason {
	return &SkipReason{
		Category: SkipCategoryRunCanceled,
		Summary:  "Run was canceled",
	}
}

// NewRunCanceledSkipResult creates a TaskResult for a task that did not execute because the run was canceled.
func NewRunCanceledSkipResult(name string) TaskResult {
	return TaskResult{
		Name:       name,
		Status:     TaskStatusCanceled,
		Error:      "canceled",
		SkipReason: NewRunCanceledSkipReason(),
	}
}

// IsWaitingForManualApproval reports whether a TaskResult represents a manual task waiting for operator approval.
// Tasks that carry a SkipReason (e.g. trigger_unsatisfied, dependency_failed, run_canceled) were bypassed or blocked,
// so they are never eligible for manual approval.
func IsWaitingForManualApproval(tr TaskResult) bool {
	if tr.SkipReason != nil {
		return false
	}
	return tr.Status == TaskStatusManual
}

// ScanBlockedDeps evaluates the status of dependencies using a lookup function.
// It reports whether any dependency is in a failed, canceled, or skipped state that blocks
// downstream execution. Trigger-unsatisfied dependencies (category "trigger_unsatisfied")
// and dependencies not resolved (ok=false) do not block execution.
func ScanBlockedDeps(deps []string, lookup func(dep string) (status string, skipReason *SkipReason, ok bool)) (blocked bool, anyCanceled bool, failedDeps []string) {
	for _, dep := range deps {
		status, sr, ok := lookup(dep)
		if !ok {
			continue
		}
		if status == TaskStatusSkipped && sr != nil && sr.Category == SkipCategoryTriggerUnsatisfied {
			continue
		}
		if status == TaskStatusFailed || status == TaskStatusCanceled || status == TaskStatusSkipped {
			blocked = true
			failedDeps = append(failedDeps, dep)
			if status == TaskStatusCanceled || (sr != nil && (sr.Category == SkipCategoryDependencyCanceled || sr.Category == SkipCategoryRunCanceled)) {
				anyCanceled = true
			}
		}
	}
	return blocked, anyCanceled, failedDeps
}
