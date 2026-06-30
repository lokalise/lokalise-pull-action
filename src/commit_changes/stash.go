package main

import (
	"fmt"
	"os"
	"strings"
)

func stashIfDirty(runner CommandRunner, msg string) (string, bool, error) {
	out, err := runner.Capture("git", "status", "--porcelain=v1")
	if err != nil {
		return "", false, fmt.Errorf("failed to check git status: %w\nOutput: %s", err, out)
	}
	if strings.TrimSpace(out) == "" {
		return "", false, nil
	}

	// -u to include untracked files just in case lokalise writes new files
	if err := runner.Run("git", "stash", "push", "-u", "-m", msg); err != nil {
		return "", false, fmt.Errorf("failed to stash changes: %w", err)
	}

	refOut, err := runner.Capture("git", "rev-parse", "--verify", "stash@{0}")
	if err != nil {
		return "", false, fmt.Errorf("failed to resolve created stash ref: %w\nOutput: %s", err, refOut)
	}

	stashRef := strings.TrimSpace(refOut)
	if stashRef == "" {
		return "", false, fmt.Errorf("created stash ref is empty")
	}

	return stashRef, true, nil
}

func restoreFilesFromStash(remote, stashRef string, runner CommandRunner) error {
	files, err := listStashedFiles(runner, stashRef)
	if err != nil {
		return fmt.Errorf("checked out %s but failed to list stashed files: %w", remote, err)
	}

	for _, f := range files {
		if err := restoreFileFromStash(runner, stashRef, f); err != nil {
			return fmt.Errorf("checked out %s but failed to restore %s from %s or %s^3: %w", remote, f, stashRef, stashRef, err)
		}
	}

	return nil
}

func restoreFileFromStash(runner CommandRunner, stashRef, file string) error {
	if err := runner.Run("git", "checkout", stashRef, "--", file); err == nil {
		return nil
	} else {
		trackedErr := err

		if err := runner.Run("git", "checkout", stashRef+"^3", "--", file); err != nil {
			return fmt.Errorf("failed to restore file %q from stash %q: tracked restore failed: %w; untracked restore failed: %v", file, stashRef, trackedErr, err)
		}
	}

	return nil
}

func listStashedFiles(runner CommandRunner, stashRef string) ([]string, error) {
	out, err := runner.Capture("git", "stash", "show", "--name-only", "--include-untracked", stashRef)
	if err != nil {
		return nil, fmt.Errorf("%w\nOutput: %s", err, out)
	}
	return splitNonEmptyLines(out), nil
}

func restoreStashBestEffort(runner CommandRunner, stashHash string) {
	stashHash = strings.TrimSpace(stashHash)
	if stashHash == "" {
		return
	}

	selector, err := findStashSelectorByHash(runner, stashHash)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to resolve stash %s for restore: %v\n", stashHash, err)
		return
	}

	if err := runner.Run("git", "stash", "pop", selector); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to restore stash %s (%s): %v\n", selector, stashHash, err)
	}
}

func findStashSelectorByHash(runner CommandRunner, stashHash string) (string, error) {
	stashHash = strings.TrimSpace(stashHash)
	if stashHash == "" {
		return "", fmt.Errorf("stash hash is empty")
	}

	out, err := runner.Capture("git", "stash", "list", "--format=%H%x09%gd")
	if err != nil {
		return "", fmt.Errorf("failed to list stashes: %w\nOutput: %s", err, out)
	}

	for _, rawLine := range strings.Split(out, "\n") {
		line := strings.TrimSuffix(rawLine, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}

		hash, selector, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}

		if strings.TrimSpace(hash) == stashHash {
			selector = strings.TrimSpace(selector)
			if selector == "" {
				return "", fmt.Errorf("matched stash hash %s but selector is empty", stashHash)
			}
			return selector, nil
		}
	}

	return "", fmt.Errorf("created stash %s was not found in stash list", stashHash)
}

func dropStashByHash(runner CommandRunner, stashHash string) error {
	selector, err := findStashSelectorByHash(runner, stashHash)
	if err != nil {
		return err
	}

	if err := runner.Run("git", "stash", "drop", selector); err != nil {
		return fmt.Errorf("failed to drop %s for stash %s: %w", selector, stashHash, err)
	}

	return nil
}
