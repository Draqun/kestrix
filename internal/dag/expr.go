// Package dag — expression parser for trigger conditions.
//
// Supports a minimal grammar sufficient for CI trigger logic:
//
//	expr   := orExpr
//	orExpr := andExpr ( "||" andExpr )*
//	andExpr:= notExpr ( "&&" notExpr )*
//	notExpr:= "!" notExpr | primary
//	primary:= "(" expr ")" | predicate
//
// predicate := "repo" "." <name> "." "changed"
//
// Tokens are identifiers, dots, parentheses, and the operators && || !.
package dag

import (
	"fmt"
	"strings"
	"unicode"
)

type tokenKind int

const (
	tkIdent tokenKind = iota
	tkDot
	tkAnd // &&
	tkOr  // ||
	tkNot // !
	tkLParen
	tkRParen
	tkEOF
)

type token struct {
	kind tokenKind
	text string
}

func tokenize(s string) ([]token, error) {
	var tokens []token
	i := 0
	for i < len(s) {
		ch := s[i]
		switch {
		case ch == ' ' || ch == '\t' || ch == '\n':
			i++
		case ch == '.':
			tokens = append(tokens, token{tkDot, "."})
			i++
		case ch == '(':
			tokens = append(tokens, token{tkLParen, "("})
			i++
		case ch == ')':
			tokens = append(tokens, token{tkRParen, ")"})
			i++
		case ch == '!' && i+1 < len(s) && s[i+1] == '=':
			return nil, fmt.Errorf("invalid token '!=' (use '!' for negation) at position %d", i)
		case ch == '!':
			tokens = append(tokens, token{tkNot, "!"})
			i++
		case ch == '&' && i+1 < len(s) && s[i+1] == '&':
			tokens = append(tokens, token{tkAnd, "&&"})
			i += 2
		case ch == '|' && i+1 < len(s) && s[i+1] == '|':
			tokens = append(tokens, token{tkOr, "||"})
			i += 2
		case isIdentStart(ch):
			start := i
			for i < len(s) && isIdentPart(s[i]) {
				i++
			}
			tokens = append(tokens, token{tkIdent, s[start:i]})
		default:
			return nil, fmt.Errorf("unexpected character %q at position %d", ch, i)
		}
	}
	tokens = append(tokens, token{tkEOF, ""})
	return tokens, nil
}

func isIdentStart(ch byte) bool {
	return unicode.IsLetter(rune(ch)) || ch == '_'
}

func isIdentPart(ch byte) bool {
	return unicode.IsLetter(rune(ch)) || unicode.IsDigit(rune(ch)) || ch == '_'
}

// exprParser holds the token stream and parsing state.
type exprParser struct {
	tokens []token
	pos    int
}

func (p *exprParser) peek() token {
	if p.pos < len(p.tokens) {
		return p.tokens[p.pos]
	}
	return token{tkEOF, ""}
}

func (p *exprParser) advance() token {
	t := p.peek()
	p.pos++
	return t
}

// parseExpr evaluates the boolean expression given the set of changed repos
// and the set of known (declared) repos. A repo not in the changed set is
// treated as "not changed" (false), not as an error.
func (p *exprParser) parseExpr(changed, knownRepos map[string]bool) (bool, error) {
	left, err := p.parseOr(changed, knownRepos)
	if err != nil {
		return false, err
	}
	return left, nil
}

func (p *exprParser) parseOr(changed, knownRepos map[string]bool) (bool, error) {
	left, err := p.parseAnd(changed, knownRepos)
	if err != nil {
		return false, err
	}
	for p.peek().kind == tkOr {
		p.advance()
		right, err := p.parseAnd(changed, knownRepos)
		if err != nil {
			return false, err
		}
		left = left || right
	}
	return left, nil
}

func (p *exprParser) parseAnd(changed, knownRepos map[string]bool) (bool, error) {
	left, err := p.parseNot(changed, knownRepos)
	if err != nil {
		return false, err
	}
	for p.peek().kind == tkAnd {
		p.advance()
		right, err := p.parseNot(changed, knownRepos)
		if err != nil {
			return false, err
		}
		left = left && right
	}
	return left, nil
}

func (p *exprParser) parseNot(changed, knownRepos map[string]bool) (bool, error) {
	if p.peek().kind == tkNot {
		p.advance()
		val, err := p.parseNot(changed, knownRepos)
		if err != nil {
			return false, err
		}
		return !val, nil
	}
	return p.parsePrimary(changed, knownRepos)
}

func (p *exprParser) parsePrimary(changed, knownRepos map[string]bool) (bool, error) {
	t := p.peek()
	switch t.kind {
	case tkLParen:
		p.advance()
		val, err := p.parseExpr(changed, knownRepos)
		if err != nil {
			return false, err
		}
		if p.peek().kind != tkRParen {
			return false, fmt.Errorf("expected ')' got %q", p.peek().text)
		}
		p.advance()
		return val, nil
	case tkIdent:
		return p.parsePredicate(changed, knownRepos)
	default:
		return false, fmt.Errorf("unexpected token %q", t.text)
	}
}

// parsePredicate handles "repo.<name>.changed"
func (p *exprParser) parsePredicate(changed, knownRepos map[string]bool) (bool, error) {
	// repo
	t := p.advance()
	if t.text != "repo" {
		return false, fmt.Errorf("expected 'repo' got %q", t.text)
	}
	// .
	if p.peek().kind != tkDot {
		return false, fmt.Errorf("expected '.' after 'repo'")
	}
	p.advance()
	// <name>
	nameTok := p.advance()
	if nameTok.kind != tkIdent {
		return false, fmt.Errorf("expected repo name after 'repo.'")
	}
	// .
	if p.peek().kind != tkDot {
		return false, fmt.Errorf("expected '.' after repo name %q", nameTok.text)
	}
	p.advance()
	// changed
	propTok := p.advance()
	if propTok.kind != tkIdent || propTok.text != "changed" {
		return false, fmt.Errorf("expected 'changed' got %q", propTok.text)
	}
	repoName := nameTok.text
	if !knownRepos[repoName] {
		return false, fmt.Errorf("unknown repo %q in trigger (not declared as ci_repo)", repoName)
	}
	// Not in the changed set → treated as "not changed" (false).
	return changed[repoName], nil
}

// String returns a human-readable label for the changed set — useful for logging.
func changedString(changed map[string]bool) string {
	var parts []string
	for name, v := range changed {
		if v {
			parts = append(parts, name)
		}
	}
	return strings.Join(parts, ", ")
}
