package dispatch

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// mockGitLab is a minimal mock of the GitLab Pipeline API.
type mockGitLab struct {
	mu        sync.Mutex
	pipelines map[int]*DispatchResponse // pipelineID → response
	nextID    int
	vars      map[int]map[string]string // pipelineID → key→value
}

func newMockGitLab() *mockGitLab {
	return &mockGitLab{
		pipelines: make(map[int]*DispatchResponse),
		nextID:    1,
		vars:      make(map[int]map[string]string),
	}
}

func (m *mockGitLab) handler() http.Handler {
	mux := http.NewServeMux()

	// POST /api/v4/projects/:id/pipeline
	mux.HandleFunc("/api/v4/projects/", func(w http.ResponseWriter, r *http.Request) {
		// Parse path: /api/v4/projects/{id}/pipeline or /api/v4/projects/{id}/pipelines/{pid}/variables
		path := r.URL.Path

		// Check for pipeline variables endpoint
		var pipelineID, projectID int
		n, _ := fmt.Sscanf(path, "/api/v4/projects/%d/pipelines/%d/variables", &projectID, &pipelineID)
		if n == 2 && r.Method == "POST" {
			m.mu.Lock()
			defer m.mu.Unlock()
			var body struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if m.vars[pipelineID] == nil {
				m.vars[pipelineID] = make(map[string]string)
			}
			m.vars[pipelineID][body.Key] = body.Value
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(body)
			return
		}

		// Check for pipeline status endpoint
		n, _ = fmt.Sscanf(path, "/api/v4/projects/%d/pipelines/%d", &projectID, &pipelineID)
		if n == 2 && r.Method == "GET" {
			m.mu.Lock()
			defer m.mu.Unlock()
			p, ok := m.pipelines[pipelineID]
			if !ok {
				http.Error(w, `{"message":"404 Not Found"}`, http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(p)
			return
		}

		// POST /api/v4/projects/:id/pipeline (create)
		n, _ = fmt.Sscanf(path, "/api/v4/projects/%d/pipeline", &projectID)
		if n == 1 && r.Method == "POST" {
			m.mu.Lock()
			defer m.mu.Unlock()

			// Verify token
			if r.Header.Get("PRIVATE-TOKEN") == "" {
				http.Error(w, `{"message":"401 Unauthorized"}`, http.StatusUnauthorized)
				return
			}

			var body struct {
				Ref string `json:"ref"`
			}
			json.NewDecoder(r.Body).Decode(&body)

			id := m.nextID
			m.nextID++
			resp := &DispatchResponse{
				PipelineID: id,
				IID:        id,
				Status:     "created",
				Ref:        body.Ref,
				WebURL:     fmt.Sprintf("https://gitlab.com/project/-/pipelines/%d", id),
			}
			m.pipelines[id] = resp
			m.vars[id] = make(map[string]string)

			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(resp)
			return
		}

		http.NotFound(w, r)
	})

	return mux
}

func TestDispatchSuccess(t *testing.T) {
	mock := newMockGitLab()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	client := &Client{
		BaseURL: srv.URL,
		Token:   "test-token",
	}

	resp, err := client.Dispatch(DispatchRequest{
		ProjectID: 12345,
		Ref:       "main",
		Variables: map[string]string{
			"METACI_TRIGGERED_TASKS": "build,test",
			"METACI_CHANGED_REPOS":   "auth_service",
		},
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if resp.PipelineID == 0 {
		t.Error("PipelineID not set")
	}
	if resp.Ref != "main" {
		t.Errorf("Ref = %q, want %q", resp.Ref, "main")
	}
	if resp.WebURL == "" {
		t.Error("WebURL not set")
	}

	// Verify variables were set.
	mock.mu.Lock()
	vars := mock.vars[resp.PipelineID]
	mock.mu.Unlock()
	if vars["METACI_TRIGGERED_TASKS"] != "build,test" {
		t.Errorf("triggered tasks = %q, want %q", vars["METACI_TRIGGERED_TASKS"], "build,test")
	}
	if vars["METACI_CHANGED_REPOS"] != "auth_service" {
		t.Errorf("changed repos = %q, want %q", vars["METACI_CHANGED_REPOS"], "auth_service")
	}
}

func TestDispatchNoVariables(t *testing.T) {
	mock := newMockGitLab()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	resp, err := client.Dispatch(DispatchRequest{ProjectID: 1, Ref: "main"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if resp.PipelineID != 1 {
		t.Errorf("PipelineID = %d, want 1", resp.PipelineID)
	}
}

func TestDispatchUnauthorized(t *testing.T) {
	mock := newMockGitLab()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: ""}
	_, err := client.Dispatch(DispatchRequest{ProjectID: 1, Ref: "main"})
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestGetPipelineStatus(t *testing.T) {
	mock := newMockGitLab()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}

	// Create a pipeline first.
	resp, _ := client.Dispatch(DispatchRequest{ProjectID: 1, Ref: "main"})

	// Get status.
	status, err := client.GetPipelineStatus(1, resp.PipelineID)
	if err != nil {
		t.Fatalf("GetPipelineStatus: %v", err)
	}
	if status.Status != "created" {
		t.Errorf("Status = %q, want %q", status.Status, "created")
	}
}

func TestGetPipelineStatusNotFound(t *testing.T) {
	mock := newMockGitLab()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	_, err := client.GetPipelineStatus(1, 99999)
	if err == nil {
		t.Fatal("expected error for nonexistent pipeline")
	}
}

func TestDispatchDryRun(t *testing.T) {
	client := &Client{BaseURL: "https://gitlab.com", Token: "tok"}
	result := client.DispatchDryRun(DispatchRequest{
		ProjectID: 12345,
		Ref:       "main",
		Variables: map[string]string{"KEY": "VAL"},
	})
	if result.Method != "POST" {
		t.Errorf("Method = %q, want POST", result.Method)
	}
	if result.ProjectID != 12345 {
		t.Errorf("ProjectID = %d, want 12345", result.ProjectID)
	}
}

func TestMultiplePipelines(t *testing.T) {
	mock := newMockGitLab()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}

	r1, _ := client.Dispatch(DispatchRequest{ProjectID: 1, Ref: "main"})
	r2, _ := client.Dispatch(DispatchRequest{ProjectID: 1, Ref: "develop"})

	if r1.PipelineID == r2.PipelineID {
		t.Error("two pipelines got same ID")
	}
	if r1.Ref != "main" {
		t.Errorf("r1.Ref = %q", r1.Ref)
	}
	if r2.Ref != "develop" {
		t.Errorf("r2.Ref = %q", r2.Ref)
	}
}
