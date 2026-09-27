package runner

import (
	"context"
	"strings"
	"testing"

	"kestrix/internal/domain"
)

func TestPipelineExecutor_RejectsUnsupportedMode(t *testing.T) {
	_, err := (&PipelineExecutor{Mode: "unknown"}).Run(context.Background(), domain.Project{}, nil, domain.RunnerOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsupported runner mode") {
		t.Fatalf("error = %v", err)
	}
}

func TestPipelineExecutor_LocalRunsPipeline(t *testing.T) {
	project := domain.Project{
		Name:  "demo",
		Repos: []domain.Repo{{Name: "main", Path: "."}},
		Tasks: []domain.Task{{
			Name: "test", Trigger: "repo.main.changed",
			Execute: domain.ExecuteSpec{Repo: "main", Script: "echo ok"},
		}},
	}
	results, err := (&PipelineExecutor{Mode: "local"}).Run(context.Background(), project, map[string]bool{"main": true}, domain.RunnerOptions{WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 1 || results[0].Status != "passed" {
		t.Fatalf("results = %+v", results)
	}
}
