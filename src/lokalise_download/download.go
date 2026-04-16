package main

import (
	"context"
	"fmt"

	"github.com/bodrovis/lokalise-actions-common/v2/parsers"
	"github.com/bodrovis/lokex/v2/client"
	"github.com/bodrovis/lokex/v2/client/download"
)

// Downloader abstracts the concrete lokex downloader. Useful for tests.
type Downloader interface {
	// Download stores files into dest
	Download(ctx context.Context, dest string, params download.DownloadParams) (string, error)
}

// AsyncDownloader is an optional extension used when AsyncMode = true.
type AsyncDownloader interface {
	DownloadAsync(ctx context.Context, dest string, params download.DownloadParams) (string, error)
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
	return download.NewDownloader(lokaliseClient), nil
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

	// Sync path (default). Client handles retries/backoff/unzip internally.
	if _, err := dl.Download(ctx, "./", params); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	return nil
}

// buildDownloadParams assembles the payload for the vendor API.
// Notes:
// - When original_filenames=true, Lokalise exports per-original path and directory_prefix is respected.
// - include_tags narrows the export to keys tagged with the current branch/tag (git-driven workflows).
// - AdditionalParams allows advanced overrides (JSON object or YAML mapping).
func buildDownloadParams(config DownloadConfig) download.DownloadParams {
	params := download.DownloadParams{
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

	if err := parsers.ParseAdditionalParamsAndMerge(params, config.AdditionalParams); err != nil {
		returnWithError("Invalid additional_params (must be JSON object or YAML mapping): " + err.Error())
	}

	return params
}
