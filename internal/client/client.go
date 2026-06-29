// Package client is a Go SDK for the bottle API. The CLI uses it, but so could
// any other Go program — keeping all HTTP details (URLs, status codes, JSON
// shapes, SSE parsing) here means the CLI commands stay thin and this logic is
// unit-testable against an httptest server.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/pjmj/bottle/internal/job"
)

// Client talks to a bottle API server.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a Client for the API at baseURL (e.g. "http://localhost:8080").
//
// The underlying http.Client has NO global timeout on purpose: the logs
// endpoint streams for as long as a job runs. Per-call deadlines are supplied
// by the context the caller passes, which is the right granularity — a short
// timeout for submit/list/get, none (just cancellation) for streaming.
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{},
	}
}

// Submit creates a job to run command and returns the created job.
func (c *Client) Submit(ctx context.Context, command string) (*job.Job, error) {
	body, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/jobs", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		return nil, errorFrom(resp)
	}
	return decodeJob(resp.Body)
}

// List returns all jobs, newest first.
func (c *Client) List(ctx context.Context) ([]*job.Job, error) {
	resp, err := c.do(ctx, http.MethodGet, "/jobs")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, errorFrom(resp)
	}
	var jobs []*job.Job
	if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
		return nil, fmt.Errorf("decode jobs: %w", err)
	}
	return jobs, nil
}

// Get returns a single job by ID.
func (c *Client) Get(ctx context.Context, id string) (*job.Job, error) {
	resp, err := c.do(ctx, http.MethodGet, "/jobs/"+id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, errorFrom(resp)
	}
	return decodeJob(resp.Body)
}

// StreamLogs connects to the job's SSE log stream and writes each log line to
// out until the job finishes (the server's "end" event), the stream closes, or
// ctx is cancelled.
func (c *Client) StreamLogs(ctx context.Context, id string, out io.Writer) error {
	resp, err := c.do(ctx, http.MethodGet, "/jobs/"+id+"/logs")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return errorFrom(resp)
	}

	// Parse the SSE wire format line by line: "data: <line>" carries output,
	// "event: end" signals completion, blank lines separate events.
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "event: end":
			return nil
		case strings.HasPrefix(line, "data: "):
			if _, err := fmt.Fprintln(out, strings.TrimPrefix(line, "data: ")); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

// do issues a simple request with no body.
func (c *Client) do(ctx context.Context, method, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	return c.http.Do(req)
}

func decodeJob(r io.Reader) (*job.Job, error) {
	var j job.Job
	if err := json.NewDecoder(r).Decode(&j); err != nil {
		return nil, fmt.Errorf("decode job: %w", err)
	}
	return &j, nil
}

// errorFrom turns a non-2xx response into an error, surfacing the API's
// {"error": "..."} message when present so the user sees a useful reason.
func errorFrom(resp *http.Response) error {
	var payload struct {
		Error string `json:"error"`
	}
	// Limit the read so a misbehaving server can't make us buffer unboundedly.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	_ = json.Unmarshal(body, &payload)
	if payload.Error != "" {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, payload.Error)
	}
	return fmt.Errorf("server returned %d", resp.StatusCode)
}
