package pagination

import (
	"net/url"
	"testing"
)

func TestParse_Defaults(t *testing.T) {
	limit, offset, errStr, status := Parse(url.Values{})
	if errStr != "" {
		t.Fatalf("unexpected error: %s", errStr)
	}
	if limit != 25 || offset != 0 || status != 200 {
		t.Errorf("expected limit=25 offset=0 status=200, got %d %d %d", limit, offset, status)
	}
}

func TestParse_CustomLimit(t *testing.T) {
	q := url.Values{"limit": {"50"}, "offset": {"10"}}
	limit, offset, errStr, _ := Parse(q)
	if errStr != "" {
		t.Fatalf("unexpected error: %s", errStr)
	}
	if limit != 50 || offset != 10 {
		t.Errorf("expected limit=50 offset=10, got %d %d", limit, offset)
	}
}

func TestParse_InvalidLimit(t *testing.T) {
	q := url.Values{"limit": {"abc"}}
	_, _, errStr, status := Parse(q)
	if errStr == "" {
		t.Fatal("expected error for invalid limit")
	}
	if status != 400 {
		t.Errorf("expected 400, got %d", status)
	}
}

func TestParse_InvalidOffset(t *testing.T) {
	q := url.Values{"offset": {"xyz"}}
	_, _, errStr, status := Parse(q)
	if errStr == "" {
		t.Fatal("expected error for invalid offset")
	}
	if status != 400 {
		t.Errorf("expected 400, got %d", status)
	}
}

func TestParse_DisallowedLimit(t *testing.T) {
	q := url.Values{"limit": {"33"}}
	_, _, errStr, status := Parse(q)
	if errStr == "" {
		t.Fatal("expected error for disallowed limit")
	}
	if status != 400 {
		t.Errorf("expected 400, got %d", status)
	}
}

func TestLinks_NextAndPrev(t *testing.T) {
	next, prev := Links(25, 25, 100, "/api/items")
	if next == nil {
		t.Fatal("expected next link")
	}
	if *next != "/api/items?limit=25&offset=50" {
		t.Errorf("unexpected next: %s", *next)
	}
	if prev == nil {
		t.Fatal("expected prev link")
	}
	if *prev != "/api/items?limit=25&offset=0" {
		t.Errorf("unexpected prev: %s", *prev)
	}
}

func TestLinks_FirstPage(t *testing.T) {
	next, prev := Links(0, 25, 100, "/api/items")
	if next == nil {
		t.Fatal("expected next link")
	}
	if prev != nil {
		t.Errorf("expected no prev on first page, got %s", *prev)
	}
}

func TestLinks_LastPage(t *testing.T) {
	next, prev := Links(75, 25, 100, "/api/items")
	if next != nil {
		t.Errorf("expected no next on last page, got %s", *next)
	}
	if prev == nil {
		t.Fatal("expected prev link")
	}
}

func TestLinks_ExactlyFilled(t *testing.T) {
	next, prev := Links(0, 25, 25, "/api/items")
	if next != nil {
		t.Errorf("expected no next when exactly filled, got %s", *next)
	}
	if prev != nil {
		t.Errorf("expected no prev on first page, got %s", *prev)
	}
}
