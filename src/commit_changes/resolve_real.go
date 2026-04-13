package main

import (
	"bufio"
	"fmt"
	"strings"
)

// resolveRealBase determines a usable base branch.
// If cfg.BaseRef is empty/synthetic, we ask the remote what HEAD points to,
// using a locale-agnostic, network-first approach.
//
// Order:
//  1. git ls-remote --symref origin HEAD  -> "ref: refs/heads/<branch> HEAD"
//  2. git symbolic-ref --short refs/remotes/origin/HEAD -> "origin/<branch>"
//  3. git remote show origin  -> parse "HEAD branch: <branch>" (best-effort)
//  4. fallback "main"
func resolveRealBase(runner CommandRunner, cfg *Config) (string, error) {
	base := strings.TrimSpace(cfg.BaseRef)
	if !isSyntheticRef(base) {
		return base, nil
	}

	if br, source, ok := resolveFallbackBase(runner); ok {
		fmt.Printf("BASE_REF synthetic/empty, using %s: %s\n", source, br)
		return br, nil
	}

	return "main", nil
}

func resolveFallbackBase(runner CommandRunner) (branch, source string, ok bool) {
	if br, ok := getDefaultBranchFromLsRemote(runner); ok {
		return br, "remote HEAD via ls-remote", true
	}

	if br, ok := getDefaultBranchFromSymbolicRef(runner); ok {
		return br, "origin/HEAD via symbolic-ref", true
	}

	if br, ok := getDefaultBranchFromRemoteShow(runner); ok {
		return br, "remote show origin", true
	}

	return "", "", false
}

func getDefaultBranchFromLsRemote(runner CommandRunner) (string, bool) {
	out, err := runner.Capture("git", "ls-remote", "--symref", "origin", "HEAD")
	if err != nil || strings.TrimSpace(out) == "" {
		return "", false
	}

	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		if br, ok := parseLsRemoteHeadLine(sc.Text()); ok {
			return br, true
		}
	}

	// Ignore scanner errors: even if a long line is truncated, the target HEAD line is tiny.
	return "", false
}

func parseLsRemoteHeadLine(line string) (string, bool) {
	const (
		linePrefix = "ref: "
		lineSuffix = "\tHEAD"
		refPrefix  = "refs/heads/"
	)

	line = strings.TrimSuffix(line, "\r")

	if !strings.HasPrefix(line, linePrefix) || !strings.HasSuffix(line, lineSuffix) {
		return "", false
	}

	rest, ok := strings.CutPrefix(line, linePrefix)
	if !ok {
		return "", false
	}

	ref := strings.TrimSuffix(rest, lineSuffix)
	ref = strings.TrimSpace(ref)

	br, ok := strings.CutPrefix(ref, refPrefix)
	if !ok || br == "" {
		return "", false
	}

	return br, true
}

func getDefaultBranchFromSymbolicRef(runner CommandRunner) (string, bool) {
	out, err := runner.Capture("git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return "", false
	}

	return parseSymbolicRefBranch(out)
}

func parseSymbolicRefBranch(out string) (string, bool) {
	line := strings.TrimSpace(out)
	if line == "" {
		return "", false
	}

	if i := strings.Index(line, "/"); i >= 0 && i+1 < len(line) {
		return line[i+1:], true
	}

	return line, true
}

func getDefaultBranchFromRemoteShow(runner CommandRunner) (string, bool) {
	out, err := runner.Capture("git", "remote", "show", "origin")
	if err != nil || strings.TrimSpace(out) == "" {
		return "", false
	}

	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		if br, ok := parseRemoteShowHeadBranchLine(sc.Text()); ok {
			return br, true
		}
	}

	return "", false
}

func parseRemoteShowHeadBranchLine(line string) (string, bool) {
	line = strings.TrimSpace(line)

	def, ok := strings.CutPrefix(line, "HEAD branch: ")
	if !ok {
		return "", false
	}

	def = strings.TrimSpace(def)
	if def == "" {
		return "", false
	}

	return def, true
}

// isSyntheticRef flags CI-provided pseudo-refs we should not base from directly.
func isSyntheticRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" || ref == "merge" || ref == "head" {
		return true
	}
	if strings.HasPrefix(ref, "refs/pull/") || strings.HasPrefix(ref, "pull/") {
		return true
	}
	if strings.HasSuffix(ref, "/merge") || strings.HasSuffix(ref, "/head") {
		return true
	}
	return false
}
