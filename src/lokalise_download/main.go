package main

import (
	"bufio"
	"context"
	"fmt"
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
	defaultMaxRetries      = 5   // Default number of retries for rate-limited requests
	defaultSleepTime       = 1   // Default initial sleep time in seconds between retries
	maxSleepTime           = 60  // Maximum sleep time in seconds between retries
	maxTotalTime           = 300 // Maximum total time in seconds for all retries
	defaultDownloadTimeout = 120 // Timeout for the download script
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
	SleepTime             int
	DownloadTimeout       int
	AsyncMode             bool
}

type Downloader interface {
	Download(ctx context.Context, dest string, params client.DownloadParams) (string, error)
}

type ClientFactory interface {
	NewDownloader(token, projectID string, retries, downloadTimeout, initialBackoff, maxBackoff int) (Downloader, error)
}

type LokaliseFactory struct{}

func (f *LokaliseFactory) NewDownloader(token, projectID string, retries, downloadTimeout, initialBackoff, maxBackoff int) (Downloader, error) {
	lokaliseClient, err := client.NewClient(
		token,
		projectID,
		client.WithMaxDownloadRetries(retries),
		client.WithHTTPTimeout(time.Duration(downloadTimeout)*time.Second),
		client.WithBackoff(time.Duration(initialBackoff)*time.Second, time.Duration(maxBackoff)*time.Second),
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
		AdditionalParams:      os.Getenv("CLI_ADD_PARAMS"),
		SkipIncludeTags:       skipIncludeTags,
		SkipOriginalFilenames: skipOriginalFilenames,
		AsyncMode:             asyncMode,
		MaxRetries:            parsers.ParseUintEnv("MAX_RETRIES", defaultMaxRetries),
		SleepTime:             parsers.ParseUintEnv("SLEEP_TIME", defaultSleepTime),
		DownloadTimeout:       parsers.ParseUintEnv("DOWNLOAD_TIMEOUT", defaultDownloadTimeout),
	}

	validateDownloadConfig(config)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(maxTotalTime)*time.Second)
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

	if config.AsyncMode {
		params["async"] = true
	}

	if !config.SkipOriginalFilenames {
		params["original_filenames"] = true
		params["directory_prefix"] = "/"
	}

	if !config.SkipIncludeTags {
		params["include_tags"] = config.GitHubRefName
	}

	// parse additional params
	if config.AdditionalParams != "" {
		scanner := bufio.NewScanner(strings.NewReader(config.AdditionalParams))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			// strip leading "--"
			line = strings.TrimPrefix(line, "--")
			// split key=val
			parts := strings.SplitN(line, "=", 2)
			key := strings.ReplaceAll(parts[0], "-", "_")
			if len(parts) == 2 {
				params[key] = parts[1]
			} else {
				// flag without value → true
				params[key] = true
			}
		}
	}

	return params
}

func downloadFiles(ctx context.Context, cfg DownloadConfig, factory ClientFactory) error {
	fmt.Println("Starting download from Lokalise")

	downloader, err := factory.NewDownloader(cfg.Token, cfg.ProjectID, cfg.MaxRetries, cfg.DownloadTimeout, cfg.SleepTime, maxSleepTime)
	if err != nil {
		return fmt.Errorf("cannot create Lokalise API client: %w", err)
	}

	params := buildDownloadParams(cfg)
	_, err = downloader.Download(ctx, "./", params)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	return nil
}

// returnWithError prints an error message to stderr and exits the program with a non-zero status code.
func returnWithError(message string) {
	fmt.Fprintf(os.Stderr, "Error: %s\n", message)
	exitFunc(1)
}
