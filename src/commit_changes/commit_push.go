package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/bodrovis/lokalise-actions-common/v2/managedpaths"
)

// commitAndPushChanges wires the whole flow: config -> git user -> base ref -> branch -> add -> commit -> push.
func commitAndPushChanges(runner CommandRunner) (string, error) {
	config, err := envVarsToConfig()
	if err != nil {
		return "", err
	}

	if err := setGitUser(config, runner); err != nil {
		return "", err
	}

	realBase, err := resolveRealBase(runner, config)
	if err != nil {
		return "", err
	}
	fmt.Printf("Using base branch: %s\n", realBase)

	branchName, err := generateBranchNameForBase(config, realBase, runner)
	if err != nil {
		return "", err
	}

	if err := checkoutBranch(branchName, realBase, config.HeadRef, runner); err != nil {
		return "", err
	}

	if err := stageManagedFiles(config, runner); err != nil {
		return "", err
	}

	return branchName, commitAndPush(branchName, runner, config)
}

func generateBranchNameForBase(config *Config, realBase string, runner CommandRunner) (string, error) {
	cfgForName := *config
	cfgForName.BaseRef = realBase
	return generateBranchName(&cfgForName, runner)
}

func stageManagedFiles(config *Config, runner CommandRunner) error {
	scope := buildTranslationScope(config)

	filesToStage, err := managedpaths.CollectManagedGitPaths(runner, scope)
	if err != nil {
		return err
	}
	if len(filesToStage) == 0 {
		return ErrNoChanges
	}

	if err := runner.Run("git", append([]string{"add", "-A", "--"}, filesToStage...)...); err != nil {
		return fmt.Errorf("failed to stage files: %v", err)
	}

	return nil
}

func buildTranslationScope(config *Config) managedpaths.TranslationScope {
	return managedpaths.TranslationScope{
		Paths:          config.TranslationPaths,
		FileExt:        config.FileExt,
		FlatNaming:     config.FlatNaming,
		AlwaysPullBase: config.AlwaysPullBase,
		BaseLang:       config.BaseLang,
	}
}

// setGitUser ensures git has user.name/user.email configured,
// defaulting to the GitHub actor with a noreply email if not provided by inputs.
func setGitUser(config *Config, runner CommandRunner) error {
	username, email := resolveGitIdentity(config)

	if err := runner.Run("git", "config", "--global", "user.name", username); err != nil {
		return fmt.Errorf("failed to set git user.name: %v", err)
	}
	if err := runner.Run("git", "config", "--global", "user.email", email); err != nil {
		return fmt.Errorf("failed to set git user.email: %v", err)
	}
	return nil
}

func resolveGitIdentity(config *Config) (username, email string) {
	username = config.GitUserName
	email = config.GitUserEmail

	if username == "" {
		username = config.GitHubActor
	}
	if email == "" {
		email = fmt.Sprintf("%s@users.noreply.github.com", username)
	}

	return username, email
}

// commitAndPush commits staged changes and pushes the branch (forcing if requested).
// Returns ErrNoChanges when nothing is staged (non-fatal for CI).
func commitAndPush(branchName string, runner CommandRunner, config *Config) error {
	hasStagedChanges, err := hasCachedDiff(runner)
	if err != nil {
		return err
	}
	if !hasStagedChanges {
		return ErrNoChanges
	}

	output, err := runner.Capture("git", buildCommitArgs(config)...)
	if err != nil {
		return fmt.Errorf("failed to commit changes: %v\nOutput: %s", err, output)
	}

	return pushBranch(branchName, runner, config)
}

func hasCachedDiff(runner CommandRunner) (bool, error) {
	out, err := runner.Capture("git", "diff", "--name-only", "--cached")
	if err != nil {
		return false, fmt.Errorf("failed to inspect staged changes: %v\nOutput: %s", err, out)
	}
	return strings.TrimSpace(out) != "", nil
}

func buildCommitArgs(config *Config) []string {
	args := []string{"commit"}
	if config.GitSignCommits {
		args = append(args, "-S")
	}
	args = append(args, "-m", config.GitCommitMessage)
	return args
}

func pushBranch(branchName string, runner CommandRunner, config *Config) error {
	if config.ForcePush {
		return runner.Run("git", "push", "--force-with-lease", "origin", branchName)
	}
	return runner.Run("git", "push", "origin", branchName)
}

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
