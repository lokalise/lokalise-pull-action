package main

import (
	"os"
	"strings"
	"time"

	"github.com/bodrovis/lokalise-actions-common/v2/parsers"
)

// Defaults for retry/backoff and timeouts. These are sane, production-leaning values.
// Note: actual HTTP/backoff implementation is delegated to lokex client.
const (
	defaultMaxRetries      = 3   // Default number of retries for rate-limited requests
	defaultSleepTime       = 1   // Default initial sleep time (seconds) before retrying
	maxSleepTime           = 60  // Backoff cap (seconds). Prevents unbounded sleeps.
	defaultDownloadTimeout = 600 // Overall operation deadline (seconds) incl. async polling
	defaultHTTPTimeout     = 120 // Per-request HTTP timeout (seconds)
	defaultPollInitialWait = 1   // Initial async poll delay (seconds)
	defaultPollMaxWait     = 120 // Async polling deadline (seconds)
)

// DownloadConfig encapsulates all runtime knobs pulled from env.
type DownloadConfig struct {
	ProjectID             string
	Token                 string
	FileFormat            string
	GitHubRefName         string
	AdditionalParams      string
	SkipIncludeTags       bool
	SkipOriginalFilenames bool
	MaxRetries            int
	InitialSleepTime      time.Duration
	MaxSleepTime          time.Duration
	HTTPTimeout           time.Duration
	DownloadTimeout       time.Duration
	AsyncMode             bool
	AsyncPollInitialWait  time.Duration
	AsyncPollMaxWait      time.Duration
}

// prepareConfig reads env, applies defaults, and normalizes whitespace.
// Tolerate bad booleans by falling back to false instead of failing early.
func prepareConfig() DownloadConfig {
	skipIncludeTags, err := parsers.ParseBoolEnv("SKIP_INCLUDE_TAGS")
	if err != nil {
		skipIncludeTags = false
	}

	skipOriginalFilenames, err := parsers.ParseBoolEnv("SKIP_ORIGINAL_FILENAMES")
	if err != nil {
		skipOriginalFilenames = false
	}

	asyncMode, err := parsers.ParseBoolEnv("ASYNC_MODE")
	if err != nil {
		asyncMode = false
	}

	// Determine the branch/tag name used for include_tags.
	// On push/tag events: GITHUB_REF_NAME is present.
	// On PR events: GITHUB_HEAD_REF may be set -> use as a fallback.
	refName := strings.TrimSpace(os.Getenv("GITHUB_REF_NAME"))
	if refName == "" {
		if v := strings.TrimSpace(os.Getenv("GITHUB_HEAD_REF")); v != "" {
			refName = v
		}
	}

	return DownloadConfig{
		ProjectID:             strings.TrimSpace(os.Getenv("LOKALISE_PROJECT_ID")),
		Token:                 strings.TrimSpace(os.Getenv("LOKALISE_API_KEY")),
		FileFormat:            strings.TrimSpace(os.Getenv("FILE_FORMAT")),
		GitHubRefName:         refName,
		AdditionalParams:      strings.TrimSpace(os.Getenv("ADDITIONAL_PARAMS")),
		SkipIncludeTags:       skipIncludeTags,
		SkipOriginalFilenames: skipOriginalFilenames,
		AsyncMode:             asyncMode,
		MaxRetries:            parsers.ParseUintEnv("MAX_RETRIES", defaultMaxRetries),
		InitialSleepTime:      time.Duration(parsers.ParseUintEnv("SLEEP_TIME", defaultSleepTime)) * time.Second,
		MaxSleepTime:          time.Duration(maxSleepTime) * time.Second,
		HTTPTimeout:           time.Duration(parsers.ParseUintEnv("HTTP_TIMEOUT", defaultHTTPTimeout)) * time.Second,
		DownloadTimeout:       time.Duration(parsers.ParseUintEnv("DOWNLOAD_TIMEOUT", defaultDownloadTimeout)) * time.Second,
		AsyncPollInitialWait:  time.Duration(parsers.ParseUintEnv("ASYNC_POLL_INITIAL_WAIT", defaultPollInitialWait)) * time.Second,
		AsyncPollMaxWait:      time.Duration(parsers.ParseUintEnv("ASYNC_POLL_MAX_WAIT", defaultPollMaxWait)) * time.Second,
	}
}
