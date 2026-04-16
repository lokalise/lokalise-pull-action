package main

import (
	"fmt"
	"strings"

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
