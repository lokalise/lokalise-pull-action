package main

import (
	"fmt"
	"strings"
)

// resolveRealBase determines a usable base branch.
//
// If cfg.BaseRef is empty or synthetic, resolution order is:
//  1. git ls-remote --symref origin HEAD
//  2. git symbolic-ref --short refs/remotes/origin/HEAD
//  3. git remote show origin
//  4. fallback to "main"
func resolveRealBase(runner CommandRunner, cfg *Config) (string, error) {
	base := strings.TrimSpace(cfg.BaseRef)
	if !isSyntheticRef(base) {
		return base, nil
	}

	if branch, source, ok := resolveFallbackBase(runner); ok {
		fmt.Printf(
			"BASE_REF synthetic/empty, using %s: %s\n",
			source,
			branch,
		)
		return branch, nil
	}

	fmt.Println("Could not resolve default branch from origin; falling back to main")
	return "main", nil
}

func resolveFallbackBase(runner CommandRunner) (string, string, bool) {
	if branch, ok := getDefaultBranchFromLsRemote(runner); ok {
		return branch, "remote HEAD via ls-remote", true
	}

	if branch, ok := getDefaultBranchFromSymbolicRef(runner); ok {
		return branch, "origin/HEAD via symbolic-ref", true
	}

	if branch, ok := getDefaultBranchFromRemoteShow(runner); ok {
		return branch, "remote show origin", true
	}

	return "", "", false
}

func getDefaultBranchFromLsRemote(runner CommandRunner) (string, bool) {
	out, err := runner.Capture(
		"git",
		"ls-remote",
		"--symref",
		"origin",
		"HEAD",
	)
	if err != nil || strings.TrimSpace(out) == "" {
		return "", false
	}

	for line := range strings.SplitSeq(out, "\n") {
		if branch, ok := parseLsRemoteHeadLine(line); ok {
			return branch, true
		}
	}

	return "", false
}

func parseLsRemoteHeadLine(line string) (string, bool) {
	const (
		linePrefix = "ref: "
		lineSuffix = "\tHEAD"
		refPrefix  = "refs/heads/"
	)

	line = strings.TrimSuffix(line, "\r")

	if !strings.HasPrefix(line, linePrefix) ||
		!strings.HasSuffix(line, lineSuffix) {
		return "", false
	}

	ref := strings.TrimSuffix(
		strings.TrimPrefix(line, linePrefix),
		lineSuffix,
	)
	ref = strings.TrimSpace(ref)

	branch, ok := strings.CutPrefix(ref, refPrefix)
	if !ok || branch == "" {
		return "", false
	}

	return branch, true
}

func getDefaultBranchFromSymbolicRef(runner CommandRunner) (string, bool) {
	out, err := runner.Capture(
		"git",
		"symbolic-ref",
		"--quiet",
		"--short",
		"refs/remotes/origin/HEAD",
	)
	if err != nil {
		return "", false
	}

	return parseSymbolicRefBranch(out)
}

func parseSymbolicRefBranch(out string) (string, bool) {
	ref := strings.TrimSpace(out)
	if ref == "" {
		return "", false
	}

	if branch, ok := strings.CutPrefix(ref, "origin/"); ok {
		return branch, branch != ""
	}

	return ref, true
}

func getDefaultBranchFromRemoteShow(runner CommandRunner) (string, bool) {
	out, err := runner.Capture("git", "remote", "show", "origin")
	if err != nil || strings.TrimSpace(out) == "" {
		return "", false
	}

	for line := range strings.SplitSeq(out, "\n") {
		if branch, ok := parseRemoteShowHeadBranchLine(line); ok {
			return branch, true
		}
	}

	return "", false
}

func parseRemoteShowHeadBranchLine(line string) (string, bool) {
	line = strings.TrimSpace(line)

	branch, ok := strings.CutPrefix(line, "HEAD branch: ")
	if !ok {
		return "", false
	}

	branch = strings.TrimSpace(branch)
	if branch == "" || branch == "(unknown)" {
		return "", false
	}

	return branch, true
}

// isSyntheticRef reports whether ref is a CI-generated pseudo-ref that
// should not be used directly as the base branch.
func isSyntheticRef(ref string) bool {
	ref = strings.ToLower(strings.TrimSpace(ref))

	if ref == "" || ref == "merge" || ref == "head" {
		return true
	}

	return strings.HasPrefix(ref, "refs/pull/") ||
		strings.HasPrefix(ref, "pull/") ||
		strings.HasSuffix(ref, "/merge") ||
		strings.HasSuffix(ref, "/head")
}
