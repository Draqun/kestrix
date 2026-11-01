package domain

import (
	"reflect"
	"testing"
)

func TestNewTriggerUnsatisfiedSkipResult(t *testing.T) {
	eval := &TriggerEvaluation{
		Satisfied:  false,
		Expression: "repo.frontend.changed",
		Terms:      map[string]bool{"repo.frontend.changed": false},
	}
	tr := NewTriggerUnsatisfiedSkipResult("build", eval)
	if tr.Name != "build" {
		t.Errorf("Name = %q, want build", tr.Name)
	}
	if tr.Status != "skipped" {
		t.Errorf("Status = %q, want skipped", tr.Status)
	}
	if tr.Error != "" {
		t.Errorf("Error = %q, want empty", tr.Error)
	}
	if tr.SkipReason == nil {
		t.Fatalf("SkipReason is nil")
	}
	if tr.SkipReason.Category != SkipCategoryTriggerUnsatisfied {
		t.Errorf("Category = %q, want %q", tr.SkipReason.Category, SkipCategoryTriggerUnsatisfied)
	}
	if tr.SkipReason.Summary != "Trigger expression not satisfied" {
		t.Errorf("Summary = %q, want 'Trigger expression not satisfied'", tr.SkipReason.Summary)
	}
	if tr.SkipReason.TriggerEvaluation != eval {
		t.Errorf("TriggerEvaluation mismatch")
	}
}

func TestNewDependencySkipResult(t *testing.T) {
	tests := []struct {
		name         string
		taskName     string
		blockedDeps  []string
		anyCanceled  bool
		wantCategory SkipCategory
		wantSummary  string
		wantError    string
	}{
		{
			name:         "dependency failed single",
			taskName:     "test",
			blockedDeps:  []string{"build"},
			anyCanceled:  false,
			wantCategory: SkipCategoryDependencyFailed,
			wantSummary:  "Dependency build failed",
			wantError:    "dependency failed",
		},
		{
			name:         "dependency failed multiple",
			taskName:     "deploy",
			blockedDeps:  []string{"lint", "test"},
			anyCanceled:  false,
			wantCategory: SkipCategoryDependencyFailed,
			wantSummary:  "Dependency lint, test failed",
			wantError:    "dependency failed",
		},
		{
			name:         "dependency canceled",
			taskName:     "deploy",
			blockedDeps:  []string{"build"},
			anyCanceled:  true,
			wantCategory: SkipCategoryDependencyCanceled,
			wantSummary:  "Dependency build was canceled",
			wantError:    "dependency canceled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := NewDependencySkipReason(tt.blockedDeps, tt.anyCanceled)
			if sr.Category != tt.wantCategory {
				t.Errorf("NewDependencySkipReason Category = %q, want %q", sr.Category, tt.wantCategory)
			}
			if sr.Summary != tt.wantSummary {
				t.Errorf("NewDependencySkipReason Summary = %q, want %q", sr.Summary, tt.wantSummary)
			}
			if !reflect.DeepEqual(sr.FailedDependencies, tt.blockedDeps) {
				t.Errorf("NewDependencySkipReason FailedDependencies = %v, want %v", sr.FailedDependencies, tt.blockedDeps)
			}

			tr := NewDependencySkipResult(tt.taskName, tt.blockedDeps, tt.anyCanceled)
			if tr.Name != tt.taskName {
				t.Errorf("Name = %q, want %q", tr.Name, tt.taskName)
			}
			if tr.Status != "skipped" {
				t.Errorf("Status = %q, want skipped", tr.Status)
			}
			if tr.Error != tt.wantError {
				t.Errorf("Error = %q, want %q", tr.Error, tt.wantError)
			}
			if tr.SkipReason == nil {
				t.Fatalf("SkipReason is nil")
			}
			if tr.SkipReason.Category != tt.wantCategory {
				t.Errorf("Category = %q, want %q", tr.SkipReason.Category, tt.wantCategory)
			}
			if tr.SkipReason.Summary != tt.wantSummary {
				t.Errorf("Summary = %q, want %q", tr.SkipReason.Summary, tt.wantSummary)
			}
			if !reflect.DeepEqual(tr.SkipReason.FailedDependencies, tt.blockedDeps) {
				t.Errorf("FailedDependencies = %v, want %v", tr.SkipReason.FailedDependencies, tt.blockedDeps)
			}
		})
	}
}

func TestNewRunCanceledSkipReasonAndResult(t *testing.T) {
	sr := NewRunCanceledSkipReason()
	if sr.Category != SkipCategoryRunCanceled {
		t.Errorf("Category = %q, want %q", sr.Category, SkipCategoryRunCanceled)
	}
	if sr.Summary != "Run was canceled" {
		t.Errorf("Summary = %q, want 'Run was canceled'", sr.Summary)
	}

	tr := NewRunCanceledSkipResult("cleanup")
	if tr.Name != "cleanup" {
		t.Errorf("Name = %q, want cleanup", tr.Name)
	}
	if tr.Status != "canceled" {
		t.Errorf("Status = %q, want canceled", tr.Status)
	}
	if tr.Error != "canceled" {
		t.Errorf("Error = %q, want canceled", tr.Error)
	}
	if tr.SkipReason == nil || tr.SkipReason.Category != SkipCategoryRunCanceled {
		t.Errorf("SkipReason = %+v, want category %q", tr.SkipReason, SkipCategoryRunCanceled)
	}
}

func TestIsWaitingForManualApproval(t *testing.T) {
	tests := []struct {
		name string
		tr   TaskResult
		want bool
	}{
		{
			name: "has SkipReason trigger_unsatisfied",
			tr: TaskResult{
				Status:     "skipped",
				SkipReason: &SkipReason{Category: SkipCategoryTriggerUnsatisfied},
			},
			want: false,
		},
		{
			name: "run_canceled SkipReason on canceled status",
			tr: TaskResult{
				Status:     "canceled",
				SkipReason: NewRunCanceledSkipReason(),
			},
			want: false,
		},
		{
			name: "run_canceled SkipReason on skipped status",
			tr: TaskResult{
				Status:     "skipped",
				SkipReason: NewRunCanceledSkipReason(),
			},
			want: false,
		},
		{
			name: "dependency_failed SkipReason with empty error",
			tr: TaskResult{
				Status:     "skipped",
				SkipReason: &SkipReason{Category: SkipCategoryDependencyFailed},
			},
			want: false,
		},
		{
			name: "dependency_canceled SkipReason with empty error",
			tr: TaskResult{
				Status:     "skipped",
				SkipReason: &SkipReason{Category: SkipCategoryDependencyCanceled},
			},
			want: false,
		},
		{
			name: "status manual without SkipReason",
			tr:   TaskResult{Status: "manual"},
			want: true,
		},
		{
			name: "skipped with empty error",
			tr:   TaskResult{Status: "skipped"},
			want: false,
		},
		{
			name: "skipped with generic error",
			tr:   TaskResult{Status: "skipped", Error: "approval needed"},
			want: false,
		},
		{
			name: "skipped with dependency failed error",
			tr:   TaskResult{Status: "skipped", Error: "dependency failed"},
			want: false,
		},
		{
			name: "skipped with dependency canceled error",
			tr:   TaskResult{Status: "skipped", Error: "dependency canceled"},
			want: false,
		},
		{
			name: "passed status",
			tr:   TaskResult{Status: "passed"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsWaitingForManualApproval(tt.tr)
			if got != tt.want {
				t.Errorf("IsWaitingForManualApproval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScanBlockedDeps(t *testing.T) {
	tests := []struct {
		name        string
		deps        []string
		lookupTable map[string]struct {
			status string
			sr     *SkipReason
			ok     bool
		}
		wantBlocked     bool
		wantAnyCanceled bool
		wantFailedDeps  []string
	}{
		{
			name: "all deps passed",
			deps: []string{"dep1", "dep2"},
			lookupTable: map[string]struct {
				status string
				sr     *SkipReason
				ok     bool
			}{
				"dep1": {status: "passed", ok: true},
				"dep2": {status: "passed", ok: true},
			},
			wantBlocked:     false,
			wantAnyCanceled: false,
			wantFailedDeps:  nil,
		},
		{
			name: "trigger unsatisfied dep ignored",
			deps: []string{"dep1"},
			lookupTable: map[string]struct {
				status string
				sr     *SkipReason
				ok     bool
			}{
				"dep1": {
					status: "skipped",
					sr:     &SkipReason{Category: SkipCategoryTriggerUnsatisfied},
					ok:     true,
				},
			},
			wantBlocked:     false,
			wantAnyCanceled: false,
			wantFailedDeps:  nil,
		},
		{
			name: "dep not found (ok=false) ignored",
			deps: []string{"outside"},
			lookupTable: map[string]struct {
				status string
				sr     *SkipReason
				ok     bool
			}{},
			wantBlocked:     false,
			wantAnyCanceled: false,
			wantFailedDeps:  nil,
		},
		{
			name: "dep failed",
			deps: []string{"build"},
			lookupTable: map[string]struct {
				status string
				sr     *SkipReason
				ok     bool
			}{
				"build": {status: "failed", ok: true},
			},
			wantBlocked:     true,
			wantAnyCanceled: false,
			wantFailedDeps:  []string{"build"},
		},
		{
			name: "dep canceled directly",
			deps: []string{"build"},
			lookupTable: map[string]struct {
				status string
				sr     *SkipReason
				ok     bool
			}{
				"build": {status: "canceled", ok: true},
			},
			wantBlocked:     true,
			wantAnyCanceled: true,
			wantFailedDeps:  []string{"build"},
		},
		{
			name: "dep skipped due to dependency_canceled",
			deps: []string{"build"},
			lookupTable: map[string]struct {
				status string
				sr     *SkipReason
				ok     bool
			}{
				"build": {
					status: "skipped",
					sr:     &SkipReason{Category: SkipCategoryDependencyCanceled},
					ok:     true,
				},
			},
			wantBlocked:     true,
			wantAnyCanceled: true,
			wantFailedDeps:  []string{"build"},
		},
		{
			name: "dep skipped due to run_canceled",
			deps: []string{"build"},
			lookupTable: map[string]struct {
				status string
				sr     *SkipReason
				ok     bool
			}{
				"build": {
					status: "skipped",
					sr:     &SkipReason{Category: SkipCategoryRunCanceled},
					ok:     true,
				},
			},
			wantBlocked:     true,
			wantAnyCanceled: true,
			wantFailedDeps:  []string{"build"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocked, anyCanceled, failedDeps := ScanBlockedDeps(tt.deps, func(dep string) (string, *SkipReason, bool) {
				entry, ok := tt.lookupTable[dep]
				return entry.status, entry.sr, ok
			})
			if blocked != tt.wantBlocked {
				t.Errorf("blocked = %v, want %v", blocked, tt.wantBlocked)
			}
			if anyCanceled != tt.wantAnyCanceled {
				t.Errorf("anyCanceled = %v, want %v", anyCanceled, tt.wantAnyCanceled)
			}
			if !reflect.DeepEqual(failedDeps, tt.wantFailedDeps) {
				t.Errorf("failedDeps = %v, want %v", failedDeps, tt.wantFailedDeps)
			}
		})
	}
}
