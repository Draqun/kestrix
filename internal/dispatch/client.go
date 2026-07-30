// Package dispatch implements GitLab pipeline dispatch.
//
// It takes a resolved set of DAG nodes and the generated gitlab-ci.yml,
// then pushes a new pipeline to a GitLab project via the API.
package dispatch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

var marshalJSON = json.Marshal

// Client dispatches pipelines to GitLab via its REST API.
type Client struct {
	// BaseURL is the GitLab instance URL (e.g. "https://gitlab.com").
	BaseURL string

	// Token is a personal access token with api scope.
	Token string

	// HTTPClient is the underlying HTTP client (defaults to http.DefaultClient).
	HTTPClient *http.Client
}

// DispatchRequest is the input to a dispatch call.
type DispatchRequest struct {
	// ProjectID is the numeric GitLab project ID.
	ProjectID int

	// Ref is the git ref to build (branch or tag).
	Ref string

	// YAML is the generated .gitlab-ci.yml content.
	YAML string

	// Variables are key-value pairs to inject as pipeline variables.
	Variables map[string]string
}

// DispatchResponse is the result of a successful pipeline creation.
type DispatchResponse struct {
	PipelineID int    `json:"id"`
	IID        int    `json:"iid"`
	Status     string `json:"status"`
	Ref        string `json:"ref"`
	WebURL     string `json:"web_url"`
}

// Dispatch creates a new pipeline on GitLab and returns the pipeline info.
//
// It uses POST /projects/:id/pipeline to create the pipeline, then
// sets variables via POST /projects/:id/pipelines/:pipeline_id/variables.
func (c *Client) Dispatch(req DispatchRequest) (*DispatchResponse, error) {
	if c.HTTPClient == nil {
		c.HTTPClient = http.DefaultClient
	}

	// Step 1: Create the pipeline
	body := map[string]interface{}{
		"ref": req.Ref,
	}
	bodyJSON, err := marshalJSON(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/v4/projects/%d/pipeline", c.BaseURL, req.ProjectID)
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("PRIVATE-TOKEN", c.Token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("dispatch request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("gitlab returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result DispatchResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Step 2: Set variables (if any)
	for key, value := range req.Variables {
		if err := c.setPipelineVariable(req.ProjectID, result.PipelineID, key, value); err != nil {
			return nil, fmt.Errorf("set variable %q: %w", key, err)
		}
	}

	return &result, nil
}

// setPipelineVariable sets a single variable on a pipeline.
func (c *Client) setPipelineVariable(projectID, pipelineID int, key, value string) error {
	body := map[string]string{
		"key":   key,
		"value": value,
	}
	bodyJSON, _ := json.Marshal(body)

	url := fmt.Sprintf("%s/api/v4/projects/%d/pipelines/%d/variables",
		c.BaseURL, projectID, pipelineID)
	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("PRIVATE-TOKEN", c.Token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gitlab returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// GetPipelineStatus returns the current status of a pipeline.
func (c *Client) GetPipelineStatus(projectID, pipelineID int) (*DispatchResponse, error) {
	if c.HTTPClient == nil {
		c.HTTPClient = http.DefaultClient
	}

	url := fmt.Sprintf("%s/api/v4/projects/%d/pipelines/%d", c.BaseURL, projectID, pipelineID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", c.Token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("status request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitlab returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result DispatchResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// DispatchDryRun simulates a dispatch without hitting the API.
// Returns what would be sent.
type DryRunResult struct {
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	ProjectID int               `json:"project_id"`
	Ref       string            `json:"ref"`
	Variables map[string]string `json:"variables,omitempty"`
}

func (c *Client) DispatchDryRun(req DispatchRequest) *DryRunResult {
	return &DryRunResult{
		URL:       fmt.Sprintf("%s/api/v4/projects/%d/pipeline", c.BaseURL, req.ProjectID),
		Method:    "POST",
		ProjectID: req.ProjectID,
		Ref:       req.Ref,
		Variables: req.Variables,
	}
}
