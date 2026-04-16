package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bodrovis/lokex/v2/client/download"
)

func TestBuildDownloadParams_JSON_MergesAndOverrides(t *testing.T) {
	cfg := DownloadConfig{
		FileFormat:            "json",
		GitHubRefName:         "release-2025-08-19",
		SkipIncludeTags:       false,
		SkipOriginalFilenames: false,
		AsyncMode:             true,
		AdditionalParams: `
{
  "indentation": "2sp",
  "export_empty_as": "skip",
  "export_sort": "a_z",
  "replace_breaks": false,
  "include_tags": ["custom-1","custom-2"]
}
`,
	}

	params := buildDownloadParams(cfg)

	want := download.DownloadParams{
		"format":             "json",
		"original_filenames": true,
		"directory_prefix":   "/",
		"include_tags":       []any{"custom-1", "custom-2"},
		"indentation":        "2sp",
		"export_empty_as":    "skip",
		"export_sort":        "a_z",
		"replace_breaks":     false,
	}

	if !reflect.DeepEqual(params, want) {
		t.Fatalf("params mismatch.\n got: %#v\nwant: %#v", params, want)
	}
}

func TestBuildDownloadParams_YAML_MergesAndOverrides(t *testing.T) {
	cfg := DownloadConfig{
		FileFormat:            "json",
		GitHubRefName:         "release-2025-08-19",
		SkipIncludeTags:       false,
		SkipOriginalFilenames: false,
		AsyncMode:             true,
		AdditionalParams: `
indentation: 2sp
export_empty_as: skip
export_sort: a_z
replace_breaks: false
include_tags:
  - custom-1
  - custom-2
`,
	}

	params := buildDownloadParams(cfg)

	want := download.DownloadParams{
		"format":             "json",
		"original_filenames": true,
		"directory_prefix":   "/",
		"include_tags":       []any{"custom-1", "custom-2"},
		"indentation":        "2sp",
		"export_empty_as":    "skip",
		"export_sort":        "a_z",
		"replace_breaks":     false,
	}

	if !reflect.DeepEqual(params, want) {
		t.Fatalf("params mismatch.\n got: %#v\nwant: %#v", params, want)
	}
}

func TestBuildDownloadParams_JSON_EmptyAdditional_UsesDefaults(t *testing.T) {
	cfg := DownloadConfig{
		FileFormat:            "yaml",
		GitHubRefName:         "release-2025-08-19",
		SkipIncludeTags:       false,
		SkipOriginalFilenames: false,
		AsyncMode:             false,
		AdditionalParams:      "",
	}

	p := buildDownloadParams(cfg)

	if p["format"] != "yaml" {
		t.Fatalf("format: got %v want yaml", p["format"])
	}
	if p["original_filenames"] != true {
		t.Fatalf("original_filenames should be true by default")
	}
	if p["directory_prefix"] != "/" {
		t.Fatalf("directory_prefix should be / by default")
	}
	gotTags, ok := p["include_tags"].([]string)
	if !ok {
		// depending on JSON merging, include_tags may be []any; tolerate both
		if aa, ok2 := p["include_tags"].([]any); ok2 {
			if len(aa) != 1 || aa[0] != "release-2025-08-19" {
				t.Fatalf("include_tags wrong: %#v", p["include_tags"])
			}
		} else {
			t.Fatalf("include_tags type wrong: %T", p["include_tags"])
		}
	} else if len(gotTags) != 1 || gotTags[0] != "release-2025-08-19" {
		t.Fatalf("include_tags wrong: %#v", gotTags)
	}
}

func TestBuildDownloadParams_JSON_Invalid_Aborts(t *testing.T) {
	cfg := DownloadConfig{
		FileFormat:       "json",
		GitHubRefName:    "ref",
		AdditionalParams: `{"indentation": "2sp",`,
	}

	requirePanicExit(t, func() {
		_ = buildDownloadParams(cfg)
	})
}

func TestBuildDownloadParams_YAML_Invalid_Aborts(t *testing.T) {
	cfg := DownloadConfig{
		FileFormat:    "json",
		GitHubRefName: "ref",
		// invalid input
		AdditionalParams: "~~~?? invalid !!",
	}

	requirePanicExit(t, func() {
		_ = buildDownloadParams(cfg)
	})
}

func TestBuildDownloadParams_LegacyFlags_Aborts(t *testing.T) {
	cfg := DownloadConfig{
		FileFormat:       "json",
		GitHubRefName:    "ref",
		AdditionalParams: `--indentation=2sp`,
	}

	requirePanicExit(t, func() {
		_ = buildDownloadParams(cfg)
	})
}

// ---------- downloadFiles tests ----------

func TestDownloadFiles_AsyncSuccess(t *testing.T) {
	cfg := DownloadConfig{
		ProjectID:             "proj_123",
		Token:                 "tok_abc",
		FileFormat:            "json",
		GitHubRefName:         "v1.2.3",
		SkipIncludeTags:       false,
		SkipOriginalFilenames: false,
		AsyncMode:             true,
		MaxRetries:            7,
		InitialSleepTime:      2 * time.Second,
		MaxSleepTime:          time.Duration(maxSleepTime) * time.Second,
		HTTPTimeout:           30 * time.Second,
	}

	fd := &fakeDownloader{}
	ad := &fakeAsyncDownloader{fakeDownloader: fd}
	ff := &fakeFactory{downloader: ad}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := downloadFiles(ctx, cfg, ff); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ff.gotToken != "tok_abc" || ff.gotProjectID != "proj_123" {
		t.Fatalf("factory received wrong credentials: token=%s projectID=%s", ff.gotToken, ff.gotProjectID)
	}
	if ff.gotRetries != 7 {
		t.Fatalf("expected retries=7, got %d", ff.gotRetries)
	}
	if ff.gotHTTPTO != 30*time.Second {
		t.Fatalf("expected http timeout=30s, got %v", ff.gotHTTPTO)
	}
	if ff.gotInitialBackoff != 2*time.Second {
		t.Fatalf("expected initial backoff=2s, got %v", ff.gotInitialBackoff)
	}
	if ff.gotMaxBackoff != time.Duration(maxSleepTime)*time.Second {
		t.Fatalf("expected max backoff=%ds, got %v", maxSleepTime, ff.gotMaxBackoff)
	}

	if !fd.called {
		t.Fatalf("expected some download method to be called")
	}
	if fd.gotDest != "./" {
		t.Fatalf("expected dest ./, got %s", fd.gotDest)
	}
	if fd.gotParams["format"] != "json" {
		t.Fatalf("expected format=json, got %v", fd.gotParams["format"])
	}

	got, ok := fd.gotParams["include_tags"].([]string)
	if !ok {
		t.Fatalf("include_tags type mismatch, got %T", fd.gotParams["include_tags"])
	}

	want := []string{"v1.2.3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected include_tags=%v, got %v", want, got)
	}
	if fd.gotParams["original_filenames"] != true {
		t.Fatalf("expected original_filenames=true, got %v", fd.gotParams["original_filenames"])
	}
	if fd.gotParams["directory_prefix"] != "/" {
		t.Fatalf("expected directory_prefix=/, got %v", fd.gotParams["directory_prefix"])
	}

	if !ad.asyncCalled {
		t.Fatalf("expected DownloadAsync to be called")
	}
}

func TestDownloadFiles_SyncSuccess(t *testing.T) {
	cfg := DownloadConfig{
		ProjectID:             "proj_123",
		Token:                 "tok_abc",
		FileFormat:            "json",
		GitHubRefName:         "v1.2.3",
		SkipIncludeTags:       false,
		SkipOriginalFilenames: false,
		AsyncMode:             false,
		MaxRetries:            7,
		InitialSleepTime:      2 * time.Second,
		MaxSleepTime:          time.Duration(maxSleepTime) * time.Second,
		HTTPTimeout:           30 * time.Second,
	}

	fd := &fakeDownloader{}
	ff := &fakeFactory{downloader: fd}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := downloadFiles(ctx, cfg, ff); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ff.gotToken != "tok_abc" || ff.gotProjectID != "proj_123" {
		t.Fatalf("factory received wrong credentials: token=%s projectID=%s", ff.gotToken, ff.gotProjectID)
	}
	if ff.gotRetries != 7 {
		t.Fatalf("expected retries=7, got %d", ff.gotRetries)
	}
	if ff.gotHTTPTO != 30*time.Second {
		t.Fatalf("expected http timeout=30s, got %v", ff.gotHTTPTO)
	}
	if ff.gotInitialBackoff != 2*time.Second {
		t.Fatalf("expected initial backoff=2s, got %v", ff.gotInitialBackoff)
	}
	if ff.gotMaxBackoff != time.Duration(maxSleepTime)*time.Second {
		t.Fatalf("expected max backoff=%ds, got %v", maxSleepTime, ff.gotMaxBackoff)
	}

	if !fd.called {
		t.Fatalf("expected Download to be called")
	}
	if fd.gotDest != "./" {
		t.Fatalf("expected dest ./, got %s", fd.gotDest)
	}
	if fd.gotParams["format"] != "json" {
		t.Fatalf("expected format=json, got %v", fd.gotParams["format"])
	}

	got, ok := fd.gotParams["include_tags"].([]string)
	if !ok {
		t.Fatalf("include_tags type mismatch, got %T", fd.gotParams["include_tags"])
	}

	want := []string{"v1.2.3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected include_tags=%v, got %v", want, got)
	}
	if fd.gotParams["original_filenames"] != true {
		t.Fatalf("expected original_filenames=true, got %v", fd.gotParams["original_filenames"])
	}
	if fd.gotParams["directory_prefix"] != "/" {
		t.Fatalf("expected directory_prefix=/, got %v", fd.gotParams["directory_prefix"])
	}
}

func TestDownloadFiles_FactoryError(t *testing.T) {
	cfg := DownloadConfig{
		ProjectID:        "proj_123",
		Token:            "tok_abc",
		FileFormat:       "json",
		GitHubRefName:    "main",
		MaxRetries:       3,
		InitialSleepTime: time.Duration(1) * time.Second,
		HTTPTimeout:      time.Duration(10) * time.Second,
	}

	ff := &fakeFactory{wantErr: errors.New("boom")}
	err := downloadFiles(context.Background(), cfg, ff)
	if err == nil || !strings.Contains(err.Error(), "cannot create Lokalise API client") {
		t.Fatalf("expected factory error to propagate, got: %v", err)
	}
}

func TestDownloadFiles_DownloadError(t *testing.T) {
	cfg := DownloadConfig{
		ProjectID:        "proj_123",
		Token:            "tok_abc",
		FileFormat:       "json",
		GitHubRefName:    "main",
		MaxRetries:       3,
		InitialSleepTime: time.Duration(1) * time.Second,
		HTTPTimeout:      time.Duration(10) * time.Second,
	}

	fd := &fakeDownloader{returnErr: errors.New("network down")}
	ff := &fakeFactory{downloader: fd}

	err := downloadFiles(context.Background(), cfg, ff)
	if err == nil || !strings.Contains(err.Error(), "download failed") {
		t.Fatalf("expected download error to propagate, got: %v", err)
	}
}

type fakeDownloader struct {
	called     bool
	gotCtx     context.Context
	gotDest    string
	gotParams  download.DownloadParams
	returnPath string
	returnErr  error
}

func (f *fakeDownloader) Download(ctx context.Context, dest string, params download.DownloadParams) (string, error) {
	f.called = true
	f.gotCtx = ctx
	f.gotDest = dest
	f.gotParams = params
	return f.returnPath, f.returnErr
}

type fakeAsyncDownloader struct {
	*fakeDownloader
	asyncCalled bool
}

func (f *fakeAsyncDownloader) DownloadAsync(ctx context.Context, dest string, params download.DownloadParams) (string, error) {
	f.asyncCalled = true
	// reuse capture from base
	return f.Download(ctx, dest, params)
}

type fakeFactory struct {
	wantErr error

	// capture args to assert
	gotToken          string
	gotProjectID      string
	gotRetries        int
	gotHTTPTO         time.Duration
	gotInitialBackoff time.Duration
	gotMaxBackoff     time.Duration

	downloader Downloader // can be *fakeDownloader OR *fakeAsyncDownloader
}

func (f *fakeFactory) NewDownloader(cfg DownloadConfig) (Downloader, error) {
	f.gotToken = cfg.Token
	f.gotProjectID = cfg.ProjectID
	f.gotRetries = cfg.MaxRetries
	f.gotHTTPTO = cfg.HTTPTimeout
	f.gotInitialBackoff = cfg.InitialSleepTime
	f.gotMaxBackoff = cfg.MaxSleepTime

	if f.wantErr != nil {
		return nil, f.wantErr
	}
	if f.downloader == nil {
		return &fakeDownloader{}, nil
	}
	return f.downloader, nil
}

// requirePanicExit runs fn and asserts our TestMain exit panic is thrown.
func requirePanicExit(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic from exitFunc, got none")
		}
		msg := fmt.Sprint(r)
		if !strings.Contains(msg, "Exit called with code") {
			t.Fatalf("expected exit panic, got: %v", r)
		}
	}()
	fn()
}
