package stevedore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client communicates with the Stevedore daemon via HTTP API.
// It enforces strict version matching to prevent subtle bugs from mismatched binaries.
type Client struct {
	// BaseURL is the daemon's HTTP address (e.g., "http://localhost:42107")
	BaseURL string
	// AdminKey is the authentication token
	AdminKey string
	// Version is this client's version (must match daemon)
	Version string
	// Build is this client's build hash (must match daemon)
	Build string
	// HTTPClient is the underlying HTTP client (uses default if nil)
	HTTPClient *http.Client
}

// NewClient creates a new client for communicating with the daemon.
func NewClient(baseURL, adminKey, version, build string) *Client {
	return &Client{
		BaseURL:  baseURL,
		AdminKey: adminKey,
		Version:  version,
		Build:    build,
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// APICheckResult represents the result of a check operation from the API.
type APICheckResult struct {
	Deployment    string `json:"deployment"`
	CurrentCommit string `json:"currentCommit"`
	RemoteCommit  string `json:"remoteCommit"`
	HasChanges    bool   `json:"hasChanges"`
	Branch        string `json:"branch"`
}

// APISyncResult represents the result of a sync operation from the API.
type APISyncResult struct {
	Deployment string `json:"deployment"`
	Commit     string `json:"commit"`
	Branch     string `json:"branch"`
	Synced     bool   `json:"synced"`
}

// APIDeployResult represents the result of a deploy operation from the API.
type APIDeployResult struct {
	Deployment  string   `json:"deployment"`
	ProjectName string   `json:"projectName"`
	ComposeFile string   `json:"composeFile"`
	Services    []string `json:"services"`
	Deployed    bool     `json:"deployed"`
}

// APIHealthResult represents the result of a health check from the API.
type APIHealthResult struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Build   string `json:"build"`
}

// ClientError represents an error from the daemon API.
type ClientError struct {
	StatusCode int
	Message    string
}

func (e *ClientError) Error() string {
	return fmt.Sprintf("daemon error (status %d): %s", e.StatusCode, e.Message)
}

// IsVersionMismatch returns true if the error is due to a version mismatch.
func (e *ClientError) IsVersionMismatch() bool {
	return e.StatusCode == http.StatusConflict
}

// Health checks if the daemon is running and returns version info.
// This does not require authentication and does not check version compatibility.
func (c *Client) Health(ctx context.Context) (*APIHealthResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/healthz", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &ClientError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var result APIHealthResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &result, nil
}

// Check checks for updates for a deployment without modifying files.
// This is safe to call while the deployment is running.
func (c *Client) Check(ctx context.Context, deployment string) (*APICheckResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/check/"+deployment, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	c.addHeaders(req)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp.StatusCode, body)
	}

	var result APICheckResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &result, nil
}

// Sync triggers a repository sync for a deployment.
func (c *Client) Sync(ctx context.Context, deployment string) (*APISyncResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/sync/"+deployment, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	c.addHeaders(req)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp.StatusCode, body)
	}

	var result APISyncResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &result, nil
}

// Deploy triggers a deployment via the daemon API.
func (c *Client) Deploy(ctx context.Context, deployment string) (*APIDeployResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/deploy/"+deployment, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	c.addHeaders(req)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp.StatusCode, body)
	}

	var result APIDeployResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &result, nil
}

// Exec executes a CLI command inside the daemon process.
// Returns the output, exit code, and any error from the daemon.
func (c *Client) Exec(ctx context.Context, args []string) (output string, exitCode int, err error) {
	reqBody := ExecRequest{Args: args}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", 1, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/exec", bytes.NewReader(body))
	if err != nil {
		return "", 1, fmt.Errorf("create request: %w", err)
	}

	c.addHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", 1, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 1, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		clientErr := c.parseError(resp.StatusCode, respBody)
		return "", 1, clientErr
	}

	var result ExecResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", 1, fmt.Errorf("parse response: %w", err)
	}

	if result.Error != "" {
		return result.Output, result.ExitCode, fmt.Errorf("%s", result.Error)
	}

	return result.Output, result.ExitCode, nil
}

// VerifyVersion checks if the daemon version matches this client.
// Returns an error if versions don't match.
func (c *Client) VerifyVersion(ctx context.Context) error {
	health, err := c.Health(ctx)
	if err != nil {
		return fmt.Errorf("failed to get daemon health: %w", err)
	}

	if health.Version != c.Version || health.Build != c.Build {
		return &ClientError{
			StatusCode: http.StatusConflict,
			Message: fmt.Sprintf(
				"version mismatch: client=%s/%s, daemon=%s/%s. "+
					"Stevedore binaries must match exactly. "+
					"Run 'stevedore doctor' to diagnose or reinstall stevedore.",
				c.Version, c.Build, health.Version, health.Build),
		}
	}

	return nil
}

// addHeaders adds required headers for authenticated API calls.
func (c *Client) addHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.AdminKey)
	req.Header.Set(HeaderStevedoreVersion, c.Version)
	req.Header.Set(HeaderStevedoreBuild, c.Build)
}

// httpClient returns the HTTP client to use.
func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

// parseError parses an error response from the daemon.
func (c *Client) parseError(statusCode int, body []byte) error {
	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err != nil {
		return &ClientError{
			StatusCode: statusCode,
			Message:    string(body),
		}
	}
	return &ClientError{
		StatusCode: statusCode,
		Message:    errResp.Error,
	}
}
