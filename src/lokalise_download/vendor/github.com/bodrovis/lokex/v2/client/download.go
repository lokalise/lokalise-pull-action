// Package client: downloader for Lokalise export bundles.
//
// This file provides a small helper around the two download flows Lokalise
// supports:
//
//   - Synchronous download: POST /files/download → returns a bundle_url (zip).
//   - Asynchronous download: POST /files/async-download → returns process_id,
//     which is then polled via /processes/{id} until it yields a download_url.
//
// The downloader will fetch the bundle URL (sync or async), download the zip
// with retry/backoff, validate it's a real zip, and then safely unzip into the
// provided destination directory with zip-slip and size guards.
package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/bodrovis/lokex/v2/internal/apierr"
	"github.com/bodrovis/lokex/v2/internal/utils"
	"github.com/bodrovis/lokex/v2/internal/zipx"
)

// Downloader wraps a *Client to perform Lokalise file exports (downloads).
// Construct with NewDownloader; the embedded client must be non-nil.
type Downloader struct {
	client *Client
}

// DownloadBundle is the minimal response payload returned by
// POST /files/download.
type DownloadBundle struct {
	BundleURL string `json:"bundle_url"`
}

// AsyncDownloadResponse is the minimal response payload returned by
// POST /files/async-download.
type AsyncDownloadResponse struct {
	ProcessID string `json:"process_id"`
}

// DownloadParams represents the JSON body for /files/download and
// /files/async-download. It's a thin alias so callers can pass a map and keep
// strong naming at call sites.
type DownloadParams map[string]any

// FetchFunc abstracts the "get me a bundle URL" step so Download and
// DownloadAsync share the same pipeline (doDownload).
type FetchFunc func(ctx context.Context, body io.Reader) (string, error)

// NewDownloader creates a new Downloader bound to c.
// c must be non-nil; it is used for HTTP, retry/backoff, and polling.
func NewDownloader(c *Client) *Downloader {
	if c == nil {
		panic("lokex/client: nil Client passed to NewDownloader")
	}
	return &Downloader{
		client: c,
	}
}

// Download performs a synchronous export:
//
//  1. POST /files/download with params
//  2. Receive bundle_url
//  3. Download the zip (with retry/backoff), validate, unzip to unzipTo
//
// Returns the bundle_url on success.
func (d *Downloader) Download(ctx context.Context, unzipTo string, params DownloadParams) (string, error) {
	if d == nil || d.client == nil {
		return "", errors.New("download: downloader/client is nil")
	}
	return d.doDownload(ctx, unzipTo, params, d.FetchBundle)
}

// DownloadAsync performs an asynchronous export:
//
//  1. POST /files/async-download with params to get process_id
//  2. Poll /processes/{id} until status is finished
//  3. Receive download_url from the finished process
//  4. Download the zip (with retry/backoff), validate, unzip to unzipTo
//
// Returns the final download_url on success.
func (d *Downloader) DownloadAsync(ctx context.Context, unzipTo string, params DownloadParams) (string, error) {
	if d == nil || d.client == nil {
		return "", errors.New("download: downloader/client is nil")
	}
	return d.doDownload(ctx, unzipTo, params, d.FetchBundleAsync)
}

// doDownload is the shared pipeline for both sync and async flows.
// It builds the JSON body, calls fetch() to obtain the bundle URL, downloads
// and validates the zip, and unzips into unzipTo. The returned string is the
// bundle URL used (sync: bundle_url; async: download_url).
func (d *Downloader) doDownload(
	ctx context.Context,
	unzipTo string,
	params DownloadParams,
	fetch FetchFunc,
) (string, error) {
	if d == nil || d.client == nil {
		return "", errors.New("download: downloader/client is nil")
	}
	if fetch == nil {
		return "", errors.New("download: fetch func is nil")
	}
	if strings.TrimSpace(unzipTo) == "" {
		return "", errors.New("download: unzipTo is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// copy to avoid mutating caller's map
	var body map[string]any
	if len(params) > 0 {
		body = make(map[string]any, len(params))
		maps.Copy(body, params)
	} else {
		body = map[string]any{}
	}

	rdr, err := utils.EncodeJSONBody(body)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}

	bundleURL, err := fetch(ctx, rdr)
	if err != nil {
		return "", err
	}

	if err := d.DownloadAndUnzip(ctx, bundleURL, unzipTo); err != nil {
		return "", err
	}

	return bundleURL, nil
}

// FetchBundleAsync kicks off an async export (POST /files/async-download) and polls
// until the process yields a terminal status. On success it returns download_url.
func (d *Downloader) FetchBundleAsync(ctx context.Context, body io.Reader) (string, error) {
	if d == nil || d.client == nil {
		return "", fmt.Errorf("fetch bundle async: nil downloader/client")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("fetch bundle async: context: %w", err)
	}
	if body == nil {
		return "", fmt.Errorf("fetch bundle async: nil request body")
	}

	// 1) Kick off async export -> get process_id.
	var kickoff AsyncDownloadResponse
	path := d.client.projectPath("files/async-download")

	if err := d.client.doWithRetry(ctx, http.MethodPost, path, body, &kickoff); err != nil {
		return "", fmt.Errorf("fetch bundle async: %w", err)
	}

	pid := strings.TrimSpace(kickoff.ProcessID)
	if pid == "" {
		return "", fmt.Errorf("fetch bundle async: empty process id")
	}

	// 2) Poll this single process until terminal or ctx/poll budget expires.
	results, err := d.client.PollProcesses(ctx, []string{pid})
	if err != nil {
		return "", fmt.Errorf("fetch bundle async: poll processes: %w", err)
	}
	if len(results) == 0 {
		return "", fmt.Errorf("fetch bundle async: no process results returned (process_id=%s)", pid)
	}

	p := results[0]

	// 3) Interpret result.
	switch p.Status {
	case StatusFinished:
		u := strings.TrimSpace(p.DownloadURL)
		if u == "" {
			return "", fmt.Errorf("fetch bundle async: process %s finished but download_url is empty", p.ProcessID)
		}
		return u, nil

	case StatusFailed:
		return "", fmt.Errorf("fetch bundle async: process %s failed", p.ProcessID)

	default:
		// Usually means we ran out of polling budget (PollMaxWait) but ctx might still be alive,
		// or Lokalise is slow and never reached terminal before our poll deadline.
		return "", fmt.Errorf("fetch bundle async: process %s did not finish (status=%s)", p.ProcessID, p.Status)
	}
}

// FetchBundle performs a synchronous export (POST /files/download) and returns bundle_url.
func (d *Downloader) FetchBundle(ctx context.Context, body io.Reader) (string, error) {
	if d == nil || d.client == nil {
		return "", fmt.Errorf("fetch bundle: nil downloader/client")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("fetch bundle: context: %w", err)
	}
	if body == nil {
		return "", fmt.Errorf("fetch bundle: nil request body")
	}

	var bundle DownloadBundle
	path := d.client.projectPath("files/download")

	if err := d.client.doWithRetry(ctx, http.MethodPost, path, body, &bundle); err != nil {
		return "", fmt.Errorf("fetch bundle: %w", err)
	}

	url := strings.TrimSpace(bundle.BundleURL)
	if url == "" {
		return "", fmt.Errorf("fetch bundle: empty bundle url")
	}
	return url, nil
}

// DownloadAndUnzip downloads the zip from bundleURL with retry/backoff,
// validates that it's a well-formed zip, and unzips it into destDir with a
// series of safety checks (zip-slip, entry count, size caps, no symlinks/devs).
func (d *Downloader) DownloadAndUnzip(ctx context.Context, bundleURL, destDir string) error {
	if d == nil || d.client == nil || d.client.HTTPClient == nil {
		return fmt.Errorf("download: nil client")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	bundleURL = strings.TrimSpace(bundleURL)
	if bundleURL == "" {
		return fmt.Errorf("download: empty bundle url")
	}
	bundleURL, err := validateBundleURL(bundleURL)
	if err != nil {
		return err
	}
	destDir = strings.TrimSpace(destDir)
	if destDir == "" {
		return fmt.Errorf("download: empty dest dir")
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("download: create dest: %w", err)
	}

	// Temp dir per download attempt group. Easy cleanup, no broken files left behind.
	tmpDir, err := os.MkdirTemp("", "lokex-zip-*")
	if err != nil {
		return fmt.Errorf("download: create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	tmpPath := filepath.Join(tmpDir, "bundle.zip")

	ua := d.client.UserAgent

	// Retry the HTTP fetch + quick zip validation until success or policy expires.
	if err := d.client.withExpBackoff(ctx, "download", func(_ int) error {
		if err := d.downloadOnce(ctx, bundleURL, tmpPath, ua); err != nil {
			return err
		}
		if err := zipx.Validate(tmpPath); err != nil {
			// keep wrapping — errors.Is(... io.ErrUnexpectedEOF) still works through wrapping
			return fmt.Errorf("validate zip: %w", err)
		}
		return nil
	}, nil); err != nil {
		return err
	}

	if err := zipx.Unzip(tmpPath, destDir, zipx.DefaultPolicy()); err != nil {
		return fmt.Errorf("unzip: %w", err)
	}
	return nil
}

// downloadOnce performs a single GET of the bundle and writes it to destPath.
// It writes into a temp file first and renames it on success, so partial downloads
// never leave broken zips at destPath.
func (d *Downloader) downloadOnce(ctx context.Context, urlStr, destPath, ua string) error {
	httpc, urlStr, destPath, err := d.downloadOncePrecheck(ctx, urlStr, destPath)
	if err != nil {
		return err
	}

	resp, err := d.doDownloadRequest(ctx, httpc, urlStr, ua)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	// Non-2xx: read a capped snippet for an APIError and bail.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, defaultErrCap))
		_, _ = io.Copy(io.Discard, resp.Body)
		return apierr.Parse(slurp, resp.StatusCode)
	}

	return writeHTTPBodyAtomically(destPath, resp.Body, resp.ContentLength)
}

// downloadOncePrecheck validates inputs and extracts the http.Client.
// Keeping this separate makes downloadOnce small and avoids nil-panics.
func (d *Downloader) downloadOncePrecheck(ctx context.Context, urlStr, destPath string) (*http.Client, string, string, error) {
	if d == nil || d.client == nil || d.client.HTTPClient == nil {
		return nil, "", "", fmt.Errorf("download: nil http client")
	}
	if ctx == nil {
		return nil, "", "", fmt.Errorf("download: nil context")
	}
	if cerr := ctx.Err(); cerr != nil {
		return nil, "", "", cerr
	}

	urlStr = strings.TrimSpace(urlStr)
	destPath = strings.TrimSpace(destPath)
	if urlStr == "" {
		return nil, "", "", fmt.Errorf("download: empty url")
	}
	if destPath == "" {
		return nil, "", "", fmt.Errorf("download: empty dest path")
	}

	return d.client.HTTPClient, urlStr, destPath, nil
}

// doDownloadRequest builds and executes the GET request with headers tuned for zip bytes.
func (d *Downloader) doDownloadRequest(ctx context.Context, httpc *http.Client, urlStr, ua string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	// Avoid transparent compression; we want raw zip bytes on disk.
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Accept", "application/zip, application/octet-stream, */*")

	resp, err := httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	return resp, nil
}

// writeHTTPBodyAtomically writes src into a temp file next to destPath and renames on success.
// If wantLen >= 0, it checks for truncation and returns io.ErrUnexpectedEOF on mismatch.
func writeHTTPBodyAtomically(destPath string, src io.Reader, wantLen int64) (err error) {
	dir := filepath.Dir(destPath)
	prefix := filepath.Base(destPath) + ".part-"

	tmp, err := os.CreateTemp(dir, prefix)
	if err != nil {
		return fmt.Errorf("create temp zip: %w", err)
	}
	tmpName := tmp.Name()

	defer func() {
		_ = tmp.Close()
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()

	n, err := io.Copy(tmp, src)
	if err != nil {
		return fmt.Errorf("write zip: %w", err)
	}

	// Detect truncation only when server told us exact length.
	if wantLen >= 0 && n != wantLen {
		return fmt.Errorf("incomplete download: got %d of %d: %w", n, wantLen, io.ErrUnexpectedEOF)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close zip: %w", err)
	}

	// On Windows, rename over existing file is flaky; remove first.
	_ = os.Remove(destPath)
	if err := os.Rename(tmpName, destPath); err != nil {
		return fmt.Errorf("finalize zip: %w", err)
	}

	return nil
}

func validateBundleURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("download: empty url")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("download: bad url: %w", err)
	}

	// Strict mode: only https.
	if !strings.EqualFold(u.Scheme, "https") {
		return "", fmt.Errorf("download: unsupported url scheme %q", u.Scheme)
	}

	if u.Host == "" {
		return "", fmt.Errorf("download: url has empty host")
	}

	// Don’t allow credentials in URL.
	if u.User != nil {
		return "", fmt.Errorf("download: url must not contain userinfo")
	}

	// Optional: reject fragments (usually useless for downloads).
	if u.Fragment != "" {
		return "", fmt.Errorf("download: url must not contain fragment")
	}

	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", fmt.Errorf("download: url has empty hostname")
	}
	if host == "localhost" {
		return "", fmt.Errorf("download: localhost is not allowed")
	}
	if strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".internal") {
		return "", fmt.Errorf("download: local/internal hostname is not allowed")
	}

	// Block IP literals in private/loopback/etc ranges.
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return "", fmt.Errorf("download: ip %s is not allowed", ip.String())
		}
	}

	// Normalize (drops weird stuff like empty path normalization).
	return u.String(), nil
}

func isBlockedIP(ip net.IP) bool {
	ip = normalizeIP(ip)
	if ip == nil {
		return true
	}

	// obvious badness
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	// private ranges (v4 + v6 ULA)
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func normalizeIP(ip net.IP) net.IP {
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	if v6 := ip.To16(); v6 != nil {
		return v6
	}
	return nil
}

var blockedNets = []*net.IPNet{
	mustCIDR("10.0.0.0/8"),
	mustCIDR("172.16.0.0/12"),
	mustCIDR("192.168.0.0/16"),
	mustCIDR("127.0.0.0/8"),
	mustCIDR("169.254.0.0/16"), // link-local v4
	mustCIDR("::1/128"),
	mustCIDR("fe80::/10"), // link-local v6
	mustCIDR("fc00::/7"),  // unique local v6
	mustCIDR("::/128"),    // unspecified v6
	mustCIDR("ff00::/8"),  // multicast v6
}

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}
