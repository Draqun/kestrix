package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"kestrix/internal/store"
	"kestrix/internal/web/pagination"
)

func seedProjects(t *testing.T, path string, names []string) {
	t.Helper()
	storePath := filepath.Join(path, "projects.json")
	st, err := store.New(storePath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	for _, name := range names {
		if err := st.Upsert(&store.ProjectConfig{
			Name:      name,
			RepoPath:  "/tmp/repo",
			CreatedAt: "2025-01-01T00:00:00Z",
		}); err != nil {
			t.Fatalf("Upsert %s: %v", name, err)
		}
	}
}

func unmarshalPaginated(t *testing.T, body []byte) (items []interface{}, pagination map[string]interface{}) {
	t.Helper()
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	items, _ = resp["items"].([]interface{})
	pagination, _ = resp["pagination"].(map[string]interface{})
	return
}

func TestPaginationDefaultLimit(t *testing.T) {
	tmpDir := t.TempDir()
	seedProjects(t, tmpDir, []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j",
		"k", "l", "m", "n", "o", "p", "q", "r", "s", "t",
		"u", "v", "w", "x", "y", "z"})
	srv := NewServer(Config{
		ListenAddr: ":0",
		StorePath:  filepath.Join(tmpDir, "projects.json"),
	})
	req := httptest.NewRequest("GET", "/api/projects", nil)
	w := httptest.NewRecorder()
	srv.handleListProjects(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	items, pagination := unmarshalPaginated(t, w.Body.Bytes())
	if len(items) != 25 {
		t.Errorf("items count = %d, want 25", len(items))
	}
	if pagination["total"] != 26.0 {
		t.Errorf("total = %v, want 26", pagination["total"])
	}
	if pagination["limit"] != 25.0 {
		t.Errorf("limit = %v, want 25", pagination["limit"])
	}
	if pagination["offset"] != 0.0 {
		t.Errorf("offset = %v, want 0", pagination["offset"])
	}
	if pagination["next"] == nil {
		t.Error("next should not be nil (there are more pages)")
	}
	if pagination["prev"] != nil {
		t.Error("prev should be nil on first page")
	}
}

func TestPaginationValidLimits(t *testing.T) {
	for _, limit := range []int{10, 25, 50, 100} {
		t.Run("limit_%d", func(t *testing.T) {
			tmpDir := t.TempDir()
			names := make([]string, 60)
			for i := 0; i < 60; i++ {
				names[i] = string(rune('a'+i%26)) + string(rune('0'+i/26))
			}
			seedProjects(t, tmpDir, names)
			srv := NewServer(Config{
				ListenAddr: ":0",
				StorePath:  filepath.Join(tmpDir, "projects.json"),
			})
			req := httptest.NewRequest("GET", "/api/projects", nil)
			q := req.URL.Query()
			q.Set("limit", strconv.Itoa(limit))
			req.URL.RawQuery = q.Encode()
			w := httptest.NewRecorder()
			srv.handleListProjects(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 for limit=%d: %s", w.Code, limit, w.Body.String())
			}
			items, pagination := unmarshalPaginated(t, w.Body.Bytes())
			expectedCount := limit
			if expectedCount > 60 {
				expectedCount = 60
			}
			if len(items) != expectedCount {
				t.Errorf("items count = %d, want %d for limit=%d", len(items), expectedCount, limit)
			}
			if pagination["limit"] != float64(limit) {
				t.Errorf("limit = %v, want %d", pagination["limit"], limit)
			}
			if pagination["total"] != 60.0 {
				t.Errorf("total = %v, want 60", pagination["total"])
			}
		})
	}
}

func TestPaginationInvalidLimit(t *testing.T) {
	tmpDir := t.TempDir()
	seedProjects(t, tmpDir, []string{"a", "b", "c"})
	srv := NewServer(Config{
		ListenAddr: ":0",
		StorePath:  filepath.Join(tmpDir, "projects.json"),
	})
	req := httptest.NewRequest("GET", "/api/projects?limit=30", nil)
	w := httptest.NewRecorder()
	srv.handleListProjects(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var body map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["error"] != "limit must be one of [10,25,50,100]" {
		t.Errorf("error message = %v, want limit must be one of [10,25,50,100]", body["error"])
	}
}

func TestPaginationInvalidQueryParams(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		errMsg string
	}{
		{"invalid_limit", "limit=abc", "invalid limit"},
		{"invalid_offset", "limit=10&offset=xyz", "invalid offset"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			seedProjects(t, tmpDir, []string{"p1", "p2"})
			srv := NewServer(Config{
				ListenAddr: ":0",
				StorePath:  filepath.Join(tmpDir, "projects.json"),
			})
			req := httptest.NewRequest("GET", "/api/projects?"+tt.query, nil)
			w := httptest.NewRecorder()
			srv.handleListProjects(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", w.Code)
			}
			var body map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &body)
			if body["error"] != tt.errMsg {
				t.Errorf("error = %v, want %s", body["error"], tt.errMsg)
			}
		})
	}
}

func TestPaginationOffset(t *testing.T) {
	tmpDir := t.TempDir()
	names := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"}
	seedProjects(t, tmpDir, names)
	srv := NewServer(Config{
		ListenAddr: ":0",
		StorePath:  filepath.Join(tmpDir, "projects.json"),
	})
	req := httptest.NewRequest("GET", "/api/projects?limit=10&offset=10", nil)
	w := httptest.NewRecorder()
	srv.handleListProjects(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	items, pagination := unmarshalPaginated(t, w.Body.Bytes())
	if len(items) != 2 {
		t.Errorf("items count = %d, want 2 (last page has 2 items)", len(items))
	}
	if pagination["offset"] != 10.0 {
		t.Errorf("offset = %v, want 10", pagination["offset"])
	}
	if pagination["next"] != nil {
		t.Error("next should be nil (last page)")
	}
	if pagination["prev"] == nil {
		t.Error("prev should not be nil (we are on page 2)")
	}
}

func TestPaginationEmptyList(t *testing.T) {
	tmpDir := t.TempDir()
	srv := NewServer(Config{
		ListenAddr: ":0",
		StorePath:  filepath.Join(tmpDir, "projects.json"),
	})
	req := httptest.NewRequest("GET", "/api/projects", nil)
	w := httptest.NewRecorder()
	srv.handleListProjects(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	items, pagination := unmarshalPaginated(t, w.Body.Bytes())
	if len(items) != 0 {
		t.Errorf("items count = %d, want 0", len(items))
	}
	if pagination["total"] != 0.0 {
		t.Errorf("total = %v, want 0", pagination["total"])
	}
	if pagination["next"] != nil {
		t.Error("next should be nil (empty list)")
	}
	if pagination["prev"] != nil {
		t.Error("prev should be nil (first page)")
	}
}

func TestPaginationRunsFallback(t *testing.T) {
	// handleListRuns returns empty array for file store (no PG)
	tmpDir := t.TempDir()
	srv := NewServer(Config{
		ListenAddr: ":0",
		StorePath:  filepath.Join(tmpDir, "projects.json"),
	})
	req := httptest.NewRequest("GET", "/api/projects/test/runs", nil)
	w := httptest.NewRecorder()
	srv.handleListRuns(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body []interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("body length = %d, want 0", len(body))
	}
}

func TestPaginationSecondPage(t *testing.T) {
	tmpDir := t.TempDir()
	names := make([]string, 60)
	for i := 0; i < 60; i++ {
		names[i] = string(rune('A'+i%26)) + string(rune('0'+i/26))
	}
	seedProjects(t, tmpDir, names)
	srv := NewServer(Config{
		ListenAddr: ":0",
		StorePath:  filepath.Join(tmpDir, "projects.json"),
	})
	req := httptest.NewRequest("GET", "/api/projects?limit=25&offset=25", nil)
	w := httptest.NewRecorder()
	srv.handleListProjects(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	items, pagination := unmarshalPaginated(t, w.Body.Bytes())
	if len(items) != 25 {
		t.Errorf("items count = %d, want 25 on second page", len(items))
	}
	if pagination["offset"] != 25.0 {
		t.Errorf("offset = %v, want 25", pagination["offset"])
	}
	if pagination["next"] == nil {
		t.Error("next should not be nil (third page exists)")
	}
	nextURL, ok := pagination["next"].(string)
	if !ok {
		t.Fatal("next is not a string")
	}
	if nextURL != "/api/projects?limit=25&offset=50" {
		t.Errorf("next URL = %s, want /api/projects?limit=25&offset=50", nextURL)
	}
}

func TestPaginationOffsetBeyondTotal(t *testing.T) {
	tmpDir := t.TempDir()
	seedProjects(t, tmpDir, []string{"a", "b", "c"})
	srv := NewServer(Config{
		ListenAddr: ":0",
		StorePath:  filepath.Join(tmpDir, "projects.json"),
	})
	req := httptest.NewRequest("GET", "/api/projects?limit=10&offset=100", nil)
	w := httptest.NewRecorder()
	srv.handleListProjects(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	items, pagination := unmarshalPaginated(t, w.Body.Bytes())
	if len(items) != 0 {
		t.Errorf("items count = %d, want 0 (offset beyond total)", len(items))
	}
	if pagination["total"] != 3.0 {
		t.Errorf("total = %v, want 3", pagination["total"])
	}
}

func TestParsePaginationUnit(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantLimit  int
		wantOffset int
		wantErr    bool
		wantErrMsg string
	}{
		{name: "defaults", query: "", wantLimit: 25, wantOffset: 0},
		{name: "limit_10", query: "limit=10", wantLimit: 10, wantOffset: 0},
		{name: "limit_50", query: "limit=50", wantLimit: 50, wantOffset: 0},
		{name: "limit_100", query: "limit=100", wantLimit: 100, wantOffset: 0},
		{name: "with_offset", query: "limit=25&offset=50", wantLimit: 25, wantOffset: 50},
		{name: "zero_offset", query: "limit=25&offset=0", wantLimit: 25, wantOffset: 0},
		{name: "invalid_limit", query: "limit=30", wantErr: true, wantErrMsg: "limit must be one of [10,25,50,100]"},
		{name: "non_numeric_limit", query: "limit=abc", wantErr: true, wantErrMsg: "invalid limit"},
		{name: "non_numeric_offset", query: "limit=25&offset=xyz", wantErr: true, wantErrMsg: "invalid offset"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/projects?"+tt.query, nil)
			limit, offset, errStr, status := pagination.Parse(req.URL.Query())
			if tt.wantErr {
				if status == http.StatusOK {
					t.Fatal("expected error, got OK")
				}
				var body map[string]interface{}
				json.Unmarshal([]byte(errStr), &body)
				if body["error"] != tt.wantErrMsg {
					t.Errorf("error = %v, want %s", body["error"], tt.wantErrMsg)
				}
			} else {
				if status != http.StatusOK {
					t.Fatalf("unexpected error: %s (status %d)", errStr, status)
				}
				if limit != tt.wantLimit {
					t.Errorf("limit = %d, want %d", limit, tt.wantLimit)
				}
				if offset != tt.wantOffset {
					t.Errorf("offset = %d, want %d", offset, tt.wantOffset)
				}
			}
		})
	}
}

func TestBuildPaginationLinks(t *testing.T) {
	prefix := "/api/projects"
	tests := []struct {
		name     string
		offset   int
		limit    int
		total    int
		wantNext *string
		wantPrev *string
	}{
		{name: "first_page_has_more", offset: 0, limit: 25, total: 100, wantNext: strPtr("/api/projects?limit=25&offset=25"), wantPrev: nil},
		{name: "middle_page", offset: 25, limit: 25, total: 100, wantNext: strPtr("/api/projects?limit=25&offset=50"), wantPrev: strPtr("/api/projects?limit=25&offset=0")},
		{name: "last_page", offset: 75, limit: 25, total: 100, wantNext: nil, wantPrev: strPtr("/api/projects?limit=25&offset=50")},
		{name: "single_page", offset: 0, limit: 25, total: 10, wantNext: nil, wantPrev: nil},
		{name: "empty_list", offset: 0, limit: 25, total: 0, wantNext: nil, wantPrev: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next, prev := pagination.Links(tt.offset, tt.limit, tt.total, prefix)
			if next == nil && tt.wantNext != nil {
				t.Errorf("next = nil, want %s", *tt.wantNext)
			} else if next != nil && tt.wantNext == nil {
				t.Errorf("next = %s, want nil", *next)
			} else if next != nil && tt.wantNext != nil && *next != *tt.wantNext {
				t.Errorf("next = %s, want %s", *next, *tt.wantNext)
			}
			if prev == nil && tt.wantPrev != nil {
				t.Errorf("prev = nil, want %s", *tt.wantPrev)
			} else if prev != nil && tt.wantPrev == nil {
				t.Errorf("prev = %s, want nil", *prev)
			} else if prev != nil && tt.wantPrev != nil && *prev != *tt.wantPrev {
				t.Errorf("prev = %s, want %s", *prev, *tt.wantPrev)
			}
		})
	}
}

func strPtr(s string) *string { return &s }
