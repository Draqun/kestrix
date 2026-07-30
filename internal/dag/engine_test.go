package dag

import (
	"testing"

	"kestrix/internal/model"
)

func newTestEngine() *Engine {
	proj := model.Project{
		Name: "test-project",
		Repos: []model.Repo{
			{Name: "service_a", Path: "src/a", Kind: "subpath"},
			{Name: "service_b", Path: "src/b", Kind: "subpath"},
			{Name: "docs", Path: "docs", Kind: "subpath"},
			{Name: "tests", Path: "tests", Kind: "subpath"},
		},
		Tasks: []model.Task{
			{
				Name:    "build_a",
				Trigger: "repo.service_a.changed",
				Execute: model.ExecuteSpec{Repo: "service_a", Script: "./build.sh"},
			},
			{
				Name:    "build_b",
				Trigger: "repo.service_b.changed",
				Execute: model.ExecuteSpec{Repo: "service_b", Script: "./build.sh"},
			},
			{
				Name:      "integration_tests",
				Trigger:   "(repo.service_a.changed || repo.service_b.changed) && !repo.docs.changed",
				DependsOn: []string{"build_a", "build_b"},
				Execute:   model.ExecuteSpec{Repo: "tests", Script: "./run.sh"},
			},
			{
				Name:      "deploy",
				Trigger:   "repo.service_a.changed || repo.service_b.changed",
				DependsOn: []string{"integration_tests"},
				Manual:    true,
				Execute:   model.ExecuteSpec{Repo: "service_a", Script: "./deploy.sh"},
			},
		},
	}
	return New(proj)
}

func changedSet(names ...string) map[string]bool {
	ch := map[string]bool{
		"service_a": false,
		"service_b": false,
		"docs":      false,
		"tests":     false,
	}
	for _, n := range names {
		ch[n] = true
	}
	return ch
}

// --- Trigger expression tests ---

func TestSimpleTrigger(t *testing.T) {
	e := newTestEngine()
	ok, err := e.EvaluateTrigger("repo.service_a.changed", changedSet("service_a"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected trigger to fire when service_a changed")
	}
}

func TestSimpleTriggerNotChanged(t *testing.T) {
	e := newTestEngine()
	ok, err := e.EvaluateTrigger("repo.service_a.changed", changedSet("service_b"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected trigger NOT to fire when only service_b changed")
	}
}

func TestOrAndNot(t *testing.T) {
	e := newTestEngine()
	expr := "(repo.service_a.changed || repo.service_b.changed) && !repo.docs.changed"

	// a changed, no docs → true
	ok, err := e.EvaluateTrigger(expr, changedSet("service_a"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected trigger to fire: service_a changed, docs unchanged")
	}

	// b changed, no docs → true
	ok, err = e.EvaluateTrigger(expr, changedSet("service_b"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected trigger to fire: service_b changed, docs unchanged")
	}

	// a changed AND docs changed → false (docs blocks it)
	ok, err = e.EvaluateTrigger(expr, changedSet("service_a", "docs"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected trigger NOT to fire: docs changed blocks it")
	}

	// only docs changed → false
	ok, err = e.EvaluateTrigger(expr, changedSet("docs"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected trigger NOT to fire: only docs changed")
	}
}

func TestNegation(t *testing.T) {
	e := newTestEngine()
	ok, err := e.EvaluateTrigger("!repo.docs.changed", changedSet("service_a"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected !docs.changed to be true when docs didn't change")
	}

	ok, err = e.EvaluateTrigger("!repo.docs.changed", changedSet("docs"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected !docs.changed to be false when docs changed")
	}
}

// --- Resolve tests ---

func TestResolveServiceAOnly(t *testing.T) {
	e := newTestEngine()
	nodes, err := e.Resolve(changedSet("service_a"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	names := taskNames(nodes)
	assertContains(t, names, "build_a")
	assertContains(t, names, "integration_tests")
	assertContains(t, names, "deploy")
	assertNotContains(t, names, "build_b")
}

func TestResolveDocsOnly(t *testing.T) {
	e := newTestEngine()
	nodes, err := e.Resolve(changedSet("docs"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("expected 0 triggered tasks for docs-only change, got %d", len(nodes))
	}
}

func TestResolveServiceAAndDocs(t *testing.T) {
	e := newTestEngine()
	nodes, err := e.Resolve(changedSet("service_a", "docs"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	names := taskNames(nodes)
	assertContains(t, names, "build_a")
	assertContains(t, names, "deploy")
	// integration_tests should NOT be triggered (docs blocks it)
	assertNotContains(t, names, "integration_tests")
}

func TestResolveBothServices(t *testing.T) {
	e := newTestEngine()
	nodes, err := e.Resolve(changedSet("service_a", "service_b"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 4 {
		t.Fatalf("expected 4 triggered tasks, got %d", len(nodes))
	}
}

// --- Cycle detection ---

func TestCycleDetection(t *testing.T) {
	proj := model.Project{
		Name:  "cyclic",
		Repos: []model.Repo{{Name: "a", Path: "a"}},
		Tasks: []model.Task{
			{Name: "task_a", Trigger: "repo.a.changed", DependsOn: []string{"task_b"}, Execute: model.ExecuteSpec{Repo: "a"}},
			{Name: "task_b", Trigger: "repo.a.changed", DependsOn: []string{"task_a"}, Execute: model.ExecuteSpec{Repo: "a"}},
		},
	}
	_, err := New(proj).Resolve(changedSet("a"))
	if err == nil {
		t.Fatal("expected cycle detection error, got nil")
	}
}

func TestValidateUnknownRepo(t *testing.T) {
	proj := model.Project{
		Name:  "bad-ref",
		Repos: []model.Repo{{Name: "a", Path: "a"}},
		Tasks: []model.Task{
			{Name: "task_x", Trigger: "repo.a.changed", Execute: model.ExecuteSpec{Repo: "nonexistent", Script: "./x.sh"}},
		},
	}
	err := New(proj).Validate()
	if err == nil {
		t.Fatal("expected validation error for unknown repo reference")
	}
}

// --- helpers ---

func taskNames(nodes []Node) map[string]bool {
	names := make(map[string]bool)
	for _, n := range nodes {
		names[n.Task.Name] = true
	}
	return names
}

func assertContains(t *testing.T, m map[string]bool, name string) {
	t.Helper()
	if !m[name] {
		t.Errorf("expected task %q to be triggered, but it was not", name)
	}
}

func assertNotContains(t *testing.T, m map[string]bool, name string) {
	t.Helper()
	if m[name] {
		t.Errorf("expected task %q NOT to be triggered, but it was", name)
	}
}
