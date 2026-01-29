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
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
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
	ctx      context.Context
	params   UploadParams
	readPath string
}

type uploadDataSpec struct {
	useFile      bool
	dataWasBytes bool
	dataString   string
	dataBytes    []byte
}

func (f uploadBodyFactory) NewBody() (io.ReadCloser, error) {
	return newUploadBody(f.ctx, f.params, f.readPath)
}

// It must satisfy io.Reader to match doWithRetry signature,
// but doWithRetry will never call Read when it sees NewBody().
func (f uploadBodyFactory) Read(p []byte) (int, error) {
	return 0, io.EOF
}

// NewUploader creates a new Uploader bound to c.
func NewUploader(c *Client) *Uploader {
	if c == nil {
		panic("lokex/client: nil Client passed to NewUploader")
	}
	return &Uploader{
		client: c,
	}
}

// Upload uploads a file to Lokalise using /files/upload.
// Behavior:
//  1. Validates and cleans the "filename" param, ensures it's a regular file (unless data is provided explicitly).
//  2. If "data" is absent, reads the file and base64-encodes it (StdEncoding).
//     If "data" is present as []byte, it is base64-encoded; if string, it is
//     used as-is (assumed base64).
//  3. Sends POST with retry/backoff via the client's doWithRetry.
//  4. Returns the server-provided process id.
//
// If poll is true, it will call PollProcesses on that process and only return
// when the process reaches "finished" (otherwise it errors). If poll is false,
// it returns immediately after kickoff with the process id.
func (u *Uploader) Upload(ctx context.Context, params UploadParams, srcPath string, poll bool) (string, error) {
	if u == nil || u.client == nil {
		return "", errors.New("upload: uploader/client is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	body, filename, err := cloneAndValidateParams(params) // filename — как в API
	if err != nil {
		return "", err
	}

	readPath := ""
	if _, hasData := body["data"]; !hasData {
		readPath = strings.TrimSpace(srcPath)
		if readPath == "" {
			readPath = filename
		}
		readPath = filepath.Clean(readPath)

		if err := ensureFileIsRegular(readPath); err != nil {
			return "", err
		}
	}

	processID, err := u.kickoffUploadStreaming(ctx, body, readPath)
	if err != nil {
		return "", err
	}

	if !poll {
		return processID, nil
	}
	return u.pollUntilFinished(ctx, processID)
}

func (u *Uploader) kickoffUploadStreaming(ctx context.Context, body UploadParams, cleanPath string) (string, error) {
	if u == nil || u.client == nil {
		return "", errors.New("upload: kickoff: uploader/client is nil")
	}

	cleanPath = strings.TrimSpace(cleanPath)
	if cleanPath == "" {
		if _, has := body["data"]; !has {
			return "", errors.New("upload: kickoff: missing local file path and 'data'")
		}
	}

	var resp UploadResponse
	path := u.client.projectPath("files/upload")

	factory := uploadBodyFactory{
		ctx:      ctx,
		params:   body,
		readPath: cleanPath,
	}

	if err := u.client.doWithRetry(ctx, http.MethodPost, path, factory, &resp); err != nil {
		return "", fmt.Errorf("upload: kickoff: %w", err)
	}

	processID := strings.TrimSpace(resp.Process.ProcessID)
	if processID == "" {
		return "", fmt.Errorf("upload: kickoff: empty process id in response")
	}
	return processID, nil
}

func newUploadBody(ctx context.Context, params UploadParams, cleanPath string) (io.ReadCloser, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	spec, err := parseUploadDataSpec(params)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	pr, pw := io.Pipe()
	go func() {
		var werr error
		defer func() {
			if werr != nil {
				_ = pw.CloseWithError(werr)
			} else {
				_ = pw.Close()
			}
		}()

		// Close the pipe if ctx is canceled.
		stop := context.AfterFunc(ctx, func() {
			_ = pw.CloseWithError(ctx.Err())
		})
		defer stop()

		// If already canceled, bail early (avoids noisy pipe errors).
		if err := ctx.Err(); err != nil {
			werr = err
			return
		}

		bw := bufio.NewWriterSize(pw, 256<<10)
		defer func() {
			if ferr := bw.Flush(); werr == nil && ferr != nil {
				werr = ferr
			}
		}()

		werr = writeUploadJSON(bw, params, cleanPath, spec)
	}()

	return pr, nil
}

func parseUploadDataSpec(params UploadParams) (uploadDataSpec, error) {
	var spec uploadDataSpec

	v, ok := params["data"]
	if !ok {
		spec.useFile = true
		return spec, nil
	}

	switch t := v.(type) {
	case string:
		// fail fast BEFORE we create the pipe / start goroutines / send HTTP.
		norm, err := validateAndNormalizeStdBase64String(t)
		if err != nil {
			return uploadDataSpec{}, err
		}
		spec.dataString = norm
	case []byte:
		spec.dataWasBytes = true
		spec.dataBytes = t
	default:
		return uploadDataSpec{}, fmt.Errorf("upload: 'data' must be string or []byte, got %T", v)
	}

	return spec, nil
}

func writeUploadJSON(w *bufio.Writer, params UploadParams, cleanPath string, spec uploadDataSpec) error {
	// Start JSON object.
	if _, err := w.WriteString("{"); err != nil {
		return err
	}

	first := true
	writeComma := func() error {
		if first {
			first = false
			return nil
		}
		_, err := w.WriteString(",")
		return err
	}

	// Write params except "data" (unordered).
	for k, v := range params {
		if k == "data" {
			continue
		}
		if err := writeUploadKV(w, k, v, &first); err != nil {
			return err
		}
	}

	// Now write "data".
	if err := writeComma(); err != nil {
		return err
	}

	if _, err := w.WriteString(`"data":"`); err != nil {
		return err
	}

	if err := writeUploadData(w, cleanPath, spec); err != nil {
		return err
	}

	// Close string + object.
	_, err := w.WriteString(`"}`)
	return err
}

func writeUploadKV(w *bufio.Writer, k string, v any, first *bool) error {
	if !*first {
		if _, err := w.WriteString(","); err != nil {
			return err
		}
	} else {
		*first = false
	}

	kb, err := json.Marshal(k)
	if err != nil {
		return err
	}
	vb, err := json.Marshal(v)
	if err != nil {
		return err
	}

	if _, err := w.Write(kb); err != nil {
		return err
	}
	if _, err := w.WriteString(":"); err != nil {
		return err
	}
	_, err = w.Write(vb)
	return err
}

func writeUploadData(w *bufio.Writer, cleanPath string, spec uploadDataSpec) error {
	// Caller provided base64 string -> just write as-is.
	if !spec.useFile && !spec.dataWasBytes {
		_, err := w.WriteString(spec.dataString)
		return err
	}

	// Pick a reader source (file or bytes).
	var (
		r         io.Reader
		closeFile func() error
	)

	switch {
	case spec.useFile:
		f, err := os.Open(cleanPath)
		if err != nil {
			return err
		}
		r = f
		closeFile = f.Close

	case spec.dataWasBytes:
		r = bytes.NewReader(spec.dataBytes)

	default:
		return fmt.Errorf("upload: invalid data spec")
	}

	enc := base64.NewEncoder(base64.StdEncoding, w)

	_, err := io.Copy(enc, r)

	// Close file (if any), but don’t clobber existing error.
	if closeFile != nil {
		if cerr := closeFile(); cerr != nil {
			if err == nil {
				err = cerr
			} else {
				err = errors.Join(err, cerr)
			}
		}
	}

	// Close encoder (flushes final base64 padding).
	if cerr := enc.Close(); cerr != nil {
		if err == nil {
			err = cerr
		} else {
			err = errors.Join(err, cerr)
		}
	}

	return err
}

func validateAndNormalizeStdBase64String(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("upload: 'data' cannot be empty")
	}

	// Std base64 cannot have length%4 == 1 (padded or not).
	switch len(s) % 4 {
	case 0, 2, 3:
		// ok
	default:
		return "", fmt.Errorf("upload: 'data' base64 length is invalid (len%%4==1)")
	}

	// Validate alphabet and padding placement.
	pad := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'A' <= c && c <= 'Z',
			'a' <= c && c <= 'z',
			'0' <= c && c <= '9',
			c == '+', c == '/':
			if pad != 0 {
				return "", fmt.Errorf("upload: invalid base64 padding position")
			}
		case c == '=':
			pad++
			if pad > 2 {
				return "", fmt.Errorf("upload: invalid base64 padding")
			}
		default:
			return "", fmt.Errorf("upload: 'data' contains non-base64 char %q", c)
		}
	}

	// If padding exists, it must occupy only the last pad chars.
	if pad > 0 {
		if len(s)%4 != 0 {
			return "", fmt.Errorf("upload: invalid base64 padding (length must be multiple of 4 when '=' present)")
		}
		for i := len(s) - pad; i < len(s); i++ {
			if s[i] != '=' {
				return "", fmt.Errorf("upload: invalid base64 padding")
			}
		}
		return s, nil
	}

	// No '=' padding provided -> normalize to StdEncoding by adding '='.
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}
	return s, nil
}

// cloneAndValidateParams copies user params and extracts a clean file path.
func cloneAndValidateParams(params UploadParams) (UploadParams, string, error) {
	body := make(UploadParams, len(params)+1)
	maps.Copy(body, params)

	raw, ok := body["filename"]
	if !ok {
		return nil, "", fmt.Errorf("upload: missing 'filename' param")
	}

	name, ok := raw.(string)
	if !ok {
		return nil, "", fmt.Errorf("upload: 'filename' must be a non-empty string")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "", fmt.Errorf("upload: 'filename' must be a non-empty string")
	}

	body["filename"] = name

	return body, name, nil
}

// ensureFileIsRegular stats the path and rejects directories / missing files.
func ensureFileIsRegular(readPath string) error {
	if strings.TrimSpace(readPath) == "" {
		return errors.New("upload: empty file path")
	}

	fi, err := os.Stat(readPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("upload: file not found: %q: %w", readPath, err)
		}
		return fmt.Errorf("upload: stat %q: %w", readPath, err)
	}
	if fi.IsDir() {
		return fmt.Errorf("upload: %q is a directory, need a file", readPath)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("upload: %q is not a regular file", readPath)
	}
	return nil
}

// pollUntilFinished polls a single process until it’s "finished", otherwise errors.
func (u *Uploader) pollUntilFinished(ctx context.Context, processID string) (string, error) {
	processID = strings.TrimSpace(processID)
	if processID == "" {
		return "", errors.New("upload: empty process_id")
	}

	results, err := u.client.PollProcesses(ctx, []string{processID})
	if err != nil {
		return "", fmt.Errorf("upload: poll processes: %w", err)
	}
	if len(results) == 0 {
		return "", fmt.Errorf("upload: no process results returned (process_id=%s)", processID)
	}

	p := results[0]
	switch p.Status {
	case StatusFinished:
		return processID, nil
	case StatusFailed:
		return "", fmt.Errorf("upload: process %s failed", processID)
	default:
		return "", fmt.Errorf("upload: process %s did not finish (status=%s)", processID, p.Status)
	}
}
