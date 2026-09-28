package web

import (
	"errors"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
)

// TestServer_CountLocalModeProjects pins the runner.allow_local startup
// warning's counting rule: a project's own Mode counts, an empty Mode
// resolves against the instance's runner.mode (defaulting to "local" when
// that too is empty), and a store failure propagates - see
// safe-production-execution design.md D1.
func TestServer_CountLocalModeProjects(t *testing.T) {
	cases := []struct {
		name         string
		instanceMode string
		projects     map[string]*domain.ProjectConfig
		want         int
	}{
		{
			name:         "explicit modes counted",
			instanceMode: "remote",
			projects: map[string]*domain.ProjectConfig{
				"a": {Mode: "local"},
				"b": {Mode: "docker"},
				"c": {Mode: "remote"},
			},
			want: 1,
		},
		{
			name:         "empty mode resolves to instance default",
			instanceMode: "local",
			projects: map[string]*domain.ProjectConfig{
				"a": {Mode: ""},
				"b": {Mode: "remote"},
			},
			want: 1,
		},
		{
			name:         "empty instance mode defaults to local",
			instanceMode: "",
			projects: map[string]*domain.ProjectConfig{
				"a": {Mode: ""},
			},
			want: 1,
		},
		{
			name:         "no stored projects",
			instanceMode: "remote",
			projects:     map[string]*domain.ProjectConfig{},
			want:         0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := &Server{
				cfg:          Config{Runner: RunnerConfig{Mode: tc.instanceMode}},
				projectStore: &mocks.MockProjectStore{LoadResult: tc.projects},
			}
			got, err := srv.CountLocalModeProjects()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("CountLocalModeProjects() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestServer_CountLocalModeProjectsStoreError(t *testing.T) {
	srv := &Server{
		projectStore: &mocks.MockProjectStore{LoadErr: errors.New("store unavailable")},
	}
	_, err := srv.CountLocalModeProjects()
	if err == nil {
		t.Fatal("expected an error when the store fails")
	}
}
