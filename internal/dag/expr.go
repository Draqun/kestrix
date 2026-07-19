// Package dag — expression parser for trigger conditions.
//
// Supports a minimal grammar sufficient for CI trigger logic:
//
//	expr   := orExpr
//	orExpr := andExpr ( "||" andExpr )*
//	andExpr:= notExpr ( "&&" notExpr )*
//	notExpr:= "!" notExpr | primary
//	primary:= "(" expr ")" | predicate
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
	tkIdent    tokenKind = iota
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
		case isIdentRune(rune(ch)):
			start := i
			for i < len(s) && isIdentRune(rune(s[i])) {
				i++
			}
			tokens = append(tokens, token{tkIdent, s[start:i]})
		default:
			return nil, fmt.Errorf("unexpected char %q at position %d", ch, i)
		}
	}
	tokens = append(tokens, token{tkEOF, ""})
	return tokens, nil
}

func isIdentRune(r rune) bool {
	return r == '_' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

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

func (p *exprParser) parseExpr(changed map[string]bool) (bool, error) {
	return p.parseOr(changed)
}

func (p *exprParser) parseOr(changed map[string]bool) (bool, error) {
	left, err := p.parseAnd(changed)
	if err != nil {
		return false, err
	}
	for p.peek().kind == tkOr {
		p.advance()
		right, err := p.parseAnd(changed)
		if err != nil {
			return false, err
		}
		left = left || right
	}
	return left, nil
}

func (p *exprParser) parseAnd(changed map[string]bool) (bool, error) {
	left, err := p.parseNot(changed)
	if err != nil {
		return false, err
	}
	for p.peek().kind == tkAnd {
		p.advance()
		right, err := p.parseNot(changed)
		if err != nil {
			return false, err
		}
		left = left && right
	}
	return left, nil
}

func (p *exprParser) parseNot(changed map[string]bool) (bool, error) {
	if p.peek().kind == tkNot {
		p.advance()
		val, err := p.parseNot(changed)
		if err != nil {
			return false, err
		}
		return !val, nil
	}
	return p.parsePrimary(changed)
}

func (p *exprParser) parsePrimary(changed map[string]bool) (bool, error) {
	t := p.peek()
	switch t.kind {
	case tkLParen:
		p.advance()
		val, err := p.parseExpr(changed)
		if err != nil {
			return false, err
		}
		if p.peek().kind != tkRParen {
			return false, fmt.Errorf("expected ')' got %q", p.peek().text)
		}
		p.advance()
		return val, nil
	case tkIdent:
		return p.parsePredicate(changed)
	default:
		return false, fmt.Errorf("unexpected token %q", t.text)
	}
}

// parsePredicate handles "repo.<name>.changed"
func (p *exprParser) parsePredicate(changed map[string]bool) (bool, error) {
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
	_, exists := changed[repoName]
	if !exists {
		return false, fmt.Errorf("unknown repo %q in trigger (not declared as ci_repo)", repoName)
	}
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
