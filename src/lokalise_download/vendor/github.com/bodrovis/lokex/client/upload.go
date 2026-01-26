// Package client: uploader for Lokalise file imports.
//
// This file implements the upload side of lokex:
//   - POST /files/upload with a JSON body that includes either a filename
//     (we'll read & base64 it) or an explicit base64 "data" field.
//   - Optionally poll the returned process until it finishes, or return
//     immediately with the process id if polling is disabled.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

type uploadBodyFactory struct {
	ctx       context.Context
	params    UploadParams
	cleanPath string
}

func (f uploadBodyFactory) NewBody() (io.ReadCloser, error) {
	return newUploadBody(f.ctx, f.params, f.cleanPath)
}

// It must satisfy io.Reader to match doWithRetry signature,
// but doWithRetry will never call Read when it sees NewBody().
func (f uploadBodyFactory) Read(p []byte) (int, error) {
	return 0, io.EOF
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

	if _, hasData := body["data"]; !hasData {
		if err := ensureFileIsRegular(cleanPath); err != nil {
			return "", err
		}
	}

	// Stream JSON + base64 instead of materializing it.
	// This works whether:
	// - params["data"] missing -> reads file and base64-encodes on the fly
	// - params["data"] is []byte -> base64-encodes on the fly
	// - params["data"] is string -> writes as-is (assumed already base64)
	processID, err := u.kickoffUploadStreaming(ctx, body, cleanPath)
	if err != nil {
		return "", err
	}

	if !poll {
		return processID, nil
	}
	return u.pollUntilFinished(ctx, processID)
}

func (u *Uploader) kickoffUploadStreaming(ctx context.Context, body map[string]any, cleanPath string) (string, error) {
	var resp UploadResponse
	path := u.client.projectPath("files/upload")

	factory := uploadBodyFactory{
		ctx:       ctx,
		params:    body,
		cleanPath: cleanPath,
	}

	if err := u.client.doWithRetry(ctx, http.MethodPost, path, factory, &resp); err != nil {
		return "", fmt.Errorf("upload: %w", err)
	}

	processID := strings.TrimSpace(resp.Process.ProcessID)
	if processID == "" {
		return "", fmt.Errorf("upload: empty process id in response")
	}
	return processID, nil
}

func newUploadBody(ctx context.Context, params UploadParams, cleanPath string) (io.ReadCloser, error) {
	pr, pw := io.Pipe()

	var (
		dataString   string
		dataBytes    []byte
		dataWasBytes bool
		useFile      bool
	)

	if v, ok := params["data"]; ok {
		switch t := v.(type) {
		case string:
			dataString = t
		case []byte:
			dataWasBytes = true
			dataBytes = t
		default:
			return nil, fmt.Errorf("upload: 'data' must be string or []byte, got %T", v)
		}
	} else {
		useFile = true
	}

	go func() {
		var err error
		defer func() {
			if err != nil {
				_ = pw.CloseWithError(err)
			} else {
				_ = pw.Close()
			}
		}()

		// Proper ctx-cancel watcher that stops once we’re done
		finished := make(chan struct{})
		defer close(finished)
		go func() {
			select {
			case <-ctx.Done():
				_ = pw.CloseWithError(ctx.Err())
			case <-finished:
			}
		}()

		w := bufio.NewWriterSize(pw, 256<<10)
		defer func() {
			if ferr := w.Flush(); err == nil && ferr != nil {
				err = ferr
			}
		}()

		keys := make([]string, 0, len(params)+1)
		for k := range params {
			if k == "data" {
				continue
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)

		if _, err = w.WriteString("{"); err != nil {
			return
		}

		first := true
		writeKV := func(k string, v any) {
			if err != nil {
				return
			}
			if !first {
				_, err = w.WriteString(",")
				if err != nil {
					return
				}
			}
			first = false

			kb, e := json.Marshal(k)
			if e != nil {
				err = e
				return
			}
			vb, e := json.Marshal(v)
			if e != nil {
				err = e
				return
			}
			if _, err = w.Write(kb); err != nil {
				return
			}
			if _, err = w.WriteString(":"); err != nil {
				return
			}
			_, err = w.Write(vb)
		}

		for _, k := range keys {
			writeKV(k, params[k])
		}

		if !first {
			if _, err = w.WriteString(","); err != nil {
				return
			}
		}

		if _, err = w.WriteString(`"data":"`); err != nil {
			return
		}

		// If caller provided base64 string, write as-is.
		if !useFile && !dataWasBytes {
			if _, err = w.WriteString(dataString); err != nil {
				return
			}
			_, err = w.WriteString(`"}`)
			return
		}

		enc := base64.NewEncoder(base64.StdEncoding, w)

		switch {
		case useFile:
			f, e := os.Open(cleanPath)
			if e != nil {
				err = e
				_ = enc.Close()
				return
			}
			_, err = io.Copy(enc, f) // let io.Copy manage buffer
			_ = f.Close()

		case dataWasBytes:
			_, err = io.Copy(enc, bytes.NewReader(dataBytes))
		}

		if cerr := enc.Close(); err == nil && cerr != nil {
			err = cerr
			return
		}

		if _, err = w.WriteString(`"}`); err != nil {
			return
		}
	}()

	return pr, nil
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
	fi, err := os.Stat(cleanPath)
	if err != nil {
		return fmt.Errorf("upload: stat %q: %w", cleanPath, err)
	}
	if fi.IsDir() {
		return fmt.Errorf("upload: %q is a directory, need a file", cleanPath)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("upload: %q is not a regular file", cleanPath)
	}
	return nil
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
