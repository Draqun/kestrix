package dispatch

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDispatchDryRun(t *testing.T) {
	c := &Client{BaseURL: "https://gitlab.com", Token: "test-token"}
	result := c.DispatchDryRun(DispatchRequest{
		ProjectID: 123,
		Ref:       "main",
		Variables: map[string]string{"API_KEY": "secret"},
	})

	if result.Method != "POST" {
		t.Errorf("method = %q, want POST", result.Method)
	}
	if result.ProjectID != 123 {
		t.Errorf("project_id = %d, want 123", result.ProjectID)
	}
	if result.Ref != "main" {
		t.Errorf("ref = %q, want main", result.Ref)
	}
	if result.Variables["API_KEY"] != "secret" {
		t.Errorf("variables[API_KEY] = %q, want secret", result.Variables["API_KEY"])
	}
}

func TestDispatchSuccess(t *testing.T) {
	// Mock GitLab API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header
		if r.Header.Get("PRIVATE-TOKEN") != "test-token" {
			t.Errorf("PRIVATE-TOKEN = %q, want test-token", r.Header.Get("PRIVATE-TOKEN"))
		}

		switch r.URL.Path {
		case "/api/v4/projects/42/pipeline":
			// Verify method and body
			if r.Method != "POST" {
				t.Errorf("method = %q, want POST", r.Method)
			}
			body, _ := io.ReadAll(r.Body)
			var req map[string]interface{}
			json.Unmarshal(body, &req)
			if req["ref"] != "main" {
				t.Errorf("ref = %v, want main", req["ref"])
			}

			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":      100,
				"iid":     5,
				"status":  "created",
				"ref":     "main",
				"web_url": "https://gitlab.com/test/-/pipelines/100",
			})

		case "/api/v4/projects/42/pipelines/100/variables":
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"key": "FOO", "value": "bar"})

		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c := &Client{BaseURL: server.URL, Token: "test-token"}
	result, err := c.Dispatch(DispatchRequest{
		ProjectID: 42,
		Ref:       "main",
		Variables: map[string]string{"FOO": "bar"},
	})

	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if result.PipelineID != 100 {
		t.Errorf("pipeline_id = %d, want 100", result.PipelineID)
	}
	if result.Status != "created" {
		t.Errorf("status = %q, want created", result.Status)
	}
}

func TestDispatchAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"403 Forbidden"}`))
	}))
	defer server.Close()

	c := &Client{BaseURL: server.URL, Token: "bad-token"}
	_, err := c.Dispatch(DispatchRequest{
		ProjectID: 1,
		Ref:       "main",
	})

	if err == nil {
		t.Fatal("expected error for 403")
	}
}
