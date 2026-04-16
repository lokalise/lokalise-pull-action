package main

import (
	"fmt"
	"strings"
	"time"
)

// generateBranchName returns either the override branch (validated) or a temp branch
// with pattern "<prefix>_<base>_<sha6>_<unixTs>".
// Notes:
//   - For override branch names, we DO NOT sanitize (to avoid breaking valid refs like "feature/foo+bar").
//     Instead we validate using `git check-ref-format --branch`.
//   - For auto-generated names, we sanitize to keep them safe and predictable.
//   - Length is capped to 255 to satisfy git ref constraints.
func generateBranchName(config *Config, runner CommandRunner) (string, error) {
	if override, ok, err := resolveOverrideBranchName(config, runner); ok || err != nil {
		return override, err
	}

	branchName, err := buildGeneratedBranchName(config)
	if err != nil {
		return "", err
	}

	if err := validateBranchName(branchName, runner); err != nil {
		return "", err
	}

	return branchName, nil
}

func resolveOverrideBranchName(config *Config, runner CommandRunner) (string, bool, error) {
	if config.OverrideBranchName == "" {
		return "", false, nil
	}

	override := strings.TrimSpace(config.OverrideBranchName)
	if override == "" {
		return "", true, fmt.Errorf("override branch name is empty after trimming")
	}

	if err := validateBranchName(override, runner); err != nil {
		return "", true, err
	}

	return override, true, nil
}

func buildGeneratedBranchName(config *Config) (string, error) {
	timestamp := time.Now().Unix()

	shortSHA, err := shortGitHubSHA(config.GitHubSHA)
	if err != nil {
		return "", err
	}

	safeRefName := sanitizeString(config.BaseRef, 50)
	if safeRefName == "" {
		// If BaseRef is synthetic or empty, still generate a usable branch name.
		safeRefName = "base"
	}

	tempBranchPrefix := strings.TrimSpace(config.TempBranchPrefix)
	if tempBranchPrefix == "" {
		tempBranchPrefix = "lok"
	}

	branchName := fmt.Sprintf("%s_%s_%s_%d", tempBranchPrefix, safeRefName, shortSHA, timestamp)
	branchName = sanitizeString(branchName, 255)

	if branchName == "" {
		return "", fmt.Errorf("generated branch name is empty after sanitization")
	}

	return branchName, nil
}

func shortGitHubSHA(sha string) (string, error) {
	if len(sha) < 6 {
		return "", fmt.Errorf("GITHUB_SHA is too short")
	}
	return sha[:6], nil
}

func validateBranchName(name string, runner CommandRunner) error {
	out, err := runner.Capture("git", "check-ref-format", "--branch", name)
	if err != nil {
		// `check-ref-format` usually prints why it failed; keep it for debugging.
		out = strings.TrimSpace(out)
		if out != "" {
			return fmt.Errorf("invalid branch name %q: %v (git output: %s)", name, err, out)
		}
		return fmt.Errorf("invalid branch name %q: %v", name, err)
	}
	return nil
}

func generateBranchNameForBase(config *Config, realBase string, runner CommandRunner) (string, error) {
	cfgForName := *config
	cfgForName.BaseRef = realBase
	return generateBranchName(&cfgForName, runner)
}
