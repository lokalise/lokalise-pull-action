package main

import (
	"testing"
	"time"
)

func TestPrepareConfig_Basic(t *testing.T) {
	t.Setenv("LOKALISE_PROJECT_ID", "proj_123")
	t.Setenv("LOKALISE_API_KEY", "sekret")
	t.Setenv("FILE_FORMAT", "json")
	t.Setenv("GITHUB_REF_NAME", "release/2025-10-09")
	t.Setenv("ADDITIONAL_PARAMS", `{"placeholder_format":"icu"}`)

	t.Setenv("SKIP_INCLUDE_TAGS", "false")
	t.Setenv("SKIP_ORIGINAL_FILENAMES", "false")
	t.Setenv("ASYNC_MODE", "true")

	t.Setenv("MAX_RETRIES", "7")
	t.Setenv("SLEEP_TIME", "3")
	t.Setenv("HTTP_TIMEOUT", "150")
	t.Setenv("DOWNLOAD_TIMEOUT", "700")
	t.Setenv("ASYNC_POLL_INITIAL_WAIT", "2")
	t.Setenv("ASYNC_POLL_MAX_WAIT", "180")

	cfg := prepareConfig()

	if cfg.ProjectID != "proj_123" {
		t.Fatalf("ProjectID mismatch: %q", cfg.ProjectID)
	}
	if cfg.Token != "sekret" {
		t.Fatalf("Token mismatch: %q", cfg.Token)
	}
	if cfg.FileFormat != "json" {
		t.Fatalf("FileFormat mismatch: %q", cfg.FileFormat)
	}
	if cfg.GitHubRefName != "release/2025-10-09" {
		t.Fatalf("GitHubRefName mismatch: %q", cfg.GitHubRefName)
	}
	if cfg.AdditionalParams != `{"placeholder_format":"icu"}` {
		t.Fatalf("AdditionalParams mismatch: %q", cfg.AdditionalParams)
	}

	if cfg.SkipIncludeTags {
		t.Fatal("SkipIncludeTags should be false")
	}
	if cfg.SkipOriginalFilenames {
		t.Fatal("SkipOriginalFilenames should be false")
	}
	if !cfg.AsyncMode {
		t.Fatal("AsyncMode should be true")
	}

	if cfg.MaxRetries != 7 {
		t.Fatalf("MaxRetries expected 7, got %d", cfg.MaxRetries)
	}
	if cfg.InitialSleepTime != 3*time.Second {
		t.Fatalf("InitialSleepTime expected 3s, got %s", cfg.InitialSleepTime)
	}
	if cfg.HTTPTimeout != 150*time.Second {
		t.Fatalf("HTTPTimeout expected 150s, got %s", cfg.HTTPTimeout)
	}
	if cfg.DownloadTimeout != 700*time.Second {
		t.Fatalf("DownloadTimeout expected 700s, got %s", cfg.DownloadTimeout)
	}
	if cfg.AsyncPollInitialWait != 2*time.Second {
		t.Fatalf("AsyncPollInitialWait expected 2s, got %s", cfg.AsyncPollInitialWait)
	}
	if cfg.AsyncPollMaxWait != 180*time.Second {
		t.Fatalf("AsyncPollMaxWait expected 180s, got %s", cfg.AsyncPollMaxWait)
	}
}

func TestPrepareConfig_TokenAndRefFallbacks(t *testing.T) {
	t.Setenv("LOKALISE_API_KEY", "secret")
	t.Setenv("LOKALISE_PROJECT_ID", "proj_456")
	t.Setenv("FILE_FORMAT", "yaml")

	t.Setenv("GITHUB_REF_NAME", "")
	t.Setenv("GITHUB_HEAD_REF", "feature/sweet-stuff")

	t.Setenv("SKIP_INCLUDE_TAGS", "not-a-bool")
	t.Setenv("SKIP_ORIGINAL_FILENAMES", "lol")
	t.Setenv("ASYNC_MODE", "nope")

	cfg := prepareConfig()

	if cfg.Token != "secret" {
		t.Fatalf("Token check failed, got %q", cfg.Token)
	}
	if cfg.GitHubRefName != "feature/sweet-stuff" {
		t.Fatalf("Ref fallback failed, got %q", cfg.GitHubRefName)
	}
	if cfg.FileFormat != "yaml" {
		t.Fatalf("FileFormat mismatch: %q", cfg.FileFormat)
	}
	if cfg.SkipIncludeTags {
		t.Fatal("SkipIncludeTags should be false on bad input")
	}
	if cfg.SkipOriginalFilenames {
		t.Fatal("SkipOriginalFilenames should be false on bad input")
	}
	if cfg.AsyncMode {
		t.Fatal("AsyncMode should be false on bad input")
	}

	if cfg.MaxRetries != defaultMaxRetries {
		t.Fatalf("MaxRetries default expected %d, got %d", defaultMaxRetries, cfg.MaxRetries)
	}
	if cfg.InitialSleepTime != time.Duration(defaultSleepTime)*time.Second {
		t.Fatalf("InitialSleepTime default mismatch: %s", cfg.InitialSleepTime)
	}
	if cfg.HTTPTimeout != time.Duration(defaultHTTPTimeout)*time.Second {
		t.Fatalf("HTTPTimeout default mismatch: %s", cfg.HTTPTimeout)
	}
	if cfg.DownloadTimeout != time.Duration(defaultDownloadTimeout)*time.Second {
		t.Fatalf("DownloadTimeout default mismatch: %s", cfg.DownloadTimeout)
	}
	if cfg.AsyncPollInitialWait != time.Duration(defaultPollInitialWait)*time.Second {
		t.Fatalf("AsyncPollInitialWait default mismatch: %s", cfg.AsyncPollInitialWait)
	}
	if cfg.AsyncPollMaxWait != time.Duration(defaultPollMaxWait)*time.Second {
		t.Fatalf("AsyncPollMaxWait default mismatch: %s", cfg.AsyncPollMaxWait)
	}
}

func TestPrepareConfig_WhitespaceTrim(t *testing.T) {
	t.Setenv("LOKALISE_PROJECT_ID", "  proj_789  ")
	t.Setenv("LOKALISE_API_KEY", "  key123  ")
	t.Setenv("FILE_FORMAT", "   json_structured ")
	t.Setenv("GITHUB_REF_NAME", "  refs/heads/release-1  ")
	t.Setenv("ADDITIONAL_PARAMS", `  { "bundle_structure": "ICU" }  `)

	cfg := prepareConfig()

	if cfg.ProjectID != "proj_789" {
		t.Fatalf("ProjectID not trimmed: %q", cfg.ProjectID)
	}
	if cfg.Token != "key123" {
		t.Fatalf("Token not trimmed: %q", cfg.Token)
	}
	if cfg.FileFormat != "json_structured" {
		t.Fatalf("FileFormat not trimmed: %q", cfg.FileFormat)
	}
	if cfg.GitHubRefName != "refs/heads/release-1" {
		t.Fatalf("GitHubRefName not trimmed: %q", cfg.GitHubRefName)
	}
	if cfg.AdditionalParams != `{ "bundle_structure": "ICU" }` {
		t.Fatalf("AdditionalParams not trimmed: %q", cfg.AdditionalParams)
	}
}
