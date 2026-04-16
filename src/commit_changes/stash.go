package main

import (
	"fmt"
	"strings"
)

func stashIfDirty(runner CommandRunner, msg string) (bool, error) {
	out, err := runner.Capture("git", "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("failed to check git status: %v\nOutput: %s", err, out)
	}
	if strings.TrimSpace(out) == "" {
		return false, nil
	}

	// -u to include untracked files just in case lokalise writes new files
	if err := runner.Run("git", "stash", "push", "-u", "-m", msg); err != nil {
		return false, fmt.Errorf("failed to stash changes: %v", err)
	}
	return true, nil
}

func restoreFileFromStash(runner CommandRunner, stashRef, file string) error {
	if err := runner.Run("git", "checkout", stashRef, "--", file); err == nil {
		return nil
	}
	if err := runner.Run("git", "checkout", stashRef+"^3", "--", file); err != nil {
		return err
	}
	return nil
}

func listStashedFiles(runner CommandRunner, stashRef string) ([]string, error) {
	out, err := runner.Capture("git", "stash", "show", "--name-only", "--include-untracked", stashRef)
	if err != nil {
		return nil, fmt.Errorf("%v\nOutput: %s", err, out)
	}
	return splitNonEmptyLines(out), nil
}

func restoreStashBestEffort(runner CommandRunner, didStash bool) {
	if didStash {
		_ = runner.Run("git", "stash", "pop")
	}
}
