package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bodrovis/lokex/client"
)

func TestMain(m *testing.M) {
	// Override exitFunc for testing
	exitFunc = func(code int) {
		panic(fmt.Sprintf("Exit called with code %d", code))
	}

	// Run tests
	code := m.Run()

	// Restore exitFunc after testing (optional)
	exitFunc = os.Exit

	os.Exit(code)
}

// ---------- buildDownloadParams tests ----------

func TestBuildDownloadParams_DefaultsAndFlags(t *testing.T) {
	cfg := DownloadConfig{
		FileFormat:            "json",
		GitHubRefName:         "release-2025-08-19",
		SkipIncludeTags:       false, // include tags
		SkipOriginalFilenames: false, // include original_filenames + directory_prefix
		AsyncMode:             true,
		AdditionalParams: `
--untranslated=true
--no-duplicate-keys
--plural-format=icu

--dry-run
`,
	}

	params := buildDownloadParams(cfg)

	want := client.DownloadParams{
		"format":             "json",
		"async":              true,
		"original_filenames": true,
		"directory_prefix":   "/",
		"include_tags":       "release-2025-08-19",
		"untranslated":       "true",
		"no_duplicate_keys":  true,  // flag without value becomes true
		"plural_format":      "icu", // hyphens -> underscores
		"dry_run":            true,  // another flag
	}

	if !reflect.DeepEqual(params, want) {
		t.Fatalf("params mismatch.\n got: %#v\nwant: %#v", params, want)
	}
}

func TestBuildDownloadParams_SkipFlags(t *testing.T) {
	cfg := DownloadConfig{
		FileFormat:            "yaml",
		GitHubRefName:         "ignored",
		SkipIncludeTags:       true,  // should omit include_tags
		SkipOriginalFilenames: true,  // should omit original_filenames & directory_prefix
		AsyncMode:             false, // should omit async
		AdditionalParams:      "",
	}

	params := buildDownloadParams(cfg)

	if params["format"] != "yaml" {
		t.Fatalf("expected format=yaml, got %v", params["format"])
	}
	if _, ok := params["include_tags"]; ok {
		t.Fatalf("include_tags should be omitted when SkipIncludeTags=true")
	}
	if _, ok := params["original_filenames"]; ok {
		t.Fatalf("original_filenames should be omitted when SkipOriginalFilenames=true")
	}
	if _, ok := params["directory_prefix"]; ok {
		t.Fatalf("directory_prefix should be omitted when SkipOriginalFilenames=true")
	}
	if _, ok := params["async"]; ok {
		t.Fatalf("async should be omitted when AsyncMode=false")
	}
}

// ---------- downloadFiles tests ----------

func TestDownloadFiles_Success(t *testing.T) {
	cfg := DownloadConfig{
		ProjectID:             "proj_123",
		Token:                 "tok_abc",
		FileFormat:            "json",
		GitHubRefName:         "v1.2.3",
		SkipIncludeTags:       false,
		SkipOriginalFilenames: false,
		AsyncMode:             true,
		MaxRetries:            7,
		SleepTime:             2,
		DownloadTimeout:       30,
	}

	fd := &fakeDownloader{}
	ff := &fakeFactory{downloader: fd}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := downloadFiles(ctx, cfg, ff); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// factory knobs
	if ff.gotToken != "tok_abc" || ff.gotProjectID != "proj_123" {
		t.Fatalf("factory received wrong credentials: token=%s projectID=%s", ff.gotToken, ff.gotProjectID)
	}
	if ff.gotRetries != 7 {
		t.Fatalf("expected retries=7, got %d", ff.gotRetries)
	}
	if ff.gotDownloadTO != 30 {
		t.Fatalf("expected download timeout=30, got %d", ff.gotDownloadTO)
	}
	if ff.gotInitialBackoff != 2 {
		t.Fatalf("expected initial backoff=2, got %d", ff.gotInitialBackoff)
	}
	if ff.gotMaxBackoff != maxSleepTime {
		t.Fatalf("expected max backoff=%d, got %d", maxSleepTime, ff.gotMaxBackoff)
	}

	// downloader inputs
	if fd.gotDest != "./" {
		t.Fatalf("expected dest ./, got %s", fd.gotDest)
	}
	if fd.gotParams["format"] != "json" {
		t.Fatalf("expected format=json, got %v", fd.gotParams["format"])
	}
	if fd.gotParams["include_tags"] != "v1.2.3" {
		t.Fatalf("expected include_tags=v1.2.3, got %v", fd.gotParams["include_tags"])
	}
	if fd.gotParams["original_filenames"] != true {
		t.Fatalf("expected original_filenames=true, got %v", fd.gotParams["original_filenames"])
	}
	if fd.gotParams["directory_prefix"] != "/" {
		t.Fatalf("expected directory_prefix=/, got %v", fd.gotParams["directory_prefix"])
	}
	if fd.gotParams["async"] != true {
		t.Fatalf("expected async=true, got %v", fd.gotParams["async"])
	}
}

func TestDownloadFiles_FactoryError(t *testing.T) {
	cfg := DownloadConfig{
		ProjectID:       "proj_123",
		Token:           "tok_abc",
		FileFormat:      "json",
		GitHubRefName:   "main",
		MaxRetries:      3,
		SleepTime:       1,
		DownloadTimeout: 10,
	}

	ff := &fakeFactory{wantErr: errors.New("boom")}
	err := downloadFiles(context.Background(), cfg, ff)
	if err == nil || !strings.Contains(err.Error(), "cannot create Lokalise API client") {
		t.Fatalf("expected factory error to propagate, got: %v", err)
	}
}

func TestDownloadFiles_DownloadError(t *testing.T) {
	cfg := DownloadConfig{
		ProjectID:       "proj_123",
		Token:           "tok_abc",
		FileFormat:      "json",
		GitHubRefName:   "main",
		MaxRetries:      3,
		SleepTime:       1,
		DownloadTimeout: 10,
	}

	fd := &fakeDownloader{returnErr: errors.New("network down")}
	ff := &fakeFactory{downloader: fd}

	err := downloadFiles(context.Background(), cfg, ff)
	if err == nil || !strings.Contains(err.Error(), "download failed") {
		t.Fatalf("expected download error to propagate, got: %v", err)
	}
}

// ---------- validateDownloadConfig tests ----------

func TestValidateDownloadConfig_ExitsOnMissingFields(t *testing.T) {
	// Missing ProjectID
	requirePanicExit(t, func() {
		validateDownloadConfig(DownloadConfig{
			ProjectID:     "",
			Token:         "t",
			FileFormat:    "json",
			GitHubRefName: "ref",
		})
	})

	// Missing Token
	requirePanicExit(t, func() {
		validateDownloadConfig(DownloadConfig{
			ProjectID:     "p",
			Token:         "",
			FileFormat:    "json",
			GitHubRefName: "ref",
		})
	})

	// Missing FILE_FORMAT
	requirePanicExit(t, func() {
		validateDownloadConfig(DownloadConfig{
			ProjectID:     "p",
			Token:         "t",
			FileFormat:    "",
			GitHubRefName: "ref",
		})
	})

	// Missing GITHUB_REF_NAME
	requirePanicExit(t, func() {
		validateDownloadConfig(DownloadConfig{
			ProjectID:     "p",
			Token:         "t",
			FileFormat:    "json",
			GitHubRefName: "",
		})
	})
}

// ---------- integration-lite: env parsing bits ----------

func TestEnvParsingIntoConfig_Smoke(t *testing.T) {
	// Simulate env that main would use when building config (not calling main).
	t.Setenv("FILE_FORMAT", "json")
	t.Setenv("GITHUB_REF_NAME", "release-1")
	t.Setenv("CLI_ADD_PARAMS", "--foo=bar\n--baz-qux\n")
	// bool envs are parsed via parsers.ParseBoolEnv in main; skip here.
	// numeric envs via parsers.ParseUintEnv; we’ll fill directly.

	cfg := DownloadConfig{
		ProjectID:             "pid",
		Token:                 "tok",
		FileFormat:            os.Getenv("FILE_FORMAT"),
		GitHubRefName:         os.Getenv("GITHUB_REF_NAME"),
		AdditionalParams:      os.Getenv("CLI_ADD_PARAMS"),
		SkipIncludeTags:       false,
		SkipOriginalFilenames: false,
		MaxRetries:            5,
		SleepTime:             2,
		DownloadTimeout:       15,
		AsyncMode:             true,
	}

	params := buildDownloadParams(cfg)
	if params["foo"] != "bar" {
		t.Fatalf("expected additional param foo=bar, got %v", params["foo"])
	}
	if params["baz_qux"] != true {
		t.Fatalf("expected flag baz_qux=true, got %v", params["baz_qux"])
	}
}

// ---------- fakes & helpers ----------

type fakeDownloader struct {
	gotCtx     context.Context
	gotDest    string
	gotParams  client.DownloadParams
	returnPath string
	returnErr  error
}

func (f *fakeDownloader) Download(ctx context.Context, dest string, params client.DownloadParams) (string, error) {
	f.gotCtx = ctx
	f.gotDest = dest
	f.gotParams = params
	return f.returnPath, f.returnErr
}

type fakeFactory struct {
	wantErr error

	// capture args to assert
	gotToken          string
	gotProjectID      string
	gotRetries        int
	gotDownloadTO     int
	gotInitialBackoff int
	gotMaxBackoff     int

	downloader Downloader
}

func (f *fakeFactory) NewDownloader(token, projectID string, retries, downloadTimeout, initialBackoff, maxBackoff int) (Downloader, error) {
	f.gotToken = token
	f.gotProjectID = projectID
	f.gotRetries = retries
	f.gotDownloadTO = downloadTimeout
	f.gotInitialBackoff = initialBackoff
	f.gotMaxBackoff = maxBackoff

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
