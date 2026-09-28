package service

import "testing"

// TestRunService_CheckLocalAllowed pins safe-production-execution
// spec.md "Stored local project does not execute": a run resolving to
// `local` mode is refused unless SetLocalExecutionPolicy(true, ...) opted
// in, and any other resolved mode is always allowed. An empty project mode
// resolves against the instance default the same way modeSelectingExecutor
// does.
func TestRunService_CheckLocalAllowed(t *testing.T) {
	cases := []struct {
		name        string
		allowLocal  bool
		defaultMode string
		mode        string
		wantErr     bool
	}{
		{"explicit local without opt-in", false, "remote", "local", true},
		{"empty mode resolves to local default without opt-in", false, "local", "", true},
		{"empty mode resolves to local default (default unset) without opt-in", false, "", "", true},
		{"explicit local with opt-in", true, "remote", "local", false},
		{"empty mode resolves to local default with opt-in", true, "local", "", false},
		{"docker mode without opt-in", false, "local", "docker", false},
		{"remote mode without opt-in", false, "local", "remote", false},
		{"empty mode resolves to non-local default without opt-in", false, "remote", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewRunService(nil, nil, nil)
			s.SetLocalExecutionPolicy(tc.allowLocal, tc.defaultMode)
			err := s.CheckLocalAllowed(tc.mode)
			if tc.wantErr && err == nil {
				t.Fatal("expected an error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
