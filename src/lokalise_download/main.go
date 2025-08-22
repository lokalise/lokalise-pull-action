package main

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"
	"time"

	"github.com/bodrovis/lokalise-actions-common/v2/parsers"
	"github.com/bodrovis/lokex/client"
)

// exitFunc is a function variable that defaults to os.Exit.
// This can be overridden in tests to capture exit behavior.
var exitFunc = os.Exit

const (
	defaultMaxRetries      = 3   // Default number of retries for rate-limited requests
	defaultSleepTime       = 1   // Default initial sleep time in seconds between retries
	maxSleepTime           = 60  // Maximum sleep time in seconds between retries
	defaultDownloadTimeout = 600 // Maximum total time in seconds for the whole operation
	defaultHTTPTimeout     = 120 // Timeout for the HTTP calls
	defaultPollInitialWait = 1
	defaultPollMaxWait     = 120
)

// DownloadConfig holds all the necessary configuration for downloading files
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

type Downloader interface {
	Download(ctx context.Context, dest string, params client.DownloadParams) (string, error)
}

type AsyncDownloader interface {
	DownloadAsync(ctx context.Context, dest string, params client.DownloadParams) (string, error)
}

type ClientFactory interface {
	NewDownloader(cfg DownloadConfig) (Downloader, error)
}

type LokaliseFactory struct{}

func (f *LokaliseFactory) NewDownloader(cfg DownloadConfig) (Downloader, error) {
	lokaliseClient, err := client.NewClient(
		cfg.Token,
		cfg.ProjectID,
		client.WithMaxRetries(cfg.MaxRetries),
		client.WithHTTPTimeout(cfg.HTTPTimeout),
		client.WithBackoff(cfg.InitialSleepTime, cfg.MaxSleepTime),
		client.WithPollWait(cfg.AsyncPollInitialWait, cfg.AsyncPollMaxWait),
		client.WithUserAgent("lokalise-pull-action/lokex"),
	)
	if err != nil {
		return nil, err
	}
	return client.NewDownloader(lokaliseClient), nil
}

func main() {
	// Ensure the required command-line arguments are provided
	if len(os.Args) < 3 {
		returnWithError("Usage: lokalise_download <project_id> <token>")
	}

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

	// Create the download configuration
	config := DownloadConfig{
		ProjectID:             os.Args[1],
		Token:                 os.Args[2],
		FileFormat:            os.Getenv("FILE_FORMAT"),
		GitHubRefName:         os.Getenv("GITHUB_REF_NAME"),
		AdditionalParams:      os.Getenv("ADDITIONAL_PARAMS"),
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

	validateDownloadConfig(config)

	ctx, cancel := context.WithTimeout(context.Background(), config.DownloadTimeout)
	defer cancel()

	err = downloadFiles(ctx, config, &LokaliseFactory{})
	if err != nil {
		returnWithError(err.Error())
	}
}

// validateDownloadConfig ensures the configuration has all necessary fields
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
	if config.GitHubRefName == "" {
		returnWithError("GITHUB_REF_NAME environment variable is required.")
	}
}

func buildDownloadParams(config DownloadConfig) client.DownloadParams {
	params := client.DownloadParams{
		"format": config.FileFormat,
	}

	if !config.SkipOriginalFilenames {
		params["original_filenames"] = true
		params["directory_prefix"] = "/"
	}

	if !config.SkipIncludeTags {
		params["include_tags"] = []string{config.GitHubRefName}
	}

	// parse additional params
	ap := strings.TrimSpace(config.AdditionalParams)
	if ap != "" {
		add, err := parseJSONMap(ap)
		if err != nil {
			returnWithError("Invalid additional_params (must be JSON object): " + err.Error())
		}
		maps.Copy(params, add)
	}

	return params
}

func parseJSONMap(s string) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	return m, nil
}

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
		// should never happen in real code
		return fmt.Errorf("async mode requested, but downloader doesn't support DownloadAsync")
	}

	if _, err := dl.Download(ctx, "./", params); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	return nil
}

// returnWithError prints an error message to stderr and exits the program with a non-zero status code.
func returnWithError(message string) {
	fmt.Fprintf(os.Stderr, "Error: %s\n", message)
	exitFunc(1)
}
