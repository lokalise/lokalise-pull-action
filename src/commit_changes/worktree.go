package main

import (
	"fmt"
	"strings"
)

// worktreeEqualsRef checks if tracked working tree changes and index changes match <ref>.
// Untracked files are not considered here; use readWorktreeStatus/hasUntrackedFiles separately.
// If yes -> safe to force-checkout.
// It uses `git diff --quiet` exit codes.
func worktreeEqualsRef(ref string, runner CommandRunner) (bool, error) {
	_, err := runner.Capture("git", "diff", "--quiet", ref)
	if err != nil {
		if isExitCode(err, 1) {
			return false, nil
		}

		return false, fmt.Errorf("git diff failed: %w", err)
	}

	_, err = runner.Capture("git", "diff", "--quiet", "--cached", ref)
	if err != nil {
		if isExitCode(err, 1) {
			return false, nil
		}

		return false, fmt.Errorf("git diff --cached failed: %w", err)
	}

	return true, nil
}

func readWorktreeStatus(runner CommandRunner) (string, bool, error) {
	out, err := runner.Capture("git", "status", "--porcelain=v1")
	if err != nil {
		return "", false, fmt.Errorf(
			"failed to check status: %w\nOutput: %s",
			err,
			strings.TrimSpace(out),
		)
	}

	status := strings.TrimRight(out, "\r\n")
	return status, hasUntrackedFiles(status), nil
}

func hasUntrackedFiles(status string) bool {
	for line := range strings.SplitSeq(status, "\n") {
		if strings.HasPrefix(line, "?? ") {
			return true
		}
	}

	return false
}
