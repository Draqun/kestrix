// Package dag implements the dependency-graph engine for Kestrix.
//
// The engine takes a parsed model.Project and a set of "changed repos" (derived
// from incoming webhook events or git diffs) and computes which tasks should
// run, in what order, and which are blocked by manual-approval gates.
package dag

import (
	"fmt"
	"strings"

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
//
// Example expressions accepted:
//
//	repo.auth_service.changed && !repo.docs.changed
//	(repo.a.changed || repo.b.changed) && !repo.docs.changed
func (e *Engine) EvaluateTrigger(expr string, changed map[string]bool) (bool, error) {
	tokens, err := tokenize(expr)
	if err != nil {
		return false, fmt.Errorf("tokenize: %w", err)
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
		return false, fmt.Errorf("evaluate trigger %q: %w", expr, err)
	}
	return result, nil
}

// Resolve computes the full execution plan. It evaluates every task's trigger
// condition and returns only the nodes that should run (triggered == true).
// Ordering via depends_on is validated for cycles.
func (e *Engine) Resolve(changed map[string]bool) ([]Node, error) {
	var nodes []Node
	for _, task := range e.Project.Tasks {
		ok, err := e.EvaluateTrigger(task.Trigger, changed)
		if err != nil {
			return nil, err
		}
		n := Node{Task: task, Triggered: ok}
		if ok {
			n.TriggeredReason = "trigger expression satisfied"
		} else {
			n.TriggeredReason = "trigger expression not satisfied"
		}
		if ok {
			nodes = append(nodes, n)
		}
	}
	// Validate dependency ordering (cycle detection).
	if err := e.detectCycles(); err != nil {
		return nil, err
	}
	return nodes, nil
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
