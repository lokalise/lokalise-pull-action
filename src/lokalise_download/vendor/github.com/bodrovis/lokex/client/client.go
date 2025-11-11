// Package client provides a wrapper around the Lokalise API that the
// upload/download packages depend on. It handles base URL normalization,
// authentication, JSON encoding/decoding, retry with exponential backoff,
// and simple polling of asynchronous processes.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bodrovis/lokex/internal/apierr"
)

const (
	// defaultBaseURL is the production Lokalise REST API v2 base.
	defaultBaseURL = "https://api.lokalise.com/api2/"

	// defaultUserAgent is sent on every request unless overridden via WithUserAgent.
	defaultUserAgent = "lokex/1.0.2"

	// defaultErrCap caps how many bytes we slurp from a non-2xx response when
	// constructing an apierr.APIError.
	defaultErrCap = 8192

	// defaults for retry/backoff and HTTP timeouts.
	defaultMaxRetries     = 3
	defaultInitialBackoff = 400 * time.Millisecond
	defaultMaxBackoff     = 5 * time.Second
	defaultHTTPTimeout    = 30 * time.Second

	// defaults for the polling helper.
	defaultPollInitialWait = 1 * time.Second
	defaultPollMaxWait     = 120 * time.Second
)

// Client is a minimal Lokalise API client.
// It is safe for concurrent use after construction (fields are not mutated
// post-NewClient). The embedded http.Client is used as-is.
type Client struct {
	BaseURL         string        // normalized base URL with trailing slash
	Token           string        // API token (X-Api-Token header)
	ProjectID       string        // default project ID for project-scoped endpoints
	UserAgent       string        // User-Agent header value
	HTTPClient      *http.Client  // underlying HTTP client
	MaxRetries      int           // number of retries after first attempt
	InitialBackoff  time.Duration // first backoff duration for withExpBackoff
	MaxBackoff      time.Duration // cap for backoff (and jittered sleep)
	PollInitialWait time.Duration // initial wait between PollProcesses rounds
	PollMaxWait     time.Duration // overall cap for PollProcesses duration
}

// QueuedProcess is a normalized view over Lokalise "processes/*" responses.
// DownloadURL is populated when the process produces a file (e.g., download).
type QueuedProcess struct {
	ProcessID   string `json:"process_id"`
	Status      string `json:"status"`
	DownloadURL string `json:"download_url,omitempty"`
}

// processResponse mirrors the subset of the Lokalise response we care about.
// It stays unexported; callers use QueuedProcess instead.
type processResponse struct {
	Process struct {
		ProcessID string `json:"process_id"`
		Status    string `json:"status"`
		Message   string `json:"message"`
		Details   struct {
			DownloadURL string `json:"download_url"`
		} `json:"details"`
	} `json:"process"`
}

// ToQueuedProcess converts a typed API response into a flattened QueuedProcess.
func (pr *processResponse) ToQueuedProcess() QueuedProcess {
	return QueuedProcess{
		ProcessID:   pr.Process.ProcessID,
		Status:      pr.Process.Status,
		DownloadURL: pr.Process.Details.DownloadURL,
	}
}

// Option customizes a Client during construction.
// Errors returned by an Option abort NewClient.
type Option func(*Client) error

// WithBaseURL sets a custom API base URL.
// The value must be an absolute URL; a trailing slash is enforced.
func WithBaseURL(u string) Option {
	return func(c *Client) error {
		u = strings.TrimSpace(u)
		if u == "" {
			return errors.New("base URL cannot be empty")
		}
		parsed, err := url.Parse(u)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return errors.New("invalid base URL")
		}
		// normalize: ensure trailing slash and keep path/joining sane
		if !strings.HasSuffix(parsed.Path, "/") {
			parsed.Path += "/"
		}
		c.BaseURL = parsed.String()
		return nil
	}
}

// WithUserAgent overrides the default User-Agent string.
// An empty value is ignored.
func WithUserAgent(ua string) Option {
	return func(c *Client) error {
		ua = strings.TrimSpace(ua)
		if ua != "" {
			c.UserAgent = ua
		}
		return nil
	}
}

// WithHTTPClient replaces the underlying http.Client.
// The client must be non-nil.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) error {
		if hc == nil {
			return errors.New("http client cannot be nil")
		}
		c.HTTPClient = hc
		return nil
	}
}

// WithHTTPTimeout sets HTTP client timeout. If no HTTP client exists yet,
// a default one is created first.
func WithHTTPTimeout(d time.Duration) Option {
	return func(c *Client) error {
		if c.HTTPClient == nil {
			c.HTTPClient = &http.Client{}
		}
		c.HTTPClient.Timeout = d
		return nil
	}
}

// WithMaxRetries sets how many *retries* to attempt after the initial try.
// Use 0 (or negative) to disable retries entirely.
func WithMaxRetries(n int) Option {
	return func(c *Client) error {
		c.MaxRetries = n
		return nil
	}
}

// WithBackoff sets the exponential backoff window for retries.
// Zero/negative inputs fall back to library defaults.
// If max < initial, max is promoted to initial.
func WithBackoff(initial, max time.Duration) Option {
	return func(c *Client) error {
		if initial <= 0 {
			initial = defaultInitialBackoff
		}
		if max <= 0 {
			max = defaultMaxBackoff
		}
		if max < initial {
			max = initial
		}
		c.InitialBackoff = initial
		c.MaxBackoff = max
		return nil
	}
}

// WithPollWait sets the initial wait and the overall max wait for PollProcesses.
// Zero/negative inputs fall back to library defaults. If max < initial,
// max is promoted to initial.
func WithPollWait(initial, max time.Duration) Option {
	return func(c *Client) error {
		if initial <= 0 {
			initial = defaultPollInitialWait
		}
		if max <= 0 {
			max = defaultPollMaxWait
		}
		if max < initial {
			max = initial
		}
		c.PollInitialWait = initial
		c.PollMaxWait = max
		return nil
	}
}

// NewClient builds a Client with sensible defaults and applies the provided
// options in order. Empty values in options are treated as explicit and may
// override defaults (e.g., MaxRetries=0 disables retries).
func NewClient(token, projectID string, opts ...Option) (*Client, error) {
	token = strings.TrimSpace(token)
	projectID = strings.TrimSpace(projectID)
	if token == "" {
		return nil, errors.New("API token is required")
	}
	if projectID == "" {
		return nil, errors.New("project ID is required")
	}

	c := &Client{
		BaseURL:         defaultBaseURL,
		Token:           token,
		ProjectID:       projectID,
		UserAgent:       defaultUserAgent,
		HTTPClient:      &http.Client{Timeout: defaultHTTPTimeout},
		MaxRetries:      defaultMaxRetries,
		InitialBackoff:  defaultInitialBackoff,
		MaxBackoff:      defaultMaxBackoff,
		PollInitialWait: defaultPollInitialWait,
		PollMaxWait:     defaultPollMaxWait,
	}

	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(c); err != nil {
			return nil, err
		}
	}

	// final normalization (in case WithBaseURL was not used)
	if !strings.HasSuffix(c.BaseURL, "/") {
		c.BaseURL += "/"
	}

	return c, nil
}

// PollProcesses polls one or more process IDs until they reach a terminal
// status or the overall poll budget (PollMaxWait) is exhausted.
// It returns a result for each input ID, preserving input order.
// Terminal statuses considered: "finished" and "failed".
//
// Errors from individual GET requests are ignored and retried on the next loop.
// Context cancellation (ctx.Done) aborts the whole poll with ctx.Err().
func (c *Client) PollProcesses(ctx context.Context, processIDs []string) ([]QueuedProcess, error) {
	start := time.Now()

	wait := c.PollInitialWait
	if wait <= 0 {
		wait = defaultPollInitialWait
	}
	maxWait := c.PollMaxWait
	if maxWait <= 0 {
		maxWait = defaultPollMaxWait
	}
	if maxWait < wait {
		maxWait = wait
	}

	processMap := make(map[string]QueuedProcess, len(processIDs))
	pending := make(map[string]struct{}, len(processIDs))

	for _, id := range processIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		processMap[id] = QueuedProcess{ProcessID: id, Status: "queued"}
		pending[id] = struct{}{}
	}

	// nothing to do? return in caller-provided order
	if len(pending) == 0 {
		results := make([]QueuedProcess, 0, len(processIDs))
		for _, id := range processIDs {
			if p, ok := processMap[id]; ok {
				results = append(results, p)
			}
		}
		return results, nil
	}

	for len(pending) > 0 {
		// respect overall max wait
		if time.Since(start) >= maxWait {
			break
		}

		for id := range pending {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			path := c.projectPath(fmt.Sprintf("processes/%s", id))
			var resp processResponse

			if err := c.doRequest(ctx, http.MethodGet, path, nil, &resp, nil); err != nil {
				// skip this id for now; try again next loop
				continue
			}

			proc := resp.ToQueuedProcess()
			processMap[id] = proc

			if proc.Status == "finished" || proc.Status == "failed" {
				delete(pending, id)
			}
		}

		if len(pending) == 0 {
			break
		}

		// compute a safe sleep that never goes negative/zero and never exceeds remaining budget
		remaining := maxWait - time.Since(start)
		if remaining <= 0 {
			break
		}
		sleep := wait
		if sleep > remaining {
			sleep = remaining
		}
		if sleep <= 0 {
			sleep = 10 * time.Millisecond // tiny floor to avoid spin
		}

		select {
		case <-time.After(sleep):
			// grow next wait, clipped by what remains
			remaining = maxWait - time.Since(start)
			next := wait * 2
			if next > remaining {
				next = remaining
			}
			if next <= 0 {
				next = 10 * time.Millisecond
			}
			wait = next
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// preserve input order in results
	results := make([]QueuedProcess, 0, len(processIDs))
	for _, id := range processIDs {
		if p, ok := processMap[id]; ok {
			results = append(results, p)
		}
	}
	return results, nil
}

// doWithRetry executes one HTTP operation with buffered body and retries
// according to the client's backoff policy. v is decoded into on success.
// method/path should be relative (e.g., "projects/<id>/...").
func (c *Client) doWithRetry(ctx context.Context, method, path string, body io.Reader, v any) error {
	var payload []byte
	if body != nil {
		b, err := io.ReadAll(body)
		if err != nil {
			return fmt.Errorf("buffer request body: %w", err)
		}
		payload = b
	}

	err := c.withExpBackoff(ctx, "request", func(_ int) error {
		var rdr io.Reader
		headers := make(http.Header)

		if payload != nil {
			rdr = bytes.NewReader(payload)
			headers.Set("Content-Type", "application/json")
		}

		if err := c.doRequest(ctx, method, path, rdr, v, headers); err != nil {
			return err
		}
		return nil
	}, nil)
	if err != nil {
		return err
	}
	return nil
}

// doRequest performs a single HTTP request (no retries).
// Body is sent as-is; if it's a bytes.Reader/strings.Reader/bytes.Buffer, we
// set Content-Length for nicer traces and potential connection reuse.
// If v is nil, the body is drained and discarded; otherwise it is decoded as JSON.
func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader, v any, headers http.Header) error {
	fullURL, err := url.JoinPath(c.BaseURL, path)
	if err != nil {
		return fmt.Errorf("join url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		// Best-effort Content-Length for common reader types
		if br, ok := body.(*bytes.Reader); ok {
			req.ContentLength = int64(br.Len())
		}
		if sr, ok := body.(*strings.Reader); ok {
			req.ContentLength = int64(sr.Len())
		}
		if bb, ok := body.(*bytes.Buffer); ok {
			req.ContentLength = int64(bb.Len())
		}
	}

	req.Header.Set("X-Api-Token", c.Token)
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	for k, vv := range headers {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, defaultErrCap))
		ae := apierr.Parse(slurp, resp.StatusCode)
		ae.Resp = resp
		return ae
	}

	// No target to decode into → nothing else to do
	if v == nil {
		// drain body to let Go reuse the connection
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	// Read full body once; classify empty vs truncated vs valid JSON
	b, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		// Server closed early (truncated) – bubble up for retry layer to decide.
		return fmt.Errorf("read response: %w", rerr)
	}

	if len(bytes.TrimSpace(b)) == 0 {
		// 204 or empty JSON body – treat as success.
		return nil
	}

	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

// withExpBackoff runs op with retries using exponential backoff + jitter.
// Semantics: MaxRetries is the number of *retries* after the initial attempt,
// so total attempts = MaxRetries + 1. A nil isRetryable defaults to apierr.IsRetryable.
// If ctx is canceled, the function returns ctx.Err().
func (c *Client) withExpBackoff(
	ctx context.Context,
	label string,
	op func(attempt int) error,
	isRetryable func(error) bool,
) error {
	if isRetryable == nil {
		isRetryable = apierr.IsRetryable
	}

	var lastErr error

	// Floors to avoid tight spins when caller sets zeros.
	initial := c.InitialBackoff
	if initial <= 0 {
		initial = 50 * time.Millisecond
	}
	max := c.MaxBackoff
	if max <= 0 {
		max = 2 * time.Second
	}
	backoff := initial

	for attempt := 0; ; attempt++ {
		// attempt is 0-based; pass it through as-is to op.
		if err := op(attempt); err == nil {
			return nil
		} else {
			lastErr = err
		}

		// If it's not retryable or we've exhausted retries, bail.
		// attempt counts completed attempts; allow up to MaxRetries retries.
		if !isRetryable(lastErr) || attempt >= c.MaxRetries {
			if label != "" {
				// attempt+1 = human-readable total attempts performed
				return fmt.Errorf("%s (attempt %d): %w", label, attempt+1, lastErr)
			}
			return lastErr
		}

		// jittered sleep capped at max; ensure positive delay
		delay := apierr.JitteredBackoff(backoff)
		if delay <= 0 {
			delay = time.Millisecond
		}
		if delay > max {
			delay = max
		}

		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
			// proceed to next retry
		case <-ctx.Done():
			// drain timer if needed
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			if label != "" {
				return fmt.Errorf("%s: context: %w", label, ctx.Err())
			}
			return ctx.Err()
		}
		// Best-effort stop; safe even if already fired.
		timer.Stop()

		// exponential growth capped at max
		backoff *= 2
		if backoff > max {
			backoff = max
		}
	}
}

// projectPath builds "projects/{id}/<suffix>" for project-scoped endpoints.
func (c *Client) projectPath(suffix string) string {
	return fmt.Sprintf("projects/%s/%s", c.ProjectID, suffix)
}
