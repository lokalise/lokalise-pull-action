// Package client provides a wrapper around the Lokalise API that the
// upload/download packages depend on. It handles base URL normalization,
// authentication, retry with exponential backoff,
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
	defaultUserAgent = "lokex/1.1.0"

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

type retryBodyFactory interface {
	// NewBody must return a fresh body for each attempt.
	// Caller (doWithRetry) will Close() it.
	NewBody() (io.ReadCloser, error)
}

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
// It returns a result for each non-empty input ID, preserving the caller’s order.
// Terminal statuses: "finished" and "failed".
//
// Each polling round fetches statuses with bounded concurrency.
// Errors from individual GET requests are ignored for that round and retried on
// subsequent rounds. Context cancellation aborts the whole poll.
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

	deadline := start.Add(maxWait)

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

	// Concurrency cap (tune as you like)
	const maxConcurrent = 8
	sem := make(chan struct{}, maxConcurrent)

	type pollResult struct {
		id   string
		proc QueuedProcess
		err  error
	}

	// Reusable timer (avoid time.After allocations)
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	defer timer.Stop()

	for len(pending) > 0 {
		// Respect overall poll budget
		if time.Now().After(deadline) {
			break
		}

		// Snapshot pending IDs for this round (so we can safely delete from pending later)
		ids := make([]string, 0, len(pending))
		for id := range pending {
			ids = append(ids, id)
		}

		resCh := make(chan pollResult, len(ids))

		// Launch requests with bounded concurrency
		for _, id := range ids {
			select {
			case sem <- struct{}{}:
				// ok
			case <-ctx.Done():
				return nil, ctx.Err()
			}

			go func(id string) {
				defer func() { <-sem }()

				path := c.projectPath(fmt.Sprintf("processes/%s", id))
				var resp processResponse
				err := c.doRequest(ctx, http.MethodGet, path, nil, &resp, nil)
				if err != nil {
					resCh <- pollResult{id: id, err: err}
					return
				}
				resCh <- pollResult{id: id, proc: resp.ToQueuedProcess()}
			}(id)
		}

		// Collect results (single goroutine mutates maps → no locks)
		for i := 0; i < len(ids); i++ {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case r := <-resCh:
				if r.err != nil {
					// ignore request-level errors; keep it pending for the next round
					continue
				}
				processMap[r.id] = r.proc
				if r.proc.Status == "finished" || r.proc.Status == "failed" {
					delete(pending, r.id)
				}
			}
		}
		close(resCh)

		if len(pending) == 0 {
			break
		}

		// Sleep with growing interval, clipped to remaining budget
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}

		sleep := wait
		if sleep > remaining {
			sleep = remaining
		}
		if sleep <= 0 {
			sleep = 10 * time.Millisecond
		}

		// Reset timer safely
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(sleep)

		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		// Grow wait for next round (exponential), still bounded
		remaining = time.Until(deadline)
		next := wait * 2
		if next > remaining {
			next = remaining
		}
		if next <= 0 {
			next = 10 * time.Millisecond
		}
		wait = next
	}

	// Preserve input order in results
	results := make([]QueuedProcess, 0, len(processIDs))
	for _, id := range processIDs {
		if p, ok := processMap[id]; ok {
			results = append(results, p)
		}
	}
	return results, nil
}

// doWithRetry executes one HTTP operation and retries according to the client's
// backoff policy. If the body is seekable or provides a retryBodyFactory it is
// reused across attempts; otherwise it is buffered into memory once.
func (c *Client) doWithRetry(ctx context.Context, method, path string, body io.Reader, v any) error {
	headers := make(http.Header)
	if body != nil {
		headers.Set("Content-Type", "application/json")
	}

	if f, ok := body.(retryBodyFactory); ok {
		return c.withExpBackoff(ctx, "request", func(_ int) error {
			rc, err := f.NewBody()
			if err != nil {
				return fmt.Errorf("create request body: %w", err)
			}
			defer func() {
				if cerr := rc.Close(); err == nil && cerr != nil {
					err = cerr
				}
			}()

			return c.doRequest(ctx, method, path, rc, v, headers)
		}, nil)
	}

	if rs, ok := body.(io.ReadSeeker); ok {
		return c.withExpBackoff(ctx, "request", func(_ int) error {
			if _, err := rs.Seek(0, io.SeekStart); err != nil {
				return fmt.Errorf("rewind body: %w", err)
			}
			return c.doRequest(ctx, method, path, rs, v, headers)
		}, nil)
	}

	var payload []byte
	if body != nil {
		b, err := io.ReadAll(body)
		if err != nil {
			return fmt.Errorf("buffer request body: %w", err)
		}
		payload = b
	}

	return c.withExpBackoff(ctx, "request", func(_ int) error {
		var rdr io.Reader
		if payload != nil {
			rdr = bytes.NewReader(payload)
		}
		return c.doRequest(ctx, method, path, rdr, v, headers)
	}, nil)
}

// doRequest performs a single HTTP request (no retries).
// Body is sent as-is; if it's a bytes.Reader/strings.Reader/bytes.Buffer, we
// set Content-Length for nicer traces and fewer chunked uploads.
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
		if len(vv) > 0 {
			req.Header.Set(k, vv[len(vv)-1])
		}
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, defaultErrCap))
		_, _ = io.Copy(io.Discard, resp.Body)

		ae := apierr.Parse(slurp, resp.StatusCode)

		resp.Body = io.NopCloser(bytes.NewReader(slurp))
		resp.ContentLength = int64(len(slurp))
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
// MaxRetries is the number of *retries* after the initial attempt (total attempts = MaxRetries+1).
// If isRetryable is nil, apierr.IsRetryable is used.
// If ctx is canceled or its deadline is exceeded, ctx.Err() is returned (wrapped with label when provided).
func (c *Client) withExpBackoff(
	ctx context.Context,
	label string,
	op func(attempt int) error,
	isRetryable func(error) bool,
) error {
	if isRetryable == nil {
		isRetryable = apierr.IsRetryable
	}

	// Floors to avoid tight spins when caller sets zeros.
	backoff := c.InitialBackoff
	if backoff <= 0 {
		backoff = 50 * time.Millisecond
	}
	max := c.MaxBackoff
	if max <= 0 {
		max = 2 * time.Second
	}

	// Reuse a single timer to avoid allocations on each retry.
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	defer timer.Stop()

	for attempt := 0; ; attempt++ {
		// Bail fast if caller already canceled / deadline exceeded.
		if err := ctx.Err(); err != nil {
			if label != "" {
				return fmt.Errorf("%s: context: %w", label, err)
			}
			return err
		}

		// attempt is 0-based; pass it through as-is to op.
		if err := op(attempt); err == nil {
			return nil
		} else {
			// If ctx got canceled during the attempt, surface that cleanly.
			if ctxErr := ctx.Err(); ctxErr != nil {
				if label != "" {
					return fmt.Errorf("%s: context: %w", label, ctxErr)
				}
				return ctxErr
			}

			// Not retryable or retries exhausted.
			if !isRetryable(err) || attempt >= c.MaxRetries {
				if label != "" {
					return fmt.Errorf("%s (attempt %d): %w", label, attempt+1, err)
				}
				return err
			}
		}

		// Jittered sleep capped at max; ensure positive delay.
		delay := apierr.JitteredBackoff(backoff)
		if delay <= 0 {
			delay = time.Millisecond
		}
		if delay > max {
			delay = max
		}

		// Reset timer safely.
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(delay)

		select {
		case <-timer.C:
			// proceed to next retry
		case <-ctx.Done():
			if label != "" {
				return fmt.Errorf("%s: context: %w", label, ctx.Err())
			}
			return ctx.Err()
		}

		// Exponential growth capped at max.
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
