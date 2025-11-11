// Package client: uploader for Lokalise file imports.
//
// This file implements the upload side of lokex:
//   - POST /files/upload with a JSON body that includes either a filename
//     (we'll read & base64 it) or an explicit base64 "data" field.
//   - Optionally poll the returned process until it finishes, or return
//     immediately with the process id if polling is disabled.
package client

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bodrovis/lokex/internal/utils"
)

// Uploader wraps a *Client to perform Lokalise file uploads.
// Construct with NewUploader; the embedded client must be non-nil.
type Uploader struct {
	client *Client
}

// UploadParams represents the JSON body for /files/upload.
// Callers typically provide:
//
//	filename (string) – required; path to a local file
//	lang_iso (string) – base language code
//
// You may also set "data" yourself (string base64 or []byte); if omitted,
// Upload will read the file and base64-encode it for you.
type UploadParams map[string]any

// UploadResponse mirrors the minimal shape we expect from /files/upload.
type UploadResponse struct {
	Process struct {
		ProcessID string `json:"process_id"`
	} `json:"process"`
}

// NewUploader creates a new Uploader bound to c.
func NewUploader(c *Client) *Uploader {
	return &Uploader{
		client: c,
	}
}

// Upload uploads a file to Lokalise using /files/upload.
// Behavior:
//  1. Validates and cleans the "filename" param, ensures it's a regular file.
//  2. If "data" is absent, reads the file and base64-encodes it (StdEncoding).
//     If "data" is present as []byte, it is base64-encoded; if string, it is
//     used as-is (assumed base64).
//  3. Sends POST with retry/backoff via the client's doWithRetry.
//  4. Returns the server-provided process id.
//
// If poll is true, it will call PollProcesses on that process and only return
// when the process reaches "finished" (otherwise it errors). If poll is false,
// it returns immediately after kickoff with the process id.
func (u *Uploader) Upload(ctx context.Context, params UploadParams, poll bool) (string, error) {
	body, cleanPath, err := cloneAndValidateParams(params)
	if err != nil {
		return "", err
	}

	if err := ensureFileIsRegular(cleanPath); err != nil {
		return "", err
	}

	if err := ensureBase64Data(body, cleanPath); err != nil {
		return "", err
	}

	buf, err := utils.EncodeJSONBody(body)
	if err != nil {
		return "", fmt.Errorf("upload: encode body: %w", err)
	}

	processID, err := u.kickoffUpload(ctx, buf)
	if err != nil {
		return "", err
	}

	// caller can opt-out of polling
	if !poll {
		return processID, nil
	}

	return u.pollUntilFinished(ctx, processID)
}

// cloneAndValidateParams copies user params and extracts a clean file path.
func cloneAndValidateParams(params UploadParams) (map[string]any, string, error) {
	// copy to avoid mutating caller's map
	body := make(map[string]any, len(params)+1)
	maps.Copy(body, params)

	raw, ok := body["filename"]
	if !ok {
		return nil, "", fmt.Errorf("upload: missing 'filename' param")
	}
	name, ok := raw.(string)
	if !ok || strings.TrimSpace(name) == "" {
		return nil, "", fmt.Errorf("upload: 'filename' must be a non-empty string")
	}
	cleanPath := filepath.Clean(name)
	return body, cleanPath, nil
}

// ensureFileIsRegular stats the path and rejects directories / missing files.
func ensureFileIsRegular(cleanPath string) error {
	if fi, err := os.Stat(cleanPath); err != nil {
		return fmt.Errorf("upload: stat %q: %w", cleanPath, err)
	} else if fi.IsDir() {
		return fmt.Errorf("upload: %q is a directory, need a file", cleanPath)
	}
	return nil
}

// ensureBase64Data injects/normalizes the "data" field in the JSON body.
func ensureBase64Data(body map[string]any, cleanPath string) error {
	if _, exists := body["data"]; !exists {
		b, err := os.ReadFile(cleanPath)
		if err != nil {
			return fmt.Errorf("upload: read %q: %w", cleanPath, err)
		}
		// strict base64 (StdEncoding already strict, no line breaks)
		body["data"] = base64.StdEncoding.EncodeToString(b)
		return nil
	}

	// Optional: normalize existing "data" to string for JSON encoding
	switch v := body["data"].(type) {
	case []byte:
		body["data"] = base64.StdEncoding.EncodeToString(v)
	case string:
		// assume caller already provided base64
	default:
		return fmt.Errorf("upload: 'data' must be string or []byte, got %T", v)
	}
	return nil
}

// kickoffUpload POSTs to /files/upload with retry; returns the process id.
func (u *Uploader) kickoffUpload(ctx context.Context, buf io.Reader) (string, error) {
	var resp UploadResponse
	path := u.client.projectPath("files/upload")
	if err := u.client.doWithRetry(ctx, http.MethodPost, path, buf, &resp); err != nil {
		return "", fmt.Errorf("upload: %w", err)
	}
	processID := strings.TrimSpace(resp.Process.ProcessID)
	if processID == "" {
		return "", fmt.Errorf("upload: empty process id in response")
	}
	return processID, nil
}

// pollUntilFinished polls a single process until it’s "finished", otherwise errors.
func (u *Uploader) pollUntilFinished(ctx context.Context, processID string) (string, error) {
	results, err := u.client.PollProcesses(ctx, []string{processID})
	if err != nil {
		return "", fmt.Errorf("upload: poll processes: %w", err)
	}
	if len(results) == 0 {
		return "", fmt.Errorf("upload: no process results returned (process_id=%s)", processID)
	}
	completed := results[0]
	if completed.Status == "finished" {
		return processID, nil
	}
	return "", fmt.Errorf("upload: process %s did not finish (status=%s)", completed.ProcessID, completed.Status)
}
