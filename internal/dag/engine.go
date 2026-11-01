// Package dag implements the dependency-graph engine for Kestrix.
//
// The engine takes a parsed model.Project and a set of "changed repos" (derived
// from incoming webhook events or git diffs) and computes which tasks should
// run, in what order, and which are blocked by manual-approval gates.
package dag

import (
	"fmt"
	"sort"
	"strings"

	"kestrix/internal/domain"
	"kestrix/internal/model"
)

// Node is a single task in the resolved DAG, annotated with whether its trigger
// condition is satisfied and whether it requires manual approval.
type Node struct {
	Task            model.Task
	Triggered       bool
	TriggeredReason string
}

// Engine holds the parsed project and provides operations to evaluate triggers
// and produce an execution plan.
type Engine struct {
	Project model.Project
}

// New creates a DAG engine from a parsed project.
func New(p model.Project) *Engine {
	return &Engine{Project: p}
}

// EvaluateTrigger computes whether a task's trigger expression is satisfied given
// the set of changed repo names. It parses the boolean expression supporting
// AND/OR/NOT and the "repo.<name>.changed" predicate.
// An empty trigger expression, "true", or "always" evaluates to true (always runs).
//
// Example expressions accepted:
//
//	repo.auth_service.changed && !repo.docs.changed
//	(repo.a.changed || repo.b.changed) && !repo.docs.changed
func (e *Engine) EvaluateTrigger(expr string, changed map[string]bool) (bool, error) {
	eval, err := e.EvaluateTriggerWithDetails(expr, changed)
	if err != nil {
		return false, err
	}
	return eval.Satisfied, nil
}

// EvaluateTriggerWithDetails evaluates a trigger expression and returns a structured
// TriggerEvaluation breakdown containing expression, changed repos, resolved terms,
// and final satisfied boolean.
func (e *Engine) EvaluateTriggerWithDetails(expr string, changed map[string]bool) (*domain.TriggerEvaluation, error) {
	expr = strings.TrimSpace(expr)
	var changedRepos []string
	for r, ok := range changed {
		if ok {
			changedRepos = append(changedRepos, r)
		}
	}
	sort.Strings(changedRepos)

	if expr == "" || expr == "true" || expr == "always" {
		return &domain.TriggerEvaluation{
			Expression:   expr,
			ChangedRepos: changedRepos,
			Terms:        map[string]bool{},
			Satisfied:    true,
		}, nil
	}
	tokens, err := tokenize(expr)
	if err != nil {
		return nil, fmt.Errorf("tokenize: %w", err)
	}
	// Build the set of declared repos so triggers can reference any ci_repo,
	// not only those present in the changed set.
	knownRepos := make(map[string]bool)
	for _, r := range e.Project.Repos {
		knownRepos[r.Name] = true
	}
	parser := &exprParser{tokens: tokens}
	result, err := parser.parseExpr(changed, knownRepos)
	if err != nil {
		return nil, fmt.Errorf("evaluate trigger %q: %w", expr, err)
	}
	terms := extractTerms(tokens, changed)
	return &domain.TriggerEvaluation{
		Expression:   expr,
		ChangedRepos: changedRepos,
		Terms:        terms,
		Satisfied:    result,
	}, nil
}

func extractTerms(tokens []token, changed map[string]bool) map[string]bool {
	terms := make(map[string]bool)
	for i := 0; i+4 < len(tokens); i++ {
		if tokens[i].text == "repo" && tokens[i+1].kind == tkDot &&
			tokens[i+2].kind == tkIdent && tokens[i+3].kind == tkDot &&
			tokens[i+4].text == "changed" {
			repo := tokens[i+2].text
			term := fmt.Sprintf("repo.%s.changed", repo)
			terms[term] = changed[repo]
		}
	}
	return terms
}

// ResolveWithSkipped evaluates every task's trigger condition and partitions tasks
// into triggered execution nodes and skipped tasks with detailed skip reasons.
func (e *Engine) ResolveWithSkipped(changed map[string]bool) ([]Node, []domain.TaskResult, error) {
	var triggered []Node
	var skipped []domain.TaskResult
	for _, task := range e.Project.Tasks {
		eval, err := e.EvaluateTriggerWithDetails(task.Trigger, changed)
		if err != nil {
			return nil, nil, err
		}
		if eval.Satisfied {
			triggered = append(triggered, Node{
				Task:            task,
				Triggered:       true,
				TriggeredReason: "trigger expression satisfied",
			})
		} else {
			skipped = append(skipped, domain.NewTriggerUnsatisfiedSkipResult(task.Name, eval))
		}
	}
	// Validate dependency ordering (cycle detection).
	if err := e.detectCycles(); err != nil {
		return nil, nil, err
	}
	return triggered, skipped, nil
}

// Resolve computes the full execution plan. It evaluates every task's trigger
// condition and returns only the nodes that should run (triggered == true).
// Ordering via depends_on is validated for cycles.
func (e *Engine) Resolve(changed map[string]bool) ([]Node, error) {
	triggered, _, err := e.ResolveWithSkipped(changed)
	return triggered, err
}

// Validate checks the project structure: trigger syntax and DAG cycles.
// It does not require a changed-set; it validates the configuration itself.
func (e *Engine) Validate() error {
	// Syntax-check all trigger expressions with an empty changed set.
	empty := make(map[string]bool)
	for _, r := range e.Project.Repos {
		empty[r.Name] = false
	}
	for _, t := range e.Project.Tasks {
		if _, err := e.EvaluateTrigger(t.Trigger, empty); err != nil {
			return fmt.Errorf("task %q: %w", t.Name, err)
		}
	}
	// Check for cross-repo execution references: every execute.repo must
	// resolve to a declared ci_repo.
	repoNames := make(map[string]bool)
	for _, r := range e.Project.Repos {
		repoNames[r.Name] = true
	}
	for _, t := range e.Project.Tasks {
		if !repoNames[t.Execute.Repo] {
			return fmt.Errorf("task %q: execute.repo %q is not a declared ci_repo", t.Name, t.Execute.Repo)
		}
	}
	return e.detectCycles()
}

// detectCycles performs a depth-first traversal of depends_on edges and returns an
// error if a cycle is found.
func (e *Engine) detectCycles() error {
	adj := make(map[string][]string)
	taskNames := make(map[string]bool)
	for _, t := range e.Project.Tasks {
		taskNames[t.Name] = true
		adj[t.Name] = t.DependsOn
	}
	visited := make(map[string]int) // 0=unvisited,1=in-stack,2=done
	var dfs func(name string, path []string) error
	dfs = func(name string, path []string) error {
		if visited[name] == 1 {
			return fmt.Errorf("cycle detected: %s -> %s", strings.Join(path, " -> "), name)
		}
		if visited[name] == 2 {
			return nil
		}
		if !taskNames[name] {
			return fmt.Errorf("unknown dependency: %q", name)
		}
		visited[name] = 1
		for _, dep := range adj[name] {
			if err := dfs(dep, append(path, name)); err != nil {
				return err
			}
		}
		visited[name] = 2
		return nil
	}
	for name := range taskNames {
		if visited[name] == 0 {
			if err := dfs(name, nil); err != nil {
				return err
			}
		}
	}
	return nil
}
