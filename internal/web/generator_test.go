package web

import (
	"strings"
	"testing"

	"kestrix/internal/dag"
	"kestrix/internal/domain"
)

func TestGenerateGitLabCIBasic(t *testing.T) {
	proj := domain.Project{
		Name:         "test",
		DefaultImage: "golang:1.22",
		Repos:        []domain.Repo{{Name: "a", Path: "a"}},
		Tasks: []domain.Task{
			{Name: "build", Trigger: "repo.a.changed", Execute: domain.ExecuteSpec{Repo: "a", Script: "./build.sh", Image: "golang:1.22"}},
		},
	}
	nodes := []dag.Node{
		{Task: proj.Tasks[0], Triggered: true, TriggeredReason: "test"},
	}

	yaml := GenerateGitLabCI(proj, nodes)

	if !strings.Contains(yaml, "stages:") {
		t.Error("expected stages declaration")
	}
	if !strings.Contains(yaml, "build:") {
		t.Error("expected build job")
	}
	if !strings.Contains(yaml, "image: golang:1.22") {
		t.Error("expected image declaration")
	}
	if !strings.Contains(yaml, "./build.sh") {
		t.Error("expected script in output")
	}
}

func TestGenerateGitLabCIWithDeps(t *testing.T) {
	proj := domain.Project{
		Name:  "test",
		Repos: []domain.Repo{{Name: "a", Path: "a"}},
		Tasks: []domain.Task{
			{Name: "build", Trigger: "repo.a.changed", Execute: domain.ExecuteSpec{Repo: "a"}},
			{Name: "test", Trigger: "repo.a.changed", DependsOn: []string{"build"}, Execute: domain.ExecuteSpec{Repo: "a"}},
		},
	}
	nodes := []dag.Node{
		{Task: proj.Tasks[0], Triggered: true},
		{Task: proj.Tasks[1], Triggered: true},
	}

	yaml := GenerateGitLabCI(proj, nodes)

	if !strings.Contains(yaml, "needs:") {
		t.Error("expected needs declaration")
	}
	if !strings.Contains(yaml, "- build") {
		t.Error("expected build in needs list")
	}
}

func TestGenerateGitLabCIManual(t *testing.T) {
	proj := domain.Project{
		Name:  "test",
		Repos: []domain.Repo{{Name: "a", Path: "a"}},
		Tasks: []domain.Task{
			{Name: "deploy", Trigger: "repo.a.changed", Manual: true, Execute: domain.ExecuteSpec{Repo: "a", Script: "./deploy.sh"}},
		},
	}
	nodes := []dag.Node{
		{Task: proj.Tasks[0], Triggered: true},
	}

	yaml := GenerateGitLabCI(proj, nodes)

	if !strings.Contains(yaml, "when: manual") {
		t.Error("expected 'when: manual' for manual task")
	}
}

func TestDeriveStages(t *testing.T) {
	tests := []struct {
		name     string
		nodes    []dag.Node
		expected []string
	}{
		{
			name:     "single build",
			nodes:    []dag.Node{{Task: domain.Task{Name: "build"}}},
			expected: []string{"build"},
		},
		{
			name: "build + test",
			nodes: []dag.Node{
				{Task: domain.Task{Name: "build"}},
				{Task: domain.Task{Name: "test", DependsOn: []string{"build"}}},
			},
			expected: []string{"build", "test"},
		},
		{
			name: "full pipeline",
			nodes: []dag.Node{
				{Task: domain.Task{Name: "build"}},
				{Task: domain.Task{Name: "test", DependsOn: []string{"build"}}},
				{Task: domain.Task{Name: "deploy", DependsOn: []string{"test"}, Manual: true}},
			},
			expected: []string{"build", "test", "deploy"},
		},
		{
			// Operationally a degenerate case: no tasks means there is no
			// pipeline, but generateGitLabCI still has to declare at least one
			// stage so the YAML is well-formed. deriveStages encapsulates that
			// fallback by injecting a single "build" stage.
			name:     "empty nodes falls back to build",
			nodes:    nil,
			expected: []string{"build"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stages := deriveStages(tt.nodes)
			if len(stages) != len(tt.expected) {
				t.Fatalf("stages = %v, want %v", stages, tt.expected)
			}
			for i, s := range stages {
				if s != tt.expected[i] {
					t.Errorf("stages[%d] = %q, want %q", i, s, tt.expected[i])
				}
			}
		})
	}
}

func TestAssignStage(t *testing.T) {
	tests := []struct {
		name     string
		task     domain.Task
		expected string
	}{
		{"no deps → build", domain.Task{Name: "build"}, "build"},
		{"has deps → test", domain.Task{Name: "test", DependsOn: []string{"build"}}, "test"},
		{"manual → deploy", domain.Task{Name: "deploy", Manual: true}, "deploy"},
		{"deps on test → deploy", domain.Task{Name: "deploy", DependsOn: []string{"test_integration"}}, "deploy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stage := assignStage(tt.task, nil)
			if stage != tt.expected {
				t.Errorf("stage = %q, want %q", stage, tt.expected)
			}
		})
	}
}
