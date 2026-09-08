package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, 400, "bad request")

	if w.Code != 400 {
		t.Errorf("expected status 400, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["error"] != "bad request" {
		t.Errorf("expected error 'bad request', got %q", body["error"])
	}
}

func TestWriteError_500(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, http.StatusInternalServerError, "internal error")

	if w.Code != 500 {
		t.Errorf("expected status 500, got %d", w.Code)
	}
	var body map[string]string
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["error"] != "internal error" {
		t.Errorf("expected error 'internal error', got %q", body["error"])
	}
}

func TestDecodeJSON(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}

	t.Run("valid", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"x"}`))
		var v payload
		if !decodeJSON(w, r, &v) {
			t.Fatal("decodeJSON returned false for valid JSON")
		}
		if v.Name != "x" {
			t.Fatalf("Name = %q, want x", v.Name)
		}
		if w.Code != 200 {
			t.Fatalf("status = %d, want unwritten (200 default)", w.Code)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/", strings.NewReader(`not json`))
		var v payload
		if decodeJSON(w, r, &v) {
			t.Fatal("decodeJSON returned true for invalid JSON")
		}
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
	})

	t.Run("oversized", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"too long for the bound"}`))
		r.Body = http.MaxBytesReader(w, r.Body, 4)
		var v payload
		if decodeJSON(w, r, &v) {
			t.Fatal("decodeJSON returned true for an oversized body")
		}
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413", w.Code)
		}
	})
}
