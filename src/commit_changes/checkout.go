package main

import (
	"fmt"
	"os"
	"strings"
)

// checkoutBranch bases the working branch off either the PR head (when updating an existing PR)
// or the base branch. We fetch the exact remote ref to work with shallow clones reliably.
func checkoutBranch(branchName, baseRef, headRef string, runner CommandRunner) error {
	remoteExists, err := hasRemote(runner, branchName)
	if err != nil {
		return err
	}

	if remoteExists {
		return checkoutExistingRemoteBranch(branchName, runner)
	}

	if shouldCheckoutPRHead(branchName, headRef) {
		return checkoutPRHeadBranch(branchName, headRef, runner)
	}

	return checkoutFromBaseBranch(branchName, baseRef, runner)
}

func shouldCheckoutPRHead(branchName, headRef string) bool {
	return headRef != "" && branchName == headRef
}

func checkoutExistingRemoteBranch(branchName string, runner CommandRunner) error {
	if err := fetchRemoteBranch(runner, branchName); err != nil {
		return err
	}

	if err := checkoutRemoteTrackingBranch(branchName, branchName, runner); err != nil {
		return err
	}

	setBranchUpstream(runner, branchName, branchName)
	return nil
}

func checkoutPRHeadBranch(branchName, headRef string, runner CommandRunner) error {
	if err := fetchRemoteBranch(runner, headRef); err != nil {
		return err
	}

	if err := checkoutRemoteTrackingBranch(branchName, headRef, runner); err == nil {
		setBranchUpstream(runner, branchName, headRef)
		return nil
	}

	// Keep old fallbacks for compatibility.
	if err := runner.Run("git", "checkout", "-B", branchName, headRef); err == nil {
		return nil
	}
	return runner.Run("git", "checkout", branchName)
}

func checkoutFromBaseBranch(branchName, baseRef string, runner CommandRunner) error {
	if err := fetchRemoteBranch(runner, baseRef); err != nil {
		return err
	}

	if err := runner.Run("git", "checkout", "-B", branchName, "origin/"+baseRef); err == nil {
		unsetBranchUpstream(runner, branchName)
		return nil
	}

	logMissingFetchedRemoteRef(runner, baseRef)

	if err := runner.Run("git", "checkout", "-B", branchName, baseRef); err == nil {
		return nil
	}
	return runner.Run("git", "checkout", branchName)
}

func fetchRemoteBranch(runner CommandRunner, ref string) error {
	// "+A:B" syntax forces update of the local remote-tracking ref.
	spec := fmt.Sprintf("+refs/heads/%[1]s:refs/remotes/origin/%[1]s", ref)
	out, err := runner.Capture("git", "fetch", "--no-tags", "--prune", "origin", spec)
	if err != nil {
		return fmt.Errorf("git fetch failed for %q (spec=%q): %v\nOutput: %s", ref, spec, err, strings.TrimSpace(out))
	}
	return nil
}

func checkoutRemoteTrackingBranch(branchName, remoteRef string, runner CommandRunner) error {
	remote := "origin/" + remoteRef

	if err := runner.Run("git", "checkout", "-B", branchName, remote); err == nil {
		return nil
	} else {
		if err2 := checkoutRemoteWithLocalChanges(branchName, remoteRef, runner, err); err2 != nil {
			return err2
		}
		return nil
	}
}

func setBranchUpstream(runner CommandRunner, branchName, remoteRef string) {
	if err := runner.Run("git", "branch", "--set-upstream-to=origin/"+remoteRef, branchName); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to set upstream for %q to origin/%s: %v\n", branchName, remoteRef, err)
	}
}

func unsetBranchUpstream(runner CommandRunner, branchName string) {
	if err := runner.Run("git", "branch", "--unset-upstream", branchName); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to unset upstream for %q: %v\n", branchName, err)
	}
}

func logMissingFetchedRemoteRef(runner CommandRunner, baseRef string) {
	_, refCheckErr := runner.Capture("git", "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+baseRef)
	if refCheckErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: origin/%s not found locally after fetch (show-ref failed): %v\n", baseRef, refCheckErr)
	}
}

// checkoutRemoteWithLocalChanges tries to switch to origin/<remoteRef> even when local changes block checkout.
// Strategy: If local working tree already matches origin/<remoteRef>, force-checkout is safe.
// Otherwise:
//
//	stash -> checkout origin/<remoteRef> -> restore stashed files by overwriting them
//	(checkout stash@{0} -- <file>, fallback to stash@{0}^3 for untracked) -> reset -> drop stash.
func checkoutRemoteWithLocalChanges(branchName, remoteRef string, runner CommandRunner, cause error) error {
	remote := "origin/" + remoteRef

	status, hasUntracked, err := readWorktreeStatus(runner)
	if err != nil {
		return err
	}
	if status == "" {
		return cause
	}

	same, err := worktreeEqualsRef(remote, runner)
	if err == nil && same && !hasUntracked {
		if err := runner.Run("git", "checkout", "-f", "-B", branchName, remote); err != nil {
			return fmt.Errorf("failed to force-checkout %s: %v", remote, err)
		}
		return nil
	}

	didStash, err := stashIfDirty(runner, "lokalise-temp")
	if err != nil {
		return err
	}

	if err := runner.Run("git", "checkout", "-B", branchName, remote); err != nil {
		restoreStashBestEffort(runner, didStash)
		return fmt.Errorf("failed to checkout %s after stashing: %v", remote, err)
	}

	if !didStash {
		return nil
	}

	if err := restoreFilesFromLatestStash(remote, runner); err != nil {
		return err
	}

	if err := runner.Run("git", "reset"); err != nil {
		return fmt.Errorf("checked out %s but failed to reset index: %v", remote, err)
	}

	if err := runner.Run("git", "stash", "drop", "stash@{0}"); err != nil {
		return fmt.Errorf("checked out %s but failed to drop %s: %v", remote, "stash@{0}", err)
	}

	return nil
}

func restoreFilesFromLatestStash(remote string, runner CommandRunner) error {
	stashRef := "stash@{0}"

	files, err := listStashedFiles(runner, stashRef)
	if err != nil {
		return fmt.Errorf("checked out %s but failed to list stashed files: %v", remote, err)
	}

	for _, f := range files {
		if err := restoreFileFromStash(runner, stashRef, f); err != nil {
			return fmt.Errorf("checked out %s but failed to restore %s from %s or %s^3: %v", remote, f, stashRef, stashRef, err)
		}
	}

	return nil
}

func hasRemote(runner CommandRunner, ref string) (bool, error) {
	out, err := runner.Capture("git", "ls-remote", "--exit-code", "--heads", "origin", ref)
	if err == nil {
		return true, nil
	}

	// `ls-remote --exit-code --heads origin <ref>` returns exit code 2 when no matches found.
	// Other exit codes usually mean auth/network/remote problems.
	if isExitCode(err, 2) {
		return false, nil
	}

	return false, fmt.Errorf("git ls-remote failed for ref %q: %v\nOutput: %s", ref, err, strings.TrimSpace(out))
}
