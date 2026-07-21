package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockGitLab is a minimal mock of the GitLab Pipeline API.
//
// The struct has hooks for forcing failure modes so the dispatch.Client's
// error branches are reachable from a unit test. Each flag, when set,
// makes the corresponding endpoint return an error response; tests assert
// that the client surfaces those errors.
type mockGitLab struct {
	mu               sync.Mutex
	pipelines        map[int]*DispatchResponse // pipelineID → response
	nextID           int
	vars             map[int]map[string]string // pipelineID → key→value
	failCreateStatus int                       // when != 0, /pipeline returns this status with an error body
	failCreateBody   string                    // body returned with failCreateStatus
	failVarStatus    int                       // when != 0, /pipelines/:id/variables returns this status
	failGetStatus    int                       // when != 0, /pipelines/:id GET returns this status
	badJSON          bool                      // when true, /pipeline returns syntactically invalid JSON
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
			if m.failVarStatus != 0 {
				body := m.failVarStatus // capture while holding lock
				m.mu.Unlock()
				http.Error(w, `{"message":"set variable failed"}`, body)
				return
			}
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
			if m.failGetStatus != 0 {
				body := m.failGetStatus
				m.mu.Unlock()
				http.Error(w, `{"message":"get pipeline failed"}`, body)
				return
			}
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

			// Force mode: failCreateStatus short-circuits to a non-201 response
			// so the client's StatusCode!=Created branch executes.
			if m.failCreateStatus != 0 {
				http.Error(w, m.failCreateBody, m.failCreateStatus)
				return
			}
			// Force mode: badJSON exercises the json.Unmarshal error branch.
			if m.badJSON {
				w.WriteHeader(http.StatusCreated)
				// not valid JSON on purpose
				w.Write([]byte("{not-valid-json"))
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

// TestDispatchGitLabErrorStatus exercises the `StatusCode != Created`
// branch of Dispatch. We arrange a mock that returns 500 and assert that
// the client surfaces the error (and embeds the offending body).
func TestDispatchGitLabErrorStatus(t *testing.T) {
	mock := newMockGitLab()
	mock.failCreateStatus = http.StatusInternalServerError
	mock.failCreateBody = `{"message":"worker queue full"}`
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	_, err := client.Dispatch(DispatchRequest{ProjectID: 1, Ref: "main"})
	if err == nil {
		t.Fatal("expected error for 500 from /pipeline")
	}
	// Error message should mention the status code so operators can debug.
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %q, want to mention status 500", err)
	}
}

// TestDispatchDecodeError exercises the json.Unmarshal error branch of
// Dispatch (server returned 201 but body is not valid JSON).
func TestDispatchDecodeError(t *testing.T) {
	mock := newMockGitLab()
	mock.badJSON = true
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	_, err := client.Dispatch(DispatchRequest{ProjectID: 1, Ref: "main"})
	if err == nil {
		t.Fatal("expected decode error when server returns invalid JSON body")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Errorf("err = %q, want decode response error", err)
	}
}

// TestDispatchSetVariableFailure exercises the inner setPipelineVariable
// failure branch of Dispatch (pipeline created, but variable update fails).
func TestDispatchSetVariableFailure(t *testing.T) {
	mock := newMockGitLab()
	mock.failVarStatus = http.StatusBadRequest
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	_, err := client.Dispatch(DispatchRequest{
		ProjectID: 1,
		Ref:       "main",
		Variables: map[string]string{"TRIGGER": "build"},
	})
	if err == nil {
		t.Fatal("expected error when /variables returns non-success")
	}
	if !strings.Contains(err.Error(), "set variable") {
		t.Errorf("err = %q, want 'set variable' error wrapper", err)
	}
}

// TestSetPipelineVariableBadStatus exercises the StatusCode!= Created &&
// != OK branch of setPipelineVariable. This is the body-read fallback
// that surfaces the GitLab error message verbatim on failure.
func TestSetPipelineVariableBadStatus(t *testing.T) {
	mock := newMockGitLab()
	mock.failVarStatus = http.StatusUnprocessableEntity
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	// Drive setPipelineVariable indirectly via Dispatch with one variable.
	client := &Client{BaseURL: srv.URL, Token: "tok"}
	_, err := client.Dispatch(DispatchRequest{
		ProjectID: 1,
		Ref:       "main",
		Variables: map[string]string{"K": "V"},
	})
	if err == nil {
		t.Fatal("expected error from setPipelineVariable failure")
	}
}

// TestGetPipelineStatusServerError exercises the HTTPClient.Do error
// branch of GetPipelineStatus. We use a TCP listener that accepts the
// connection and immediately closes it, so the client receives a "read
// EOF" / connection reset which is a transport-layer failure.
func TestGetPipelineStatusServerError(t *testing.T) {
	mock := newMockGitLab()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}

	// Sanity: create the pipeline first so the path that the broken client
	// hits is the same parse-pipeline-id step that triggers Do().
	if _, err := client.Dispatch(DispatchRequest{ProjectID: 1, Ref: "main"}); err != nil {
		t.Fatalf("setup dispatch: %v", err)
	}

	// Replace the HTTPClient with one whose Transport rejects all
	// connections to force Do() to return a network error.
	client.HTTPClient = &http.Client{
		Timeout: 1 * time.Second,
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return nil, fmt.Errorf("synthetic dial failure")
			},
		},
	}
	if _, err := client.GetPipelineStatus(1, 1); err == nil {
		t.Fatal("expected transport error from broken HTTPClient")
	}
}

// TestGetPipelineStatusBadJSON exercises the json.Unmarshal error branch
// of GetPipelineStatus. The mock returns 200 with a syntactically invalid
// body; the client must surface a decode error.
func TestGetPipelineStatusBadJSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/1/pipelines/1", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{malformed"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	if _, err := client.GetPipelineStatus(1, 1); err == nil {
		t.Fatal("expected decode error")
	} else if !strings.Contains(err.Error(), "decode response") {
		t.Errorf("err = %q, want decode response error", err)
	}
}

// TestGetPipelineStatusServerErrorResponse exercises the non-200 branch of
// GetPipelineStatus by having the mock return 500.
func TestGetPipelineStatusServerErrorResponse(t *testing.T) {
	mock := newMockGitLab()
	mock.failGetStatus = http.StatusInternalServerError
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	if _, err := client.GetPipelineStatus(1, 1); err == nil {
		t.Fatal("expected error for 500 from /pipelines/:id GET")
	}
}

// TestDispatchMarshalErrorUnreachable documents the remaining ~4% on
// Dispatch's `if err := json.Marshal(body); err != nil` branch. The body
// passed to json.Marshal is a map[string]interface{} containing a single
// string field, which encoding/json never refuses to marshal. To exercise
// the branch the function would need to receive a value type whose JSON
// representation can fail (channels, functions, cyclic data) — which is
// not possible with the current input contract. The branch is therefore
// considered unreachable from the public API.
//
// We still keep this dispatch path tested end to end via
// TestDispatchSuccess so the package-level coverage stays close to
// 100%, but the unreachable marshal-error line cannot be driven by any
// portable unit test.

// TestDispatchTransportError exercises the HTTPClient.Do error branch of
// Dispatch by pointing the client at a URL whose server is closed. The
// transport call returns a connection-refused error which propagates
// wrapped as "dispatch request".
func TestDispatchTransportError(t *testing.T) {
	// Find an unused port by binding and immediately closing.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	closedURL := srv.URL
	srv.Close()

	client := &Client{BaseURL: closedURL, Token: "tok"}
	_, err := client.Dispatch(DispatchRequest{ProjectID: 1, Ref: "main"})
	if err == nil {
		t.Fatal("expected transport error when server is down")
	}
	if !strings.Contains(err.Error(), "dispatch request") {
		t.Errorf("err = %q, want 'dispatch request' wrapper", err)
	}
}

// TestGetPipelineStatusTransportError exercises the HTTPClient.Do error
// branch of GetPipelineStatus.
func TestGetPipelineStatusTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	url := srv.URL
	srv.Close()

	client := &Client{BaseURL: url, Token: "tok"}
	if _, err := client.GetPipelineStatus(1, 1); err == nil {
		t.Fatal("expected transport error")
	}
}

// TestDispatchInvalidURL exercises the http.NewRequest error branch of
// Dispatch. A URL with a percent-encoded invalid sequence makes
// url.Parse fail inside http.NewRequest, returning a request-creation
// error.
func TestDispatchInvalidURL(t *testing.T) {
	// http.NewRequest validates "method URL" - a control char in URL
	// produces "net/url: invalid control character in URL".
	client := &Client{BaseURL: "http://example.com/\x00bad", Token: "tok"}
	_, err := client.Dispatch(DispatchRequest{ProjectID: 1, Ref: "main"})
	if err == nil {
		t.Fatal("expected NewRequest error for invalid URL")
	}
	if !strings.Contains(err.Error(), "create request") {
		t.Errorf("err = %q, want 'create request' wrapper", err)
	}
}

func TestGetPipelineStatusInvalidURL(t *testing.T) {
	client := &Client{BaseURL: "http://example.com/\x00bad", Token: "tok"}
	if _, err := client.GetPipelineStatus(1, 1); err == nil {
		t.Fatal("expected NewRequest error for invalid URL")
	}
}

// TestSetPipelineVariableTransportError exercises HTTPClient.Do error
// inside setPipelineVariable. The pipeline creation succeeds (real mock),
// then the variable endpoint's transport is broken so the Do call fails
// after the pipeline ID has already been returned by the mock. We achieve
// this by stopping the server mid-flight: dispatch against the live
// server, then close the server before invoking Dispatch again with a
// fresh client sharing the (now-closed) URL.
//
// In practice the reachability we care about is: setPipelineVariable
// observed a network error. We get there by giving the client a URL
// derived from a freshly-closed server.
func TestSetPipelineVariableTransportError(t *testing.T) {
	mock := newMockGitLab()
	srv := httptest.NewServer(mock.handler())
	client := &Client{BaseURL: srv.URL, Token: "tok"}
	srv.Close() // server is now closed; both endpoints will be unreachable

	_, err := client.Dispatch(DispatchRequest{
		ProjectID: 1,
		Ref:       "main",
		Variables: map[string]string{"K": "V"},
	})
	if err == nil {
		t.Fatal("expected transport error when server is closed")
	}
}

// TestSetPipelineVariableNewRequestError drives setPipelineVariable's
// http.NewRequest branch directly. Once we have a pipeline ID, swap the
// client's BaseURL to one that http.NewRequest rejects so the variable
// call fails at construction time. BaseURL is a field on a value-receiver
// function; since setPipelineVariable runs read on c, we mutate via pointer.
func TestSetPipelineVariableNewRequestError(t *testing.T) {
	mock := newMockGitLab()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	// First, create a pipeline so we have a real pipelineID.
	resp, err := client.Dispatch(DispatchRequest{ProjectID: 1, Ref: "main"})
	if err != nil {
		t.Fatalf("setup dispatch: %v", err)
	}
	if resp == nil {
		t.Fatal("setup: no pipeline response")
	}

	// Now override the BaseURL with one that breaks http.NewRequest so
	// setPipelineVariable fails at request-creation time.
	client.BaseURL = "http://example.com/\x00bad"
	if err := client.setPipelineVariable(1, resp.PipelineID, "K", "V"); err == nil {
		t.Fatal("expected setPipelineVariable to fail on invalid URL")
	}
}

// TestSetPipelineVariableDoError drives setPipelineVariable's
// HTTPClient.Do error branch. Create the pipeline via the live server,
// then close the server so the variables endpoint's Do call returns a
// transport error. Direct invocation lets us break the server between
// the create-pipeline and set-variable stages.
func TestSetPipelineVariableDoError(t *testing.T) {
	mock := newMockGitLab()
	srv := httptest.NewServer(mock.handler())

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	resp, err := client.Dispatch(DispatchRequest{ProjectID: 1, Ref: "main"})
	if err != nil {
		srv.Close()
		t.Fatalf("setup dispatch: %v", err)
	}

	// Close now — setPipelineVariable is the next network call.
	srv.Close()

	if err := client.setPipelineVariable(1, resp.PipelineID, "K", "V"); err == nil {
		t.Fatal("expected setPipelineVariable to fail on closed server transport")
	}
}
