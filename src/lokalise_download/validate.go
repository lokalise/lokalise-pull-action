package main

import "errors"

// validateDownloadConfig enforces required inputs and guards common pitfalls.
// Intentionally fails fast with actionable messages for CI logs.
func validateDownloadConfig(config DownloadConfig) error {
	if config.ProjectID == "" {
		return errors.New("LOKALISE_PROJECT_ID is required and cannot be empty")
	}

	if config.Token == "" {
		return errors.New("LOKALISE_API_KEY is required and cannot be empty")
	}

	if config.FileFormat == "" {
		return errors.New("FILE_FORMAT environment variable is required")
	}

	// include_tags requires a non-empty ref. Users can opt out via SKIP_INCLUDE_TAGS=true.
	if !config.SkipIncludeTags && config.GitHubRefName == "" {
		return errors.New(
			"GITHUB_REF_NAME or GITHUB_HEAD_REF is required when include_tags are enabled. " +
				"Set SKIP_INCLUDE_TAGS=true to disable tag filtering",
		)
	}

	return nil
}
