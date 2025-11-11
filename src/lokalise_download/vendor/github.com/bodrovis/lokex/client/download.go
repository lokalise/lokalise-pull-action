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
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/bodrovis/lokex/internal/apierr"
	"github.com/bodrovis/lokex/internal/utils"
	"github.com/bodrovis/lokex/internal/zipx"
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
	// copy to avoid mutating caller's map
	body := make(map[string]any, len(params))
	maps.Copy(body, params)

	buf, err := utils.EncodeJSONBody(body)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}

	bundleURL, err := fetch(ctx, buf)
	if err != nil {
		return "", err
	}

	if err := d.DownloadAndUnzip(ctx, bundleURL, unzipTo); err != nil {
		return "", err
	}

	return bundleURL, nil
}

// FetchBundleAsync kicks off an async export and polls until it yields a
// download URL. On success it returns the final download_url.
func (d *Downloader) FetchBundleAsync(ctx context.Context, body io.Reader) (string, error) {
	var resp AsyncDownloadResponse
	path := d.client.projectPath("files/async-download")

	if err := d.client.doWithRetry(ctx, http.MethodPost, path, body, &resp); err != nil {
		return "", fmt.Errorf("fetch bundle async: %w", err)
	}
	if resp.ProcessID == "" {
		return "", fmt.Errorf("fetch bundle async: empty process id")
	}

	// Poll this single process until it finishes or times out.
	results, err := d.client.PollProcesses(ctx, []string{resp.ProcessID})
	if err != nil {
		return "", fmt.Errorf("fetch bundle async: poll processes: %w", err)
	}
	if len(results) == 0 {
		return "", fmt.Errorf("fetch bundle async: no process results returned")
	}

	completed := results[0]
	if completed.Status == "finished" && completed.DownloadURL != "" {
		return completed.DownloadURL, nil
	}

	return "", fmt.Errorf(
		"fetch bundle async: process %s did not finish (status=%s)",
		completed.ProcessID,
		completed.Status,
	)
}

// FetchBundle performs a synchronous export and returns the bundle_url.
func (d *Downloader) FetchBundle(ctx context.Context, body io.Reader) (string, error) {
	var bundle DownloadBundle
	path := d.client.projectPath("files/download")

	if err := d.client.doWithRetry(ctx, http.MethodPost, path, body, &bundle); err != nil {
		return "", fmt.Errorf("fetch bundle: %w", err)
	}
	if bundle.BundleURL == "" {
		return "", fmt.Errorf("fetch bundle: empty bundle url")
	}
	return bundle.BundleURL, nil
}

// DownloadAndUnzip downloads the zip from bundleURL with retry/backoff,
// validates that it's a well-formed zip, and unzips it into destDir with a
// series of safety checks (zip-slip, entry count, size caps, no symlinks/devs).
func (d *Downloader) DownloadAndUnzip(ctx context.Context, bundleURL, destDir string) error {
	if strings.TrimSpace(bundleURL) == "" {
		return fmt.Errorf("download: empty bundle url")
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("download: create dest: %w", err)
	}

	// temp ZIP (system temp dir)
	tmpZip, err := os.CreateTemp("", "lokex-*.zip")
	if err != nil {
		return fmt.Errorf("download: create temp zip: %w", err)
	}
	tmpPath := tmpZip.Name()
	_ = tmpZip.Close() // we'll reopen inside downloadOnce()
	defer func() { _ = os.Remove(tmpPath) }()

	ua := ""
	if d.client != nil {
		ua = d.client.UserAgent
	}

	// Retry the HTTP fetch + quick zip validation until success or policy expires.
	if err := d.client.withExpBackoff(ctx, "download", func(_ int) error {
		if err := d.downloadOnce(ctx, bundleURL, tmpPath, ua); err != nil {
			return err
		}
		// validate it's a real zip; if not, return ErrUnexpectedEOF to trigger retry
		return zipx.Validate(tmpPath)
	}, nil); err != nil {
		return err
	}

	if err := zipx.Unzip(tmpPath, destDir, zipx.DefaultPolicy()); err != nil {
		return fmt.Errorf("unzip: %w", err)
	}
	return nil
}

// downloadOnce performs a single GET of the bundle and writes it to destPath.
// It sets Accept headers appropriate for zips and optionally propagates a
// User-Agent. It verifies Content-Length when available to detect truncation.
func (d *Downloader) downloadOnce(ctx context.Context, url, destPath, ua string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	// avoid transparent compression; we want raw bytes on disk
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Accept", "application/zip, application/octet-stream, */*")

	hc := d.client.HTTPClient
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, defaultErrCap))
		return &apierr.APIError{
			Status:  resp.StatusCode,
			Message: strings.TrimSpace(string(slurp)),
			Code:    resp.StatusCode,
		}
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create temp zip: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	var want int64 = -1
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		if n, perr := strconv.ParseInt(cl, 10, 64); perr == nil && n >= 0 {
			want = n
		}
	}

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		return fmt.Errorf("write zip: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("flush zip: %w", err)
	}

	// trigger retry if server cut us short
	if want >= 0 && n != want {
		return fmt.Errorf("incomplete download: got %d of %d: %w", n, want, io.ErrUnexpectedEOF)
	}
	return nil
}
