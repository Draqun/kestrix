package domain

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// MaxRunnerTags bounds how many tags a runner or a task may carry.
const MaxRunnerTags = 32

var runnerTagPattern = regexp.MustCompile(`^[a-z0-9._-]{1,64}$`)

// NormalizeRunnerTags trims, lowercases, deduplicates, and sorts tags, and
// rejects any that do not match [a-z0-9._-]{1,64} or more than
// MaxRunnerTags - so "GPU" and " gpu" can never silently mismatch "gpu"
// (remote-runner-management design.md, tags normalised at the boundary).
// Empty strings are dropped. A nil or empty input returns nil.
func NormalizeRunnerTags(tags []string) ([]string, error) {
	seen := make(map[string]bool, len(tags))
	var out []string
	for _, raw := range tags {
		tag := strings.ToLower(strings.TrimSpace(raw))
		if tag == "" || seen[tag] {
			continue
		}
		if !runnerTagPattern.MatchString(tag) {
			return nil, fmt.Errorf("invalid runner tag %q: must match [a-z0-9._-]{1,64}", raw)
		}
		seen[tag] = true
		out = append(out, tag)
	}
	if len(out) > MaxRunnerTags {
		return nil, fmt.Errorf("too many runner tags: %d, at most %d", len(out), MaxRunnerTags)
	}
	sort.Strings(out)
	return out, nil
}
