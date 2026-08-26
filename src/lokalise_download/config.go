package main

import (
	"os"
	"strings"
	"time"

	"github.com/bodrovis/lokalise-actions-common/v2/parsers"
)

const (
	defaultMaxRetries      = 3
	defaultSleepTime       = 1
	maxSleepTime           = 60 * time.Second
	defaultDownloadTimeout = 600
	defaultHTTPTimeout     = 120
	defaultPollInitialWait = 1
	defaultPollMaxWait     = 120
)

// DownloadConfig encapsulates all runtime configuration for a download.
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

// prepareConfig reads environment variables, applies defaults, and normalizes whitespace.
// Invalid boolean values fall back to false.
func prepareConfig() DownloadConfig {
	return DownloadConfig{
		ProjectID:             trimmedEnv("LOKALISE_PROJECT_ID"),
		Token:                 trimmedEnv("LOKALISE_API_KEY"),
		FileFormat:            trimmedEnv("FILE_FORMAT"),
		GitHubRefName:         resolveGitHubRefName(),
		AdditionalParams:      trimmedEnv("ADDITIONAL_PARAMS"),
		SkipIncludeTags:       parseBoolEnvOrFalse("SKIP_INCLUDE_TAGS"),
		SkipOriginalFilenames: parseBoolEnvOrFalse("SKIP_ORIGINAL_FILENAMES"),
		AsyncMode:             parseBoolEnvOrFalse("ASYNC_MODE"),
		MaxRetries:            parsers.ParseUintEnv("MAX_RETRIES", defaultMaxRetries),
		InitialSleepTime:      parseSecondsEnv("SLEEP_TIME", defaultSleepTime),
		MaxSleepTime:          maxSleepTime,
		HTTPTimeout:           parseSecondsEnv("HTTP_TIMEOUT", defaultHTTPTimeout),
		DownloadTimeout:       parseSecondsEnv("DOWNLOAD_TIMEOUT", defaultDownloadTimeout),
		AsyncPollInitialWait:  parseSecondsEnv("ASYNC_POLL_INITIAL_WAIT", defaultPollInitialWait),
		AsyncPollMaxWait:      parseSecondsEnv("ASYNC_POLL_MAX_WAIT", defaultPollMaxWait),
	}
}

func parseSecondsEnv(key string, defaultValue int) time.Duration {
	return time.Duration(parsers.ParseUintEnv(key, defaultValue)) * time.Second
}

func parseBoolEnvOrFalse(key string) bool {
	value, err := parsers.ParseBoolEnv(key)
	if err != nil {
		return false
	}

	return value
}

func trimmedEnv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

// resolveGitHubRefName determines the branch or tag used for include_tags.
// On pull requests, GITHUB_HEAD_REF takes precedence because
// GITHUB_REF_NAME may contain "<pr_number>/merge".
func resolveGitHubRefName() string {
	if ref := trimmedEnv("GITHUB_HEAD_REF"); ref != "" {
		return ref
	}

	return trimmedEnv("GITHUB_REF_NAME")
}
