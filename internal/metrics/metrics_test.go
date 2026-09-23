package metrics

import (
	"strings"
	"testing"
	"time"

	"kestrix/internal/version"
)

// TestNoOp asserts every Recorder method on NoOp is callable and genuinely a
// no-op: it must never panic, and nothing about it is observable afterward,
// which is exactly the disabled-metrics guarantee (spec.md "Metrics disabled
// by default": no metric SHALL be collected or retained in memory).
func TestNoOp(t *testing.T) {
	var r Recorder = NoOp{}
	r.RunFinished("passed", "kestrix", 2*time.Second)
	r.WebhookDelivery("accepted")
	// Reaching this line without a panic is the assertion; NoOp exposes no
	// state to inspect, by design.
}

func TestRegistry_RunFinished_CountsAndDuration(t *testing.T) {
	reg := NewRegistry()
	reg.RunFinished("passed", "kestrix", 10*time.Second)
	reg.RunFinished("passed", "kestrix", 90*time.Second)
	reg.RunFinished("failed", "kestrix", 5*time.Second)

	snap := reg.Snapshot(0, 0, "test")
	if got := snap.RunTotal["passed"]["kestrix"]; got != 2 {
		t.Fatalf("passed count = %d, want 2", got)
	}
	if got := snap.RunTotal["failed"]["kestrix"]; got != 1 {
		t.Fatalf("failed count = %d, want 1", got)
	}
	if snap.DurationCountInf != 3 {
		t.Fatalf("duration count = %d, want 3", snap.DurationCountInf)
	}
	if snap.DurationSum != 105 {
		t.Fatalf("duration sum = %v, want 105", snap.DurationSum)
	}
	// 10s falls in every bucket >= 15; 90s only in buckets >= 120; 5s in
	// every bucket >= 5. Spot-check a couple of cumulative buckets.
	idx5 := indexOfBucket(t, 5)
	idx15 := indexOfBucket(t, 15)
	idx120 := indexOfBucket(t, 120)
	if snap.DurationCounts[idx5] != 1 {
		t.Fatalf("le=5 bucket = %d, want 1 (only the 5s run)", snap.DurationCounts[idx5])
	}
	if snap.DurationCounts[idx15] != 2 {
		t.Fatalf("le=15 bucket = %d, want 2 (5s and 10s runs)", snap.DurationCounts[idx15])
	}
	if snap.DurationCounts[idx120] != 3 {
		t.Fatalf("le=120 bucket = %d, want 3 (all runs)", snap.DurationCounts[idx120])
	}
}

func indexOfBucket(t *testing.T, upper float64) int {
	t.Helper()
	for i, b := range durationBucketsSeconds {
		if b == upper {
			return i
		}
	}
	t.Fatalf("no bucket boundary %v in durationBucketsSeconds", upper)
	return -1
}

// TestRegistry_RunFinished_NegativeDurationClampedToZero covers a caller
// passing a negative duration (a clock going backward, or a bug upstream):
// it must not corrupt the sum or silently panic on a negative bucket
// comparison.
func TestRegistry_RunFinished_NegativeDurationClampedToZero(t *testing.T) {
	reg := NewRegistry()
	reg.RunFinished("passed", "kestrix", -5*time.Second)

	snap := reg.Snapshot(0, 0, "test")
	if snap.DurationSum != 0 {
		t.Fatalf("duration sum = %v, want 0 (negative clamped)", snap.DurationSum)
	}
	if snap.DurationCountInf != 1 {
		t.Fatalf("duration count = %d, want 1", snap.DurationCountInf)
	}
	if snap.DurationCounts[0] != 1 {
		t.Fatalf("le=1 bucket = %d, want 1 (0s falls in every bucket)", snap.DurationCounts[0])
	}
}

func TestRegistry_WebhookDelivery(t *testing.T) {
	reg := NewRegistry()
	reg.WebhookDelivery("accepted")
	reg.WebhookDelivery("accepted")
	reg.WebhookDelivery("rejected")

	snap := reg.Snapshot(0, 0, "test")
	if snap.WebhookTotal["accepted"] != 2 {
		t.Fatalf("accepted = %d, want 2", snap.WebhookTotal["accepted"])
	}
	if snap.WebhookTotal["rejected"] != 1 {
		t.Fatalf("rejected = %d, want 1", snap.WebhookTotal["rejected"])
	}
}

// TestRegistry_ProjectLabelCap asserts the 101st distinct project collapses
// into overflowProject instead of gaining its own series - design.md D4,
// spec.md "Label cardinality is bounded".
func TestRegistry_ProjectLabelCap(t *testing.T) {
	reg := NewRegistry()
	for i := 0; i < maxProjectLabels; i++ {
		reg.RunFinished("passed", projectName(i), time.Second)
	}
	reg.RunFinished("passed", "project-101", time.Second)
	reg.RunFinished("passed", "project-102", time.Second)

	snap := reg.Snapshot(0, 0, "test")
	byProject := snap.RunTotal["passed"]
	if len(byProject) != maxProjectLabels+1 {
		t.Fatalf("distinct series = %d, want %d (cap + 1 overflow)", len(byProject), maxProjectLabels+1)
	}
	if byProject[overflowProject] != 2 {
		t.Fatalf("overflow series count = %d, want 2", byProject[overflowProject])
	}
	if _, ok := byProject["project-101"]; ok {
		t.Fatal("project-101 got its own series; want it collapsed into overflow")
	}
	if byProject[projectName(0)] != 1 {
		t.Fatalf("first project's own series = %d, want 1 (not collapsed)", byProject[projectName(0)])
	}
}

func projectName(i int) string {
	return "project-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
}

// TestRender_GoldenFixture pins the whole rendered page for a known state,
// so a future change to escaping, ordering, or HELP/TYPE lines is caught
// explicitly rather than passing on a loosened assertion - design.md's
// stated mitigation for "hand-rolled exposition is subtly wrong".
func TestRender_GoldenFixture(t *testing.T) {
	snap := Snapshot{
		RunTotal: map[string]map[string]uint64{
			"passed": {"kestrix": 2},
			"failed": {"kestrix": 1},
		},
		DurationCounts:   []uint64{0, 1, 2, 2, 3, 3, 3, 3, 3, 3, 3},
		DurationCountInf: 3,
		DurationSum:      105,
		WebhookTotal: map[string]uint64{
			"accepted": 2,
			"rejected": 1,
		},
		RunsRunning:  1,
		RunsQueued:   2,
		BuildVersion: "v1.2.3",
	}
	want := `# HELP kestrix_runs_total Total number of runs that reached each terminal status.
# TYPE kestrix_runs_total counter
kestrix_runs_total{status="failed",project="kestrix"} 1
kestrix_runs_total{status="passed",project="kestrix"} 2
# HELP kestrix_runs_running Number of runs currently executing.
# TYPE kestrix_runs_running gauge
kestrix_runs_running 1
# HELP kestrix_runs_queued Number of runs waiting for a concurrency slot.
# TYPE kestrix_runs_queued gauge
kestrix_runs_queued 2
# HELP kestrix_run_duration_seconds Duration of completed runs in seconds.
# TYPE kestrix_run_duration_seconds histogram
kestrix_run_duration_seconds_bucket{le="1"} 0
kestrix_run_duration_seconds_bucket{le="5"} 1
kestrix_run_duration_seconds_bucket{le="15"} 2
kestrix_run_duration_seconds_bucket{le="30"} 2
kestrix_run_duration_seconds_bucket{le="60"} 3
kestrix_run_duration_seconds_bucket{le="120"} 3
kestrix_run_duration_seconds_bucket{le="300"} 3
kestrix_run_duration_seconds_bucket{le="600"} 3
kestrix_run_duration_seconds_bucket{le="1200"} 3
kestrix_run_duration_seconds_bucket{le="1800"} 3
kestrix_run_duration_seconds_bucket{le="3600"} 3
kestrix_run_duration_seconds_bucket{le="+Inf"} 3
kestrix_run_duration_seconds_sum 105
kestrix_run_duration_seconds_count 3
# HELP kestrix_webhook_deliveries_total Total number of webhook deliveries by outcome.
# TYPE kestrix_webhook_deliveries_total counter
kestrix_webhook_deliveries_total{outcome="accepted"} 2
kestrix_webhook_deliveries_total{outcome="rejected"} 1
# HELP kestrix_build_info Kestrix build information. Value is always 1.
# TYPE kestrix_build_info gauge
kestrix_build_info{version="v1.2.3"} 1
`
	got := string(Render(snap))
	if got != want {
		t.Fatalf("Render output mismatch.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestEscapeLabelValue covers the three characters Prometheus exposition
// format requires escaped in a label value - design.md D1.
func TestEscapeLabelValue(t *testing.T) {
	cases := map[string]string{
		`back\slash`:   `back\\slash`,
		`quo"te`:       `quo\"te`,
		"new\nline":    `new\nline`,
		"plain":        "plain",
		`mix\"` + "\n": `mix\\\"` + `\n`,
	}
	for in, want := range cases {
		if got := escapeLabelValue(in); got != want {
			t.Errorf("escapeLabelValue(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRender_EscapesLabels asserts an escaped value actually reaches the
// rendered page, not just the helper function in isolation.
func TestRender_EscapesLabels(t *testing.T) {
	snap := Snapshot{
		RunTotal: map[string]map[string]uint64{
			"passed": {`weird"project`: 1},
		},
		DurationCounts: make([]uint64, len(durationBucketsSeconds)),
		WebhookTotal:   map[string]uint64{},
	}
	out := string(Render(snap))
	if !strings.Contains(out, `project="weird\"project"`) {
		t.Fatalf("expected escaped project label in output, got:\n%s", out)
	}
}

// TestBuildInfoMatchesVersion pins the build-info gauge to the same version
// string GET /api/version reports (internal/version.Version) - spec.md
// "Exposed metric set".
func TestBuildInfoMatchesVersion(t *testing.T) {
	snap := Snapshot{
		RunTotal:       map[string]map[string]uint64{},
		DurationCounts: make([]uint64, len(durationBucketsSeconds)),
		WebhookTotal:   map[string]uint64{},
		BuildVersion:   version.Version,
	}
	out := string(Render(snap))
	want := `kestrix_build_info{version="` + version.Version + `"} 1`
	if !strings.Contains(out, want) {
		t.Fatalf("expected %q in output, got:\n%s", want, out)
	}
}
