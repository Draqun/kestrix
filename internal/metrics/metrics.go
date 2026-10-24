// Package metrics is Kestrix's in-process Prometheus metric registry. It has
// no I/O of its own: Recorder methods only ever mutate in-memory counters,
// and Render only ever reads them - see observability design.md D1/D2. The
// package has no dependency on internal/service or internal/web, so neither
// the run scheduler nor the HTTP layer needs to depend on it to be recorded
// into; they depend on the small Recorder interface instead.
package metrics

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxProjectLabels bounds how many distinct project names ever become their
// own label series; see design.md D4. A constant, not a setting: an
// operator has no reason to want unbounded label cardinality, only a
// reason to want it capped.
const maxProjectLabels = 100

// overflowProject is the label value a project beyond maxProjectLabels
// collapses to, so cardinality stays bounded instead of a project simply
// being dropped from the metric.
const overflowProject = "__other__"

// durationBucketsSeconds are the histogram boundaries for
// kestrix_run_duration_seconds, chosen for CI run durations rather than web
// request latencies: seconds through minutes through the better part of an
// hour.
var durationBucketsSeconds = []float64{1, 5, 15, 30, 60, 120, 300, 600, 1200, 1800, 3600}

// Recorder is the write-side interface every instrumentation call site uses.
// NoOp and *Registry both implement it, so a call site never has to check
// whether metrics are enabled - see design.md D2.
type Recorder interface {
	// RunFinished records one run reaching a terminal status. project is
	// capped (see design.md D4); status is expected to be one of "passed",
	// "failed", "canceled" but is not itself validated - an unexpected value
	// still becomes its own counter series rather than being dropped, so a
	// caller bug is visible on the scrape instead of silently swallowed.
	RunFinished(status, project string, d time.Duration)
	// WebhookDelivery records one webhook delivery outcome: "accepted",
	// "rejected", or "skipped".
	WebhookDelivery(outcome string)
}

// NoOp is the Recorder used when metrics are disabled. Every method is a
// no-op, so nothing is collected or retained in memory - see observability
// spec.md "Metrics disabled by default".
type NoOp struct{}

// RunFinished and WebhookDelivery deliberately keep a named parameter and a
// trivial no-op statement rather than an empty `{}` body: an entirely empty
// method body produces no coverable statement for Go's coverage tool to
// instrument, which would leave these permanently reported as 0% covered
// regardless of how many tests call them through the Recorder interface.
func (NoOp) RunFinished(status, project string, d time.Duration) {
	_, _, _ = status, project, d
}

func (NoOp) WebhookDelivery(outcome string) {
	_ = outcome
}

// Registry is the real Recorder, and also owns rendering: Render reads the
// same state RunFinished/WebhookDelivery write, guarded by the same mutex,
// so a scrape can never observe a torn update.
type Registry struct {
	mu sync.Mutex

	// runTotal[status][project] counts runs reaching that terminal status
	// for that project (or overflowProject beyond the cap). Monotonic for
	// the life of the process; restarts at zero after a restart - see
	// spec.md "Exposed metric set".
	runTotal map[string]map[string]uint64
	// durationCounts[i] is the number of observations <= durationBucketsSeconds[i].
	// durationCountInf is the total observation count (the +Inf bucket).
	// durationSum is the sum of all observed durations in seconds.
	durationCounts   []uint64
	durationCountInf uint64
	durationSum      float64

	webhookTotal map[string]uint64

	// seenProjects tracks which project names already have their own label
	// series, so the cap (design.md D4) is enforced deterministically: the
	// first maxProjectLabels distinct projects seen keep their own series,
	// every later one collapses into overflowProject.
	seenProjects map[string]bool
}

// NewRegistry returns an empty Registry ready to record into.
func NewRegistry() *Registry {
	return &Registry{
		runTotal:       make(map[string]map[string]uint64),
		durationCounts: make([]uint64, len(durationBucketsSeconds)),
		webhookTotal:   make(map[string]uint64),
		seenProjects:   make(map[string]bool),
	}
}

// projectLabel resolves project to itself if it already has a series or the
// cap has not been reached yet, or to overflowProject once maxProjectLabels
// distinct projects have already been seen. Caller must hold r.mu.
func (r *Registry) projectLabel(project string) string {
	if r.seenProjects[project] {
		return project
	}
	if len(r.seenProjects) >= maxProjectLabels {
		return overflowProject
	}
	r.seenProjects[project] = true
	return project
}

func (r *Registry) RunFinished(status, project string, d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	label := r.projectLabel(project)
	byProject, ok := r.runTotal[status]
	if !ok {
		byProject = make(map[string]uint64)
		r.runTotal[status] = byProject
	}
	byProject[label]++

	seconds := d.Seconds()
	if seconds < 0 {
		seconds = 0
	}
	r.durationSum += seconds
	r.durationCountInf++
	for i, upper := range durationBucketsSeconds {
		if seconds <= upper {
			r.durationCounts[i]++
		}
	}
}

func (r *Registry) WebhookDelivery(outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.webhookTotal[outcome]++
}

// Snapshot is a point-in-time, render-ready copy of a Registry plus the
// gauges and build info that live outside it (design.md D3: running/queued
// are read from the scheduler on scrape, not tracked here). Render takes one
// so the rendering format is exercised by a pure function independent of a
// live Registry - see design.md's golden-fixture test (task 3.2).
type Snapshot struct {
	RunTotal            map[string]map[string]uint64
	DurationCounts      []uint64
	DurationCountInf    uint64
	DurationSum         float64
	WebhookTotal        map[string]uint64
	RunsRunning         int
	RunsQueued          int
	RemoteJobsUnmatched int
	BuildVersion        string
}

// Snapshot copies the Registry's current state plus the supplied gauges and
// build version into a Snapshot ready for Render.
func (r *Registry) Snapshot(running, queued, remoteJobsUnmatched int, buildVersion string) Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	runTotal := make(map[string]map[string]uint64, len(r.runTotal))
	for status, byProject := range r.runTotal {
		copied := make(map[string]uint64, len(byProject))
		for project, n := range byProject {
			copied[project] = n
		}
		runTotal[status] = copied
	}
	webhookTotal := make(map[string]uint64, len(r.webhookTotal))
	for outcome, n := range r.webhookTotal {
		webhookTotal[outcome] = n
	}
	return Snapshot{
		RunTotal:            runTotal,
		DurationCounts:      append([]uint64(nil), r.durationCounts...),
		DurationCountInf:    r.durationCountInf,
		DurationSum:         r.durationSum,
		WebhookTotal:        webhookTotal,
		RunsRunning:         running,
		RunsQueued:          queued,
		RemoteJobsUnmatched: remoteJobsUnmatched,
		BuildVersion:        buildVersion,
	}
}

// escapeLabelValue escapes a label value for Prometheus text exposition
// format 0.0.4: backslash, double quote, and newline each need their own
// escape - see design.md D1 ("our problem").
func escapeLabelValue(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	return v
}

// sortedKeys returns m's keys sorted, so Render's output is deterministic
// across calls - required for the golden-fixture test and considerate to a
// human reading a scrape by hand.
func sortedKeys(m map[string]uint64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Render writes s in Prometheus text exposition format 0.0.4. It never
// returns an error: every value rendered is already a Go number or an
// escaped string, so there is nothing for a caller to handle beyond writing
// the bytes to the response.
func Render(s Snapshot) []byte {
	var b strings.Builder

	b.WriteString("# HELP kestrix_runs_total Total number of runs that reached each terminal status.\n")
	b.WriteString("# TYPE kestrix_runs_total counter\n")
	statuses := make([]string, 0, len(s.RunTotal))
	for status := range s.RunTotal {
		statuses = append(statuses, status)
	}
	sort.Strings(statuses)
	for _, status := range statuses {
		byProject := s.RunTotal[status]
		for _, project := range sortedKeys(byProject) {
			fmt.Fprintf(&b, "kestrix_runs_total{status=\"%s\",project=\"%s\"} %d\n",
				escapeLabelValue(status), escapeLabelValue(project), byProject[project])
		}
	}

	b.WriteString("# HELP kestrix_runs_running Number of runs currently executing.\n")
	b.WriteString("# TYPE kestrix_runs_running gauge\n")
	fmt.Fprintf(&b, "kestrix_runs_running %d\n", s.RunsRunning)

	b.WriteString("# HELP kestrix_runs_queued Number of runs waiting for a concurrency slot.\n")
	b.WriteString("# TYPE kestrix_runs_queued gauge\n")
	fmt.Fprintf(&b, "kestrix_runs_queued %d\n", s.RunsQueued)

	b.WriteString("# HELP kestrix_run_duration_seconds Duration of completed runs in seconds.\n")
	b.WriteString("# TYPE kestrix_run_duration_seconds histogram\n")
	for i, upper := range durationBucketsSeconds {
		fmt.Fprintf(&b, "kestrix_run_duration_seconds_bucket{le=\"%s\"} %d\n", formatFloat(upper), s.DurationCounts[i])
	}
	fmt.Fprintf(&b, "kestrix_run_duration_seconds_bucket{le=\"+Inf\"} %d\n", s.DurationCountInf)
	fmt.Fprintf(&b, "kestrix_run_duration_seconds_sum %s\n", formatFloat(s.DurationSum))
	fmt.Fprintf(&b, "kestrix_run_duration_seconds_count %d\n", s.DurationCountInf)

	b.WriteString("# HELP kestrix_webhook_deliveries_total Total number of webhook deliveries by outcome.\n")
	b.WriteString("# TYPE kestrix_webhook_deliveries_total counter\n")
	for _, outcome := range sortedKeys(s.WebhookTotal) {
		fmt.Fprintf(&b, "kestrix_webhook_deliveries_total{outcome=\"%s\"} %d\n", escapeLabelValue(outcome), s.WebhookTotal[outcome])
	}

	b.WriteString("# HELP kestrix_remote_jobs_unmatched Number of pending remote jobs that no registered runner can take.\n")
	b.WriteString("# TYPE kestrix_remote_jobs_unmatched gauge\n")
	fmt.Fprintf(&b, "kestrix_remote_jobs_unmatched %d\n", s.RemoteJobsUnmatched)

	b.WriteString("# HELP kestrix_build_info Kestrix build information. Value is always 1.\n")
	b.WriteString("# TYPE kestrix_build_info gauge\n")
	fmt.Fprintf(&b, "kestrix_build_info{version=\"%s\"} 1\n", escapeLabelValue(s.BuildVersion))

	return []byte(b.String())
}

// formatFloat renders f in the shortest decimal form that round-trips back
// to the same float64, matching how Prometheus client libraries format
// exposition values - so "100" renders as "100", not "100.000000". Used for
// both the histogram sum and its bucket boundaries: every entry in
// durationBucketsSeconds today is integral, so a boundary renders without a
// trailing ".0", but this stays correct if a fractional boundary is added
// later.
func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
