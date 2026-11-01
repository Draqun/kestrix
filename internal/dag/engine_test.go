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

func TestDefaultTriggerRunsAlways(t *testing.T) {
	proj := model.Project{
		Name:  "default-trigger-proj",
		Repos: []model.Repo{{Name: "auth", Path: "auth"}},
		Tasks: []model.Task{
			{Name: "build", Trigger: "", Execute: model.ExecuteSpec{Repo: "auth", Script: "make build"}},
			{Name: "deploy", Trigger: "always", DependsOn: []string{"build"}, Execute: model.ExecuteSpec{Repo: "auth", Script: "make deploy"}},
			{Name: "notify", Trigger: "true", DependsOn: []string{"deploy"}, Execute: model.ExecuteSpec{Repo: "auth", Script: "make notify"}},
		},
	}
	engine := New(proj)
	if err := engine.Validate(); err != nil {
		t.Fatalf("expected project with empty/always/true trigger to validate, got: %v", err)
	}

	nodes, err := engine.Resolve(map[string]bool{"auth": false})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	names := taskNames(nodes)
	assertContains(t, names, "build")
	assertContains(t, names, "deploy")
	assertContains(t, names, "notify")
}

func TestEvaluateTriggerWithDetails(t *testing.T) {
	e := newTestEngine()
	expr := "(repo.service_a.changed || repo.service_b.changed) && !repo.docs.changed"

	eval, err := e.EvaluateTriggerWithDetails(expr, changedSet("service_a"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !eval.Satisfied {
		t.Fatalf("expected satisfied to be true, got false")
	}
	if eval.Expression != expr {
		t.Errorf("expected expr %q, got %q", expr, eval.Expression)
	}
	if !eval.Terms["repo.service_a.changed"] {
		t.Errorf("expected repo.service_a.changed to be true")
	}
	if eval.Terms["repo.service_b.changed"] {
		t.Errorf("expected repo.service_b.changed to be false")
	}
	if eval.Terms["repo.docs.changed"] {
		t.Errorf("expected repo.docs.changed to be false")
	}

	// Docs changed blocks it
	evalDocs, err := e.EvaluateTriggerWithDetails(expr, changedSet("service_a", "docs"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evalDocs.Satisfied {
		t.Fatalf("expected satisfied to be false when docs changed")
	}
	if !evalDocs.Terms["repo.docs.changed"] {
		t.Errorf("expected repo.docs.changed to be true")
	}
}

func TestResolveWithSkipped(t *testing.T) {
	e := newTestEngine()
	triggered, skipped, err := e.ResolveWithSkipped(changedSet("service_a"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	trigNames := taskNames(triggered)
	assertContains(t, trigNames, "build_a")
	assertContains(t, trigNames, "integration_tests")
	assertContains(t, trigNames, "deploy")

	if len(skipped) != 1 {
		t.Fatalf("expected 1 skipped task, got %d", len(skipped))
	}
	st := skipped[0]
	if st.Name != "build_b" {
		t.Errorf("expected skipped task 'build_b', got %q", st.Name)
	}
	if st.Status != "skipped" {
		t.Errorf("expected status 'skipped', got %q", st.Status)
	}
	if st.SkipReason == nil {
		t.Fatalf("expected non-nil SkipReason")
	}
	if st.SkipReason.Category != "trigger_unsatisfied" {
		t.Errorf("expected category 'trigger_unsatisfied', got %q", st.SkipReason.Category)
	}
	if st.SkipReason.TriggerEvaluation == nil {
		t.Fatalf("expected non-nil TriggerEvaluation")
	}
	if st.SkipReason.TriggerEvaluation.Terms["repo.service_b.changed"] != false {
		t.Errorf("expected repo.service_b.changed term to be false")
	}
}

func TestEvaluateTriggerWithDetails_EmptyTrueAlways(t *testing.T) {
	e := newTestEngine()
	changed := changedSet("service_b", "docs", "service_a")

	for _, expr := range []string{"", "true", "always"} {
		eval, err := e.EvaluateTriggerWithDetails(expr, changed)
		if err != nil {
			t.Fatalf("unexpected error for expr %q: %v", expr, err)
		}
		if !eval.Satisfied {
			t.Errorf("expected satisfied to be true for expr %q", expr)
		}
		if len(eval.Terms) != 0 {
			t.Errorf("expected empty terms for expr %q, got %v", expr, eval.Terms)
		}
		expectedRepos := []string{"docs", "service_a", "service_b"}
		if len(eval.ChangedRepos) != len(expectedRepos) {
			t.Fatalf("expected %d changed repos, got %d", len(expectedRepos), len(eval.ChangedRepos))
		}
		for i, r := range expectedRepos {
			if eval.ChangedRepos[i] != r {
				t.Errorf("expected repo %d to be %q, got %q", i, r, eval.ChangedRepos[i])
			}
		}
	}
}

func TestEvaluateTriggerWithDetails_Errors(t *testing.T) {
	e := newTestEngine()
	changed := changedSet("service_a")

	// Tokenize error
	eval, err := e.EvaluateTriggerWithDetails("repo.service_a.changed && @bad", changed)
	if err == nil {
		t.Fatalf("expected error on tokenize failure, got nil")
	}
	if eval != nil {
		t.Errorf("expected nil eval on error, got %v", eval)
	}

	// Unknown repo
	eval, err = e.EvaluateTriggerWithDetails("repo.unknown_repo.changed", changed)
	if err == nil {
		t.Fatalf("expected error on unknown repo, got nil")
	}
	if eval != nil {
		t.Errorf("expected nil eval on error, got %v", eval)
	}

	// Syntax error
	eval, err = e.EvaluateTriggerWithDetails("repo.service_a.changed &&", changed)
	if err == nil {
		t.Fatalf("expected error on trailing operator, got nil")
	}
	if eval != nil {
		t.Errorf("expected nil eval on error, got %v", eval)
	}
}

func TestEvaluateTriggerWithDetails_CompoundAndNegation(t *testing.T) {
	e := newTestEngine()
	expr := "!repo.docs.changed && (repo.service_a.changed || repo.service_b.changed)"
	eval, err := e.EvaluateTriggerWithDetails(expr, changedSet("service_b"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !eval.Satisfied {
		t.Errorf("expected satisfied to be true")
	}
	if eval.Terms["repo.docs.changed"] {
		t.Errorf("expected repo.docs.changed to be false")
	}
	if !eval.Terms["repo.service_b.changed"] {
		t.Errorf("expected repo.service_b.changed to be true")
	}
	if eval.Terms["repo.service_a.changed"] {
		t.Errorf("expected repo.service_a.changed to be false")
	}
}

func TestResolveWithSkipped_MultipleSkippedAndCycle(t *testing.T) {
	e := newTestEngine()
	triggered, skipped, err := e.ResolveWithSkipped(changedSet())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(triggered) != 0 {
		t.Errorf("expected 0 triggered tasks, got %d", len(triggered))
	}
	if len(skipped) != 4 {
		t.Fatalf("expected 4 skipped tasks, got %d", len(skipped))
	}
	for _, st := range skipped {
		if st.Status != "skipped" {
			t.Errorf("task %s status = %q, want skipped", st.Name, st.Status)
		}
		if st.SkipReason == nil {
			t.Fatalf("task %s has nil SkipReason", st.Name)
		}
		if st.SkipReason.Category != "trigger_unsatisfied" {
			t.Errorf("task %s category = %q, want trigger_unsatisfied", st.Name, st.SkipReason.Category)
		}
		if st.SkipReason.Summary != "Trigger expression not satisfied" {
			t.Errorf("task %s summary = %q, want Trigger expression not satisfied", st.Name, st.SkipReason.Summary)
		}
	}

	// Cycle error
	cyclic := model.Project{
		Name:  "cyclic",
		Repos: []model.Repo{{Name: "r1"}},
		Tasks: []model.Task{
			{Name: "t1", DependsOn: []string{"t2"}, Trigger: "repo.r1.changed"},
			{Name: "t2", DependsOn: []string{"t1"}, Trigger: "repo.r1.changed"},
		},
	}
	cycleEngine := New(cyclic)
	_, _, err = cycleEngine.ResolveWithSkipped(changedSet("r1"))
	if err == nil {
		t.Fatalf("expected cycle error, got nil")
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
