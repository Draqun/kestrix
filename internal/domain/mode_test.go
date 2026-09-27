package domain

import (
	"slices"
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
