package main

import (
	"fmt"
	"strings"
)

// worktreeEqualsRef checks if BOTH working tree and index match <ref>.
// If yes -> safe to force-checkout.
// It uses `git diff --quiet` exit codes.
func worktreeEqualsRef(ref string, runner CommandRunner) (bool, error) {
	_, err1 := runner.Capture("git", "diff", "--quiet", ref)
	if err1 != nil && !isExitCode(err1, 1) {
		return false, fmt.Errorf("git diff failed: %v", err1)
	}
	if isExitCode(err1, 1) {
		return false, nil
	}

	_, err2 := runner.Capture("git", "diff", "--quiet", "--cached", ref)
	if err2 != nil && !isExitCode(err2, 1) {
		return false, fmt.Errorf("git diff --cached failed: %v", err2)
	}
	if isExitCode(err2, 1) {
		return false, nil
	}

	return true, nil
}

func readWorktreeStatus(runner CommandRunner) (status string, hasUntracked bool, err error) {
	out, err := runner.Capture("git", "status", "--porcelain")
	if err != nil {
		return "", false, fmt.Errorf("failed to check status: %v\nOutput: %s", err, out)
	}

	status = strings.TrimSpace(out)
	return status, hasUntrackedFiles(status), nil
}

func hasUntrackedFiles(status string) bool {
	for _, line := range strings.Split(status, "\n") {
		if strings.HasPrefix(line, "?? ") {
			return true
		}
	}
	return false
}
