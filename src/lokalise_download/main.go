package main

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"
	"time"

	yaml "go.yaml.in/yaml/v4"

	"github.com/bodrovis/lokalise-actions-common/v2/parsers"
	"github.com/bodrovis/lokex/v2/client"
)

// exitFunc is a function variable that defaults to os.Exit.
// Overridable in tests to assert exit behavior without actually terminating the process.
// Rationale: makes CLI testable without forking a process.
var exitFunc = os.Exit

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

// Downloader abstracts the concrete lokex downloader. Useful for tests.
type Downloader interface {
	// Download stores files into dest
	Download(ctx context.Context, dest string, params client.DownloadParams) (string, error)
}

// AsyncDownloader is an optional extension used when AsyncMode = true.
type AsyncDownloader interface {
	DownloadAsync(ctx context.Context, dest string, params client.DownloadParams) (string, error)
}

// ClientFactory allows injecting a fake client in tests and keeping main() thin.
type ClientFactory interface {
	NewDownloader(cfg DownloadConfig) (Downloader, error)
}

// LokaliseFactory builds a real lokex client with configured backoff/timeouts.
type LokaliseFactory struct{}

// NewDownloader wires lokex client with timeouts, retries, UA and polling knobs.
// All resilience (retry/backoff) is delegated to the lokex library.
func (f *LokaliseFactory) NewDownloader(cfg DownloadConfig) (Downloader, error) {
	lokaliseClient, err := client.NewClient(
		cfg.Token,
		cfg.ProjectID,
		client.WithMaxRetries(cfg.MaxRetries),
		client.WithHTTPTimeout(cfg.HTTPTimeout),
		client.WithBackoff(cfg.InitialSleepTime, cfg.MaxSleepTime),
		client.WithPollWait(cfg.AsyncPollInitialWait, cfg.AsyncPollMaxWait),
		// UA helps identify traffic on the vendor side (support/debug).
		client.WithUserAgent("lokalise-pull-action/lokex"),
	)
	if err != nil {
		return nil, err
	}
	return client.NewDownloader(lokaliseClient), nil
}

func main() {
	// Build config from env and fail fast on obvious misconfigurations.
	config := prepareConfig()
	validateDownloadConfig(config)

	// Hard deadline for the whole run to avoid hanging jobs in CI.
	ctx, cancel := context.WithTimeout(context.Background(), config.DownloadTimeout)
	defer cancel()

	// Perform the download using a factory (good for DI in tests).
	if err := downloadFiles(ctx, config, &LokaliseFactory{}); err != nil {
		returnWithError(err.Error())
	}
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

// validateDownloadConfig enforces required inputs and guards common pitfalls.
// Intentionally fails fast with actionable messages for CI logs.
func validateDownloadConfig(config DownloadConfig) {
	if config.ProjectID == "" {
		returnWithError("Project ID is required and cannot be empty.")
	}

	if config.Token == "" {
		returnWithError("API token is required and cannot be empty.")
	}

	if config.FileFormat == "" {
		returnWithError("FILE_FORMAT environment variable is required.")
	}

	// include_tags requires a non-empty ref. Users can opt-out via SKIP_INCLUDE_TAGS=true.
	if !config.SkipIncludeTags && config.GitHubRefName == "" {
		returnWithError(
			"GITHUB_REF_NAME is required when include_tags are enabled. " +
				"Set SKIP_INCLUDE_TAGS=true to disable tag filtering.",
		)
	}
}

// buildDownloadParams assembles the payload for the vendor API.
// Notes:
// - When original_filenames=true, Lokalise exports per-original path and directory_prefix is respected.
// - include_tags narrows the export to keys tagged with the current branch/tag (git-driven workflows).
// - AdditionalParams allows advanced overrides (JSON object or YAML mapping).
func buildDownloadParams(config DownloadConfig) client.DownloadParams {
	params := client.DownloadParams{
		"format": config.FileFormat,
	}

	if !config.SkipOriginalFilenames {
		// Preserve original bundle structure.
		params["original_filenames"] = true
		// "/" makes Lokalise export into repo root relative structure.
		params["directory_prefix"] = "/"
	}

	if !config.SkipIncludeTags {
		// Only pull keys tagged with the current ref.
		params["include_tags"] = []string{config.GitHubRefName}
	}

	ap := strings.TrimSpace(config.AdditionalParams)
	if ap != "" {
		add, err := parseAdditionalParams(ap)
		if err != nil {
			returnWithError("Invalid additional_params (must be JSON object or YAML mapping): " + err.Error())
		}
		// Caller-specified values win over our defaults if keys overlap.
		maps.Copy(params, add)
	}

	return params
}

// parseAdditionalParams detects JSON vs YAML and returns a map[string]any.
// Rule: if first non-space char is '{' => JSON object; otherwise YAML mapping.
func parseAdditionalParams(s string) (map[string]any, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return map[string]any{}, nil
	}

	if strings.HasPrefix(s, "{") {
		return parseJSONMap(s)
	}

	return parseYAMLMap(s)
}

// parseYAMLMap parses a YAML mapping into map[string]any.
func parseYAMLMap(s string) (map[string]any, error) {
	var m map[string]any
	err := yaml.Unmarshal([]byte(s), &m)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, fmt.Errorf("YAML must be a mapping (key: value)")
	}
	return m, nil
}

// parseJSONMap parses a JSON object string into map[string]any.
// Validation: we only accept objects; arrays/primitives are rejected by unmarshal error.
func parseJSONMap(s string) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, fmt.Errorf("JSON must be an object (not null)")
	}
	return m, nil
}

// downloadFiles orchestrates the vendor call respecting AsyncMode.
// The actual HTTP, backoff and archive handling live inside the lokex client.
func downloadFiles(ctx context.Context, cfg DownloadConfig, factory ClientFactory) error {
	fmt.Println("Starting download from Lokalise")

	dl, err := factory.NewDownloader(cfg)
	if err != nil {
		return fmt.Errorf("cannot create Lokalise API client: %w", err)
	}

	params := buildDownloadParams(cfg)

	if cfg.AsyncMode {
		if ad, ok := dl.(AsyncDownloader); ok {
			if _, err := ad.DownloadAsync(ctx, "./", params); err != nil {
				return fmt.Errorf("download failed: %w", err)
			}
			return nil
		}
		// Defensive: ensures miswired factories don't silently change behavior.
		return fmt.Errorf("async mode requested, but downloader doesn't support DownloadAsync")
	}

	// Sync path (default). Client handles retries/backoff/untar internally.
	if _, err := dl.Download(ctx, "./", params); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	return nil
}

// Kept as a function to allow test substitution via exitFunc.
func returnWithError(message string) {
	fmt.Fprintf(os.Stderr, "Error: %s\n", message)
	exitFunc(1)
}
