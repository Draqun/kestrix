package domain

import (
	"fmt"
	"path/filepath"
	"strings"
)

// MaxTaskArtifacts bounds how many artifact paths a single task may declare.
const MaxTaskArtifacts = 32

// ValidateArtifacts validates a task's declared artifact paths per design D1:
// - at most 32 entries;
// - no empty path;
// - each path must be relative (not absolute);
// - each path must not contain a ".." component.
func ValidateArtifacts(taskName string, artifacts []string) error {
	if len(artifacts) > MaxTaskArtifacts {
		return fmt.Errorf("task %q: artifacts: at most %d entries allowed, got %d", taskName, MaxTaskArtifacts, len(artifacts))
	}
	for _, p := range artifacts {
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("task %q: artifact path cannot be empty", taskName)
		}
		if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") {
			return fmt.Errorf("task %q: artifact %q: path must be relative", taskName, p)
		}
		cleanSlash := filepath.ToSlash(p)
		for _, part := range strings.Split(cleanSlash, "/") {
			if part == ".." {
				return fmt.Errorf("task %q: artifact %q: path must not contain '..' components", taskName, p)
			}
		}
	}
	return nil
}
