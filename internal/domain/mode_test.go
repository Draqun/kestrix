package domain

import (
	"slices"
	"strings"
	"testing"
)

func TestIsValidMode(t *testing.T) {
	for _, mode := range []string{"", "local", "docker", "remote"} {
		if !IsValidMode(mode) {
			t.Fatalf("IsValidMode(%q) = false, want true", mode)
		}
	}
	for _, mode := range []string{"bogus", "Local", " docker"} {
		if IsValidMode(mode) {
			t.Fatalf("IsValidMode(%q) = true, want false", mode)
		}
	}
}

func TestModesReturnsACopy(t *testing.T) {
	got := Modes()
	if !slices.Equal(got, []string{"local", "docker", "remote"}) {
		t.Fatalf("Modes() = %v", got)
	}
	got[0] = "changed"
	if !IsValidMode("local") {
		t.Fatal("mutating the Modes() result changed the valid set")
	}
}

// TestResolveMode pins the one fallback chain every mode-aware call site
// shares: mode wins when set, instanceDefault wins when mode is empty, and
// "local" is the final fallback when both are empty.
func TestResolveMode(t *testing.T) {
	cases := []struct {
		mode, instanceDefault, want string
	}{
		{"docker", "remote", "docker"},
		{"", "remote", "remote"},
		{"", "", "local"},
		{"remote", "", "remote"},
	}
	for _, tc := range cases {
		if got := ResolveMode(tc.mode, tc.instanceDefault); got != tc.want {
			t.Errorf("ResolveMode(%q, %q) = %q, want %q", tc.mode, tc.instanceDefault, got, tc.want)
		}
	}
}

// TestLocalExecutionDisabledMessage pins that the shared explanation names
// the key-file exposure, remote, and the opt-in - see
// safe-production-execution spec.md "Local instance mode without the
// opt-in".
func TestLocalExecutionDisabledMessage(t *testing.T) {
	msg := LocalExecutionDisabledMessage()
	for _, want := range []string{"master key", "JWT signing key", "remote", "KESTRIX_RUNNER_ALLOW_LOCAL"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("LocalExecutionDisabledMessage() = %q, want it to contain %q", msg, want)
		}
	}
}
