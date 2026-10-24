package service

import (
	"bytes"
	"fmt"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/model"
)

func TestOrderedPassedAncestors_NoAncestors(t *testing.T) {
	proj := model.Project{
		Tasks: []model.Task{
			{Name: "root"},
		},
	}
	res := orderedPassedAncestors(proj, proj.Tasks[0], map[string]bool{})
	if len(res) != 0 {
		t.Fatalf("expected nil or empty, got %v", res)
	}
}

func TestOrderedPassedAncestors_Chain(t *testing.T) {
	proj := model.Project{
		Tasks: []model.Task{
			{Name: "a"},
			{Name: "b", DependsOn: []string{"a"}},
			{Name: "c", DependsOn: []string{"b"}},
		},
	}
	passed := map[string]bool{"a": true, "b": true}
	res := orderedPassedAncestors(proj, proj.Tasks[2], passed)
	if len(res) != 2 || res[0] != "a" || res[1] != "b" {
		t.Fatalf("res = %v, want [a, b]", res)
	}
}

func TestOrderedPassedAncestors_Diamond(t *testing.T) {
	// a declared before b
	projAB := model.Project{
		Tasks: []model.Task{
			{Name: "a"},
			{Name: "b"},
			{Name: "c", DependsOn: []string{"b", "a"}},
		},
	}
	passed := map[string]bool{"a": true, "b": true}
	resAB := orderedPassedAncestors(projAB, projAB.Tasks[2], passed)
	if len(resAB) != 2 || resAB[0] != "a" || resAB[1] != "b" {
		t.Fatalf("resAB = %v, want [a, b]", resAB)
	}

	// b declared before a
	projBA := model.Project{
		Tasks: []model.Task{
			{Name: "b"},
			{Name: "a"},
			{Name: "c", DependsOn: []string{"a", "b"}},
		},
	}
	resBA := orderedPassedAncestors(projBA, projBA.Tasks[2], passed)
	if len(resBA) != 2 || resBA[0] != "b" || resBA[1] != "a" {
		t.Fatalf("resBA = %v, want [b, a]", resBA)
	}
}

func TestOrderedPassedAncestors_UnknownDependency(t *testing.T) {
	proj := model.Project{
		Tasks: []model.Task{
			{Name: "a", DependsOn: []string{"nonexistent"}},
		},
	}
	passed := map[string]bool{"nonexistent": true}
	res := orderedPassedAncestors(proj, proj.Tasks[0], passed)
	if len(res) != 0 {
		t.Fatalf("expected empty, got %v", res)
	}
}

func TestBuildRemoteJobPayload_WithArtifacts(t *testing.T) {
	task := model.Task{Name: "deploy", Execute: domain.ExecuteSpec{Repo: "app"}}
	artA := []byte("artifact-a")
	artB := []byte("artifact-b")

	payload := buildRemoteJobPayload(task, nil, "", nil, nil, nil, [][]byte{artA, artB})
	if len(payload.Artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(payload.Artifacts))
	}
	if !bytes.Equal(payload.Artifacts[0], artA) || !bytes.Equal(payload.Artifacts[1], artB) {
		t.Fatalf("unexpected artifacts in payload: %v", payload.Artifacts)
	}
}

func TestResumeRemoteRun_CarriesAncestorsArtifacts(t *testing.T) {
	proj := model.Project{
		Repos: []domain.Repo{{Name: "app", Path: "app"}},
		Tasks: []model.Task{
			{Name: "build", Trigger: "repo.app.changed"},
			{Name: "deploy", Trigger: "repo.app.changed", DependsOn: []string{"build"}},
		},
	}

	artBuild := []byte("build-tar-gz")
	remoteJobs := &mocks.MockRemoteJobStore{
		Artifacts: map[string][]byte{
			"1:build": artBuild,
		},
	}
	runStore := &mocks.MockRunStore{
		GetRunResult: &domain.Run{
			ID:          1,
			ProjectName: "app",
			Status:      "running",
			Changed:     []string{"app"},
			PipelineDef: &proj,
			TaskResults: []domain.TaskResult{
				{Name: "build", Status: "passed"},
			},
		},
	}

	pipeline := &mocks.MockPipelineRunner{}
	svc := NewRunService(runStore, pipeline, pipeline)
	svc.SetRemoteJobStore(remoteJobs)

	// Persist snapshot so resumeRemoteRun does not think it's a single-task re-run
	if err := remoteJobs.SetRemoteRunSnapshot(1, []byte("source-archive"), nil, nil); err != nil {
		t.Fatal(err)
	}

	decided, err := svc.resumeRemoteRun(1)
	if err != nil || !decided {
		t.Fatalf("ResumeRemoteRun: decided=%v err=%v", decided, err)
	}

	if len(remoteJobs.CreateRemoteJobCalls) != 1 {
		t.Fatalf("expected 1 job dispatched, got %d", len(remoteJobs.CreateRemoteJobCalls))
	}
	call := remoteJobs.CreateRemoteJobCalls[0]
	if call.TaskName != "deploy" {
		t.Fatalf("dispatched task = %q, want deploy", call.TaskName)
	}
	if len(call.Payload.Artifacts) != 1 || !bytes.Equal(call.Payload.Artifacts[0], artBuild) {
		t.Fatalf("expected deploy payload to carry build artifact, got %v", call.Payload.Artifacts)
	}
}

func TestResumeRemoteRun_ListArtifactsErrorSurfaces(t *testing.T) {
	proj := model.Project{
		Repos: []domain.Repo{{Name: "app", Path: "app"}},
		Tasks: []model.Task{
			{Name: "build", Trigger: "repo.app.changed"},
			{Name: "deploy", Trigger: "repo.app.changed", DependsOn: []string{"build"}},
		},
	}

	remoteJobs := &mocks.MockRemoteJobStore{
		ListArtifactsErr: fmt.Errorf("list artifacts failed"),
	}
	runStore := &mocks.MockRunStore{
		GetRunResult: &domain.Run{
			ID:          1,
			ProjectName: "app",
			Status:      "running",
			Changed:     []string{"app"},
			PipelineDef: &proj,
			TaskResults: []domain.TaskResult{
				{Name: "build", Status: "passed"},
			},
		},
	}

	pipeline := &mocks.MockPipelineRunner{}
	svc := NewRunService(runStore, pipeline, pipeline)
	svc.SetRemoteJobStore(remoteJobs)

	if err := remoteJobs.SetRemoteRunSnapshot(1, []byte("source-archive"), nil, nil); err != nil {
		t.Fatal(err)
	}

	decided, err := svc.resumeRemoteRun(1)
	if decided || err == nil {
		t.Fatalf("expected decided=false, err != nil; got decided=%v err=%v", decided, err)
	}
}
