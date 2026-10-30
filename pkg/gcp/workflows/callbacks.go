package workflows

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/httpdebug"
)

const callbacksAPIBase = "https://workflowexecutions.googleapis.com/v1"

// CallbackInfo holds metadata about a pending callback.
type CallbackInfo struct {
	Name   string `json:"name"`
	Method string `json:"method"`
	URL    string `json:"url"`
}

type callbacksResponse struct {
	Callbacks []struct {
		Name   string `json:"name"`
		Method string `json:"method"`
	} `json:"callbacks"`
}

// ListCallbacks returns pending callbacks for an execution using the REST API.
// executionName must be the full resource name with project number.
func (c *Client) ListCallbacks(ctx context.Context, executionName string) ([]CallbackInfo, error) {
	httpClient, err := newWorkflowHTTPClient(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, wrapAuthError("creating HTTP client for callbacks", err)
	}
	httpClient.Transport = httpdebug.Wrap(httpClient.Transport)

	url := fmt.Sprintf("%s/%s/callbacks", callbacksAPIBase, executionName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating callbacks request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, wrapAuthError("listing callbacks", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading callbacks response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listing callbacks: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var parsed callbacksResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parsing callbacks response: %w", err)
	}

	var result []CallbackInfo
	for _, cb := range parsed.Callbacks {
		result = append(result, CallbackInfo{
			Name:   cb.Name,
			Method: cb.Method,
			URL:    fmt.Sprintf("%s/%s", callbacksAPIBase, cb.Name),
		})
	}

	return result, nil
}

// TriggerCallback sends an HTTP request to a callback URL to resume a paused workflow.
func (c *Client) TriggerCallback(ctx context.Context, callbackURL, method string, data map[string]interface{}) error {
	httpClient, err := newWorkflowHTTPClient(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return wrapAuthError("creating HTTP client for callback trigger", err)
	}
	httpClient.Transport = httpdebug.Wrap(httpClient.Transport)

	var bodyReader io.Reader
	if data != nil {
		jsonData, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("marshaling callback data: %w", err)
		}
		bodyReader = bytes.NewReader(jsonData)
	}

	if method == "" {
		method = http.MethodPost
	}

	req, err := http.NewRequestWithContext(ctx, method, callbackURL, bodyReader)
	if err != nil {
		return fmt.Errorf("creating callback request: %w", err)
	}
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return wrapAuthError("triggering callback", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("triggering callback: HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
