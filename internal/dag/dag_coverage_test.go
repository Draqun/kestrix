// Coverage retrofit tests for internal/dag.
// Goal: drive expr.go and engine.go to 100% statement coverage on
// the same branch model the parser already uses.
//
// Each test target corresponds to an uncovered branch identified from
// `go tool cover -func=coverage.out`. Adding tests here MUST NOT alter
// production behaviour — if a test fails because the parser rejected
// input we expected to accept, the parser is the source of truth.
package dag

import (
	"strings"
	"testing"

	"kestrix/internal/model"
)

// --- parsePrimary / parsePredicate error paths -----------------------------

// badPredEngine returns an engine with one declared repo ("good") so it is
// possible to drive predicate parsing without Validate exploding first.
func badPredEngine() *Engine {
	return New(model.Project{
		Name:  "bad-pred",
		Repos: []model.Repo{{Name: "good", Path: "g"}},
		Tasks: []model.Task{
			{Name: "t", Trigger: "repo.good.changed",
				Execute: model.ExecuteSpec{Repo: "good", Script: "./x.sh"}},
		},
	})
}

func TestPredicate_ErrorPaths(t *testing.T) {
	e := badPredEngine()
	cases := []struct {
		name string
		expr string
		want string
	}{
		{"missing_repo_keyword", "service.good.changed", `expected 'repo'`},
		{"missing_dot_after_repo", "repo changed", `expected '.' after 'repo'`},
		{"missing_repo_name", "repo..changed", `expected repo name after 'repo.'`},
		{"missing_dot_after_name", "repo.good changed", `expected '.' after repo name`},
		{"bad_property_name", "repo.good.modified", `expected 'changed'`},
		{"unknown_repo", "repo.ghost.changed", `unknown repo`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.EvaluateTrigger(tc.expr, map[string]bool{"good": true})
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %q", tc.want, err.Error())
			}
		})
	}
}

func TestPrimary_UnexpectedToken(t *testing.T) {
	// An expression starting with a leading operator hits the
	// default arm of parsePrimary (tkAnd would be the simplest case).
	e := badPredEngine()
	_, err := e.EvaluateTrigger("&& repo.good.changed", map[string]bool{"good": true})
	if err == nil {
		t.Fatal("expected error for unexpected leading token")
	}
	if !strings.Contains(err.Error(), "unexpected token") {
		t.Fatalf("expected 'unexpected token', got %q", err.Error())
	}
}

func TestParen_MissingClose(t *testing.T) {
	e := badPredEngine()
	// open paren with no closing one
	_, err := e.EvaluateTrigger("(repo.good.changed", map[string]bool{"good": true})
	if err == nil {
		t.Fatal("expected error for missing close paren")
	}
	if !strings.Contains(err.Error(), "expected ')'") {
		t.Fatalf("expected missing-rparen message, got %q", err.Error())
	}
}

// --- tokenize error paths --------------------------------------------------

func TestTokenize_NotEqual(t *testing.T) {
	e := badPredEngine()
	// "!=" is forbidden — must trip tokenize before parse
	_, err := e.EvaluateTrigger("!= repo.good.changed", map[string]bool{"good": true})
	if err == nil {
		t.Fatal("expected error for invalid '!='")
	}
	if !strings.Contains(err.Error(), "invalid token '!='") {
		t.Fatalf("expected !-message, got %q", err.Error())
	}
}

func TestTokenize_UnexpectedChar(t *testing.T) {
	e := badPredEngine()
	// "@" is not part of the grammar — should error
	_, err := e.EvaluateTrigger("repo.good@changed", map[string]bool{"good": true})
	if err == nil {
		t.Fatal("expected error for unexpected character @")
	}
	if !strings.Contains(err.Error(), "unexpected character") {
		t.Fatalf("expected 'unexpected character', got %q", err.Error())
	}
}

// --- peek EOF fallback -----------------------------------------------------

// exprParser.peek() has two arms: in-bounds and EOF-fallback (pos >= len).
// The in-bounds arm is exhausted whenever pos points past the final
// tkEOF token. To exercise that branch without changing production
// code we construct an exprParser whose token slice is empty and call
// peek directly. This is safe because peek is a pure read.
func TestExprParser_PeekEOFFallback(t *testing.T) {
	p := &exprParser{tokens: nil}
	got := p.peek()
	if got.kind != tkEOF {
		t.Fatalf("expected tkEOF, got %v", got)
	}
	if got.text != "" {
		t.Fatalf("expected empty text, got %q", got.text)
	}
}

// Sanity coverage: a happy-path EvaluateTrigger consumes tkEOF during
// the trailing while-loop probe in parseOr/parseAnd.
func TestEvaluateTrigger_StopsAtEOF(t *testing.T) {
	e := badPredEngine()
	_, err := e.EvaluateTrigger(
		"repo.good.changed || repo.good.changed",
		map[string]bool{"good": true},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Engine-level error paths ---------------------------------------------

// projWith returns an engine whose tasks carry the supplied triggers.
// Used to drive EvaluateTrigger error paths inside Resolve / Validate.
func projWith(triggers ...string) *Engine {
	tasks := make([]model.Task, len(triggers))
	for i, trig := range triggers {
		tasks[i] = model.Task{
			Name:    "t" + string(rune('a'+i)),
			Trigger: trig,
			Execute: model.ExecuteSpec{Repo: "good", Script: "./x.sh"},
		}
	}
	return New(model.Project{
		Name:  "engine-errors",
		Repos: []model.Repo{{Name: "good", Path: "g"}},
		Tasks: tasks,
	})
}

func TestResolve_TriggerParseError(t *testing.T) {
	// First task parses fine, second task has an invalid trigger
	// -> Resolve should bubble up the error from EvaluateTrigger.
	e := projWith("repo.good.changed", "not_a_repo_predicate")
	_, err := e.Resolve(map[string]bool{"good": true})
	if err == nil {
		t.Fatal("expected error from Resolve due to invalid trigger")
	}
}

func TestValidate_TriggerSyntaxError(t *testing.T) {
	// Validate must surface bad trigger syntax (not just swallows it).
	e := projWith("not_a_repo_predicate")
	err := e.Validate()
	if err == nil {
		t.Fatal("expected Validate to report trigger error")
	}
	if !strings.Contains(err.Error(), "not_a_repo_predicate") &&
		!strings.Contains(err.Error(), "expected") {
		t.Fatalf("expected parser error message, got %q", err.Error())
	}
}

func TestValidate_DetectCyclesError(t *testing.T) {
	// Two tasks depending on each other -> detectCycles must report.
	e := New(model.Project{
		Name:  "cycle-on-validate",
		Repos: []model.Repo{{Name: "g", Path: "g"}},
		Tasks: []model.Task{
			{
				Name:      "a",
				Trigger:   "repo.g.changed",
				DependsOn: []string{"b"},
				Execute:   model.ExecuteSpec{Repo: "g", Script: "./x.sh"},
			},
			{
				Name:      "b",
				Trigger:   "repo.g.changed",
				DependsOn: []string{"a"},
				Execute:   model.ExecuteSpec{Repo: "g", Script: "./x.sh"},
			},
		},
	})
	err := e.Validate()
	if err == nil {
		t.Fatal("expected cycle detection error from Validate")
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Fatalf("expected 'cycle detected', got %q", err.Error())
	}
}

func TestDetectCycles_UnknownDependency(t *testing.T) {
	// Tasks list contains a DependsOn referencing a non-existent task.
	// detectCycles walks every node, and on the first unknown dep it
	// returns the "unknown dependency" branch.
	e := New(model.Project{
		Name:  "unknown-dep",
		Repos: []model.Repo{{Name: "g", Path: "g"}},
		Tasks: []model.Task{
			{
				Name:      "real",
				Trigger:   "repo.g.changed",
				DependsOn: []string{"ghost"},
				Execute:   model.ExecuteSpec{Repo: "g", Script: "./x.sh"},
			},
		},
	})
	_, err := e.Resolve(map[string]bool{"g": true})
	if err == nil {
		t.Fatal("expected unknown-dependency error")
	}
	if !strings.Contains(err.Error(), "unknown dependency") {
		t.Fatalf("expected 'unknown dependency', got %q", err.Error())
	}
}

// --- continuation error paths in the recursive parser --------------------
//
// parseOr / parseAnd / parseNot / parsePrimary each have a "the rhs
// of an operator exploded" error path. Drive each one with an
// expression where a top-level conjunction succeeds but its right
// operand trips a parse error AFTER the operator has been consumed.

func TestContinuation_Errors(t *testing.T) {
	e := badPredEngine()
	cases := []struct {
		name string
		expr string
	}{
		// "repo.good.changed || ..." continuation: rhs is `&&garbage`
		// (operator at start of rhs → parsePrimary default → err bubbles
		// up through parseAnd → parseOr's continuation err path).
		{"or_rhs_parse_error", "repo.good.changed || &&garbage"},

		// "repo.good.changed && ..." continuation: rhs starts with
		// another operator → parseAnd rhs loop hits parseNot →
		// parsePrimary default arm → err bubbles through parseAnd.
		{"and_rhs_parse_error", "repo.good.changed && &&garbage"},

		// "!" followed by an operator — parseNot consumes `!`, recurses,
		// hits parsePrimary's default (and-operator as primary) → err
		// bubbles through parseNot via the inner err path. This is
		// distinct from the simple "bad token" handled by TestPrimary.
		{"not_rhs_parse_error", "! &&garbage"},

		// "( <BAD> )" — parsePrimary sees `(`, advances, recurses into
		// parseExpr → parseOr → parseAnd → parsePrimary default arm
		// → err bubbles through parsePrimary back to parseExpr.
		{"parens_subexpr_error", "(&&garbage)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// `&&garbage` is a token mix: `&&` consumes successfully but
			// `garbage` is not a valid predicate. Some trigger-parsers
			// bail at the token level (we previously confirmed), so we
			// expect EvaluateTrigger to report SOME error. The point of
			// these cases is purely to keep the continuation branches
			// in the parser reachable.
			//
			// NOTE: `&&garbage` actually fails during tokenize because
			// `&&garbage` begins with `&&` followed by an identifier. The
			// tokenizer is happy (it returns tokens for `&&` then
			// `garbage`), and only parseAnd->parsePrimary's default arm
			// errors on the unexpected `&&`. So evaluate must return a
			// parser error — not a tokenizer error.
			_, err := e.EvaluateTrigger(tc.expr, map[string]bool{"good": true})
			if err == nil {
				t.Fatalf("expected error from %q, got nil", tc.expr)
			}
		})
	}
}
