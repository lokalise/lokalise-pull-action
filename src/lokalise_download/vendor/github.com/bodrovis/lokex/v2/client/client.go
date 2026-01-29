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

	"github.com/bodrovis/lokex/v2/internal/apierr"
	"golang.org/x/sync/errgroup"
)

const (
	// defaultBaseURL is the production Lokalise REST API v2 base.
	defaultBaseURL = "https://api.lokalise.com/api2/"

	// defaultUserAgent is sent on every request unless overridden via WithUserAgent.
	defaultUserAgent = "lokex/2.0.0"

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

	// Queued process statuses
	StatusQueued   = "queued"
	StatusFinished = "finished"
	StatusFailed   = "failed"
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

type pollResult struct {
	id   string
	proc QueuedProcess
	err  error
}

type countingReader struct {
	r io.Reader
	n int64
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
// A zero value disables the timeout.
func WithHTTPTimeout(d time.Duration) Option {
	return func(c *Client) error {
		if d < 0 {
			return errors.New("http timeout cannot be negative")
		}
		if c.HTTPClient == nil {
			c.HTTPClient = &http.Client{Timeout: defaultHTTPTimeout}
		}
		c.HTTPClient.Timeout = d
		return nil
	}
}

// WithMaxRetries sets how many *retries* to attempt after the initial try.
// Use 0 (or negative) to disable retries entirely.
func WithMaxRetries(n int) Option {
	return func(c *Client) error {
		if n < 0 {
			n = 0
		}
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
// options in order.
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
	c.BaseURL = strings.TrimSpace(c.BaseURL)
	if !strings.HasSuffix(c.BaseURL, "/") {
		c.BaseURL += "/"
	}

	return c, nil
}

// PollProcesses polls one or more Lokalise async process IDs until each reaches a
// terminal status ("finished" or "failed"), or until the overall polling budget
// (PollMaxWait) is exhausted.
//
// Ordering rules:
//   - Returns one result per NON-empty input ID
//   - Preserves the caller’s order
//   - Preserves duplicates (same ID can appear multiple times in the output)
//
// Error handling rules:
//   - Transient request errors do NOT abort polling; that ID stays pending and
//     will be retried in the next round.
//   - Non-retryable errors for an ID mark ONLY that process as "failed" and
//     remove it from pending; polling continues for other IDs.
//   - Context cancellation / deadline aborts the whole poll and returns ctx error.
//
// Implementation notes:
//   - Each polling round does parallel GETs with a fixed concurrency cap.
//   - We buffer the result channel so workers never block on send.
//   - We enforce an overall deadline via context.WithDeadline.
func (c *Client) PollProcesses(ctx context.Context, processIDs []string) ([]QueuedProcess, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	wait, maxWait := c.pollConfig()
	deadline := time.Now().Add(maxWait)

	// pollCtx enforces the polling budget (PollMaxWait). When it expires,
	// we should stop polling and return best-effort results (not an error),
	// unless the caller's ctx itself is canceled/deadline-exceeded.
	pollCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	ordered, processMap, pending := normalizeProcessIDs(processIDs)
	if len(pending) == 0 {
		return buildResults(ordered, processMap), nil
	}

	// Bound parallelism so we don't spam Lokalise or overload the client.
	const maxConcurrent = 6

	// Reuse a timer to avoid allocating time.After() on each round.
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	defer timer.Stop()

	for len(pending) > 0 {
		// Caller cancellation/deadline is a hard stop (real error).
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Poll budget expired: stop polling and return what we have.
		if pollCtx.Err() != nil {
			break
		}

		// One round: fetch all pending statuses concurrently (bounded).
		procs, errs := c.pollRound(pollCtx, pending, maxConcurrent)

		// If caller ctx died during the round, surface that (real error).
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Apply outcomes to processMap/pending (single goroutine mutates maps => no locks).
		applyRound(processMap, pending, procs, errs)

		if len(pending) == 0 {
			break
		}

		// If budget expired during the round, stop now (best-effort return).
		if pollCtx.Err() != nil {
			break
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}

		sleep := min(wait, remaining)
		if sleep <= 0 {
			sleep = 10 * time.Millisecond
		}

		if err := sleepWithTimer(pollCtx, timer, sleep); err != nil {
			// If caller ctx is canceled/deadline-exceeded -> error.
			if cerr := ctx.Err(); cerr != nil {
				return nil, cerr
			}
			// Otherwise it's our polling budget -> best-effort return.
			break
		}

		// Exponential backoff for next round, clipped to remaining budget.
		remaining = time.Until(deadline)
		next := min(wait*2, remaining)
		if next <= 0 {
			next = 10 * time.Millisecond
		}
		wait = next
	}

	return buildResults(ordered, processMap), nil
}

// pollConfig returns initial wait and overall max wait with safe defaults.
func (c *Client) pollConfig() (wait time.Duration, maxWait time.Duration) {
	// This might be an overkill as config should already
	// check these values but let's leave just in case.
	wait = c.PollInitialWait
	if wait <= 0 {
		wait = defaultPollInitialWait
	}

	maxWait = c.PollMaxWait
	if maxWait <= 0 {
		maxWait = defaultPollMaxWait
	}
	if maxWait < wait {
		maxWait = wait
	}
	return wait, maxWait
}

// normalizeProcessIDs trims inputs, preserves caller order (including duplicates),
// and returns:
//   - ordered: trimmed IDs in original order (empties kept as "")
//   - processMap: latest status per UNIQUE non-empty ID (seeded with StatusQueued)
//   - pending: set of UNIQUE non-empty IDs to poll
func normalizeProcessIDs(processIDs []string) (ordered []string, processMap map[string]QueuedProcess, pending map[string]struct{}) {
	ordered = make([]string, 0, len(processIDs))
	processMap = make(map[string]QueuedProcess, len(processIDs))
	pending = make(map[string]struct{}, len(processIDs))

	for _, raw := range processIDs {
		id := strings.TrimSpace(raw)
		ordered = append(ordered, id)
		if id == "" {
			continue
		}
		if _, ok := processMap[id]; !ok {
			processMap[id] = QueuedProcess{ProcessID: id, Status: StatusQueued}
		}
		pending[id] = struct{}{}
	}
	return
}

// pollRound performs one polling round for all currently pending IDs.
// It returns successful process statuses and per-ID errors.
// Workers never block on send because resCh is buffered to len(pending).
func (c *Client) pollRound(ctx context.Context, pending map[string]struct{}, maxConcurrent int) ([]QueuedProcess, map[string]error) {
	ids := make([]string, 0, len(pending))
	for id := range pending {
		ids = append(ids, id)
	}

	resCh := make(chan pollResult, len(ids))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrent)

	for _, id := range ids {
		cur := id
		g.Go(func() error {
			path := c.projectPath(fmt.Sprintf("processes/%s", cur))
			var resp processResponse
			if err := c.doRequest(gctx, http.MethodGet, path, nil, &resp, nil); err != nil {
				resCh <- pollResult{id: cur, err: err}
				return nil
			}
			resCh <- pollResult{id: cur, proc: resp.ToQueuedProcess()}
			return nil
		})
	}

	_ = g.Wait()
	close(resCh)

	procs := make([]QueuedProcess, 0, len(ids))
	errs := make(map[string]error)

	for r := range resCh {
		if r.err != nil {
			errs[r.id] = r.err
			continue
		}
		procs = append(procs, r.proc)
	}

	return procs, errs
}

// applyRound updates processMap/pending based on successful statuses and errors.
func applyRound(
	processMap map[string]QueuedProcess,
	pending map[string]struct{},
	procs []QueuedProcess,
	errs map[string]error,
) {
	// Successful statuses update the latest view and remove terminal IDs.
	for _, p := range procs {
		processMap[p.ProcessID] = p
		if p.Status == StatusFinished || p.Status == StatusFailed {
			delete(pending, p.ProcessID)
		}
	}

	// Errors: retryable stays pending; non-retryable fails and is removed.
	for id, err := range errs {
		// defensive; PollProcesses should return ctx.Err earlier
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			// don't mark failed; caller asked to stop the whole poll
			continue
		}

		if apierr.IsRetryable(err) {
			continue
		}

		processMap[id] = QueuedProcess{ProcessID: id, Status: StatusFailed}
		delete(pending, id)
	}
}

// buildResults reconstructs output preserving caller order and duplicates.
// Empty IDs are skipped.
func buildResults(ordered []string, processMap map[string]QueuedProcess) []QueuedProcess {
	out := make([]QueuedProcess, 0, len(ordered))
	for _, id := range ordered {
		if id == "" {
			continue
		}
		if p, ok := processMap[id]; ok {
			out = append(out, p)
		} else {
			out = append(out, QueuedProcess{ProcessID: id, Status: StatusQueued})
		}
	}
	return out
}

// sleepWithTimer waits for d or returns early on ctx cancellation.
// Timer is reused to avoid allocations.
func sleepWithTimer(ctx context.Context, timer *time.Timer, d time.Duration) error {
	if d <= 0 {
		d = 10 * time.Millisecond
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(d)

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// doWithRetry executes one HTTP operation and retries according to the client's
// backoff policy. If the body is seekable or provides a retryBodyFactory it is
// reused across attempts; otherwise it is buffered into memory once.
func (c *Client) doWithRetry(ctx context.Context, method, path string, body io.Reader, v any) error {
	headers := make(http.Header)
	if body != nil {
		headers.Set("Content-Type", "application/json")
	}

	// 1) Preferred: retryBodyFactory (fresh body each attempt).
	if f, ok := body.(retryBodyFactory); ok {
		return c.withExpBackoff(ctx, "request", func(_ int) error {
			rc, err := f.NewBody()
			if err != nil {
				return fmt.Errorf("create request body: %w", err)
			}
			// pass rc as-is (it is io.ReadCloser) so Transport can close it properly
			return c.doRequest(ctx, method, path, rc, v, headers)
		}, nil)
	}

	// 2) Seekable: rewind per attempt.
	// If it's also a Closer (e.g. *os.File), we must prevent net/http from closing it per attempt,
	// otherwise retries break. So we hide Close() and close once after all attempts.
	if rs, ok := body.(io.ReadSeeker); ok {
		if cl, ok := body.(io.Closer); ok {
			defer func() { _ = cl.Close() }()

			return c.withExpBackoff(ctx, "request", func(_ int) error {
				if _, err := rs.Seek(0, io.SeekStart); err != nil {
					return fmt.Errorf("rewind body: %w", err)
				}
				// Hide Close(): wrapper does NOT implement io.ReadCloser.
				rdr := struct{ io.Reader }{rs}
				return c.doRequest(ctx, method, path, rdr, v, headers)
			}, nil)
		}

		// ReadSeeker but not Closer: safe to pass directly.
		return c.withExpBackoff(ctx, "request", func(_ int) error {
			if _, err := rs.Seek(0, io.SeekStart); err != nil {
				return fmt.Errorf("rewind body: %w", err)
			}
			return c.doRequest(ctx, method, path, rs, v, headers)
		}, nil)
	}

	// 3) Fallback: buffer once (may allocate).
	var payload []byte
	if body != nil {
		b, err := io.ReadAll(body)
		if err != nil {
			return fmt.Errorf("buffer request body: %w", err)
		}
		if cbody, ok := body.(io.Closer); ok {
			_ = cbody.Close()
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

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	cr.n += int64(n)
	return n, err
}

// doRequest performs a single HTTP request (no retries).
// Body is sent as-is. If v is nil, the body is drained and discarded;
// otherwise it is decoded as JSON.
func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader, v any, headers http.Header) error {
	// Close request body ONLY if we fail before http.Client.Do().
	closeBody := func() {
		if body == nil {
			return
		}
		if cl, ok := body.(io.Closer); ok {
			_ = cl.Close()
		}
	}

	fullURL, err := url.JoinPath(c.BaseURL, path)
	if err != nil {
		closeBody()
		return fmt.Errorf("join url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		closeBody()
		return fmt.Errorf("create request: %w", err)
	}

	// Best-effort Content-Length for common reader types (helps traces; avoids chunked uploads).
	switch b := body.(type) {
	case *bytes.Reader:
		req.ContentLength = int64(b.Len())
	case *strings.Reader:
		req.ContentLength = int64(b.Len())
	}

	req.Header.Set("X-Api-Token", c.Token)
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")

	for k, vv := range headers {
		if len(vv) == 0 {
			continue
		}
		req.Header.Del(k)
		req.Header[k] = append([]string(nil), vv...)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		// after Do() net/http already handled closing the request body.
		return fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Non-2xx: parse as APIError with a bounded snippet for debugging.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, defaultErrCap))
		// Drain the rest to maximize chances of connection reuse.
		_, _ = io.Copy(io.Discard, resp.Body)

		ae := apierr.Parse(slurp, resp.StatusCode)
		// Keep headers/status accessible; body is already consumed (don't read it).
		ae.Resp = resp
		return ae
	}

	// No target to decode into → drain body and return.
	if v == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	// Stream JSON decode to avoid buffering potentially large bodies,
	// but keep good error semantics for retry layer.
	cr := &countingReader{r: resp.Body}
	dec := json.NewDecoder(cr)

	if err := dec.Decode(v); err != nil {
		// Empty body (204 or some 200s) is fine.
		if errors.Is(err, io.EOF) {
			return nil
		}

		// Decoder returns io.ErrUnexpectedEOF for truncated JSON.
		// Distinguish "transport truncation" vs "server sent broken JSON".
		if errors.Is(err, io.ErrUnexpectedEOF) {
			// If Content-Length is known and we read less than promised,
			// treat as a truncated response -> retryable for higher layer.
			if resp.ContentLength > 0 && cr.n < resp.ContentLength {
				return fmt.Errorf("read response: %w", io.ErrUnexpectedEOF)
			}
			// Otherwise behave like json.Unmarshal on incomplete JSON:
			// stable message + NOT retryable.
			return fmt.Errorf("decode response: unexpected end of JSON input")
		}

		// Other decode errors (SyntaxError, type errors, etc.) -> non-retryable by default.
		return fmt.Errorf("decode response: %w", err)
	}

	// Strict trailing junk detection (optional). Keep if you want.
	if err := dec.Decode(new(struct{})); err == nil {
		return fmt.Errorf("decode response: trailing data")
	} else if !errors.Is(err, io.EOF) {
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

	maxRetries, totalAttempts := c.MaxRetries, c.MaxRetries+1
	backoff, maxBackoff := normalizeBackoff(c.InitialBackoff, c.MaxBackoff)

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
			return wrapCtxErr(label, attempt, totalAttempts, err)
		}

		err := op(attempt)
		if err == nil {
			return nil
		}

		// If ctx got canceled during the attempt, surface that cleanly.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return wrapCtxErr(label, attempt, totalAttempts, ctxErr)
		}

		// Not retryable or retries exhausted.
		if !isRetryable(err) || attempt >= maxRetries {
			return wrapErr(label, attempt, totalAttempts, err)
		}

		// Sleep with jittered backoff, capped.
		delay := apierr.JitteredBackoff(backoff)
		if delay <= 0 {
			delay = time.Millisecond
		}
		if delay > maxBackoff {
			delay = maxBackoff
		}

		if err := sleepWithTimer(ctx, timer, delay); err != nil {
			return wrapCtxErr(label, attempt, totalAttempts, err)
		}

		// Exponential growth capped.
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func normalizeBackoff(initial, max time.Duration) (time.Duration, time.Duration) {
	if initial <= 0 {
		initial = 50 * time.Millisecond
	}
	if max <= 0 {
		max = 2 * time.Second
	}
	if max < initial {
		max = initial
	}
	return initial, max
}

func wrapErr(label string, attempt, total int, err error) error {
	if label == "" {
		return err
	}
	return fmt.Errorf("%s (attempt %d/%d): %w", label, attempt+1, total, err)
}

func wrapCtxErr(label string, attempt, total int, err error) error {
	if label == "" {
		return err
	}
	return fmt.Errorf("%s (attempt %d/%d): context: %w", label, attempt+1, total, err)
}

// projectPath builds "projects/{id}/<suffix>" for project-scoped endpoints.
func (c *Client) projectPath(suffix string) string {
	return fmt.Sprintf("projects/%s/%s", url.PathEscape(c.ProjectID), suffix)
}
