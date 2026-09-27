package domain

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNormalizeRunnerTags(t *testing.T) {
	got, err := NormalizeRunnerTags([]string{"GPU", " gpu", "linux", "", "arm64.v8_x-1"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"arm64.v8_x-1", "gpu", "linux"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, err := NormalizeRunnerTags(nil); err != nil || got != nil {
		t.Fatalf("nil input: got %q, %v", got, err)
	}
}

func TestNormalizeRunnerTags_Rejects(t *testing.T) {
	for _, tags := range [][]string{{"has space"}, {"slash/tag"}, {strings.Repeat("a", 65)}} {
		if _, err := NormalizeRunnerTags(tags); err == nil || !strings.Contains(err.Error(), "invalid runner tag") {
			t.Fatalf("%q: err = %v, want invalid runner tag", tags, err)
		}
	}
	many := make([]string, MaxRunnerTags+1)
	for i := range many {
		many[i] = fmt.Sprintf("t%d", i)
	}
	if _, err := NormalizeRunnerTags(many); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("33 tags: err = %v, want too many", err)
	}
	if _, err := NormalizeRunnerTags(many[:MaxRunnerTags]); err != nil {
		t.Fatalf("32 tags: %v", err)
	}
}

func TestRunnerStatus(t *testing.T) {
	now := time.Now()
	recent, stale, boundary := now.Add(-10*time.Second), now.Add(-2*time.Minute), now.Add(-time.Minute)
	for _, c := range []struct {
		r    Runner
		want string
	}{
		{Runner{Busy: true, LastSeenAt: &stale}, RunnerStatusBusy},
		{Runner{Busy: true}, RunnerStatusBusy}, // busy wins over never seen
		{Runner{}, RunnerStatusNeverSeen},
		{Runner{LastSeenAt: &recent}, RunnerStatusOnline},
		{Runner{LastSeenAt: &boundary}, RunnerStatusOnline}, // exactly offlineAfter ago is still online
		{Runner{LastSeenAt: &stale}, RunnerStatusOffline},
	} {
		if got := RunnerStatus(c.r, now, time.Minute); got != c.want {
			t.Fatalf("RunnerStatus(%+v) = %q, want %q", c.r, got, c.want)
		}
	}
}
