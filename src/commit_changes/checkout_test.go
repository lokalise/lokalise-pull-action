package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestCheckoutBranch(t *testing.T) {
	// helper: by default pretend remote branch does NOT exist
	captureNoRemote := func(name string, args ...string) (string, error) {
		if name != "git" {
			return "", fmt.Errorf("unexpected binary: %s", name)
		}
		if len(args) >= 1 && args[0] == "ls-remote" {
			// emulate: no remote branch found -> exit code 2
			return "", &mockExitError{code: 2}
		}
		if len(args) >= 1 && args[0] == "fetch" {
			return "", nil
		}
		return "", nil
	}
	t.Run("creates from origin base", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: captureNoRemote,
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					return fmt.Errorf("unexpected binary: %s", name)
				}
				// allow upstream/unset-upstream calls if any
				if len(args) >= 1 && args[0] == "branch" {
					return nil
				}
				switch {
				case len(args) == 4 &&
					args[0] == "checkout" && args[1] == "-B" &&
					args[2] == "new_branch" && args[3] == "origin/main":
					return nil
				default:
					return fmt.Errorf("unexpected command: git %v", args)
				}
			},
		}

		if err := checkoutBranch("new_branch", "main", "", runner); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("falls back to local base", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: captureNoRemote,
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					return fmt.Errorf("unexpected binary: %s", name)
				}
				if len(args) >= 1 && args[0] == "branch" {
					return nil
				}
				switch {
				case len(args) == 4 &&
					args[0] == "checkout" && args[1] == "-B" &&
					args[2] == "branch_from_local" && args[3] == "origin/dev":
					return fmt.Errorf("remote base missing")
				case len(args) == 4 &&
					args[0] == "checkout" && args[1] == "-B" &&
					args[2] == "branch_from_local" && args[3] == "dev":
					return nil
				default:
					return fmt.Errorf("unexpected command: git %v", args)
				}
			},
		}

		if err := checkoutBranch("branch_from_local", "dev", "", runner); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("switches to existing branch", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: captureNoRemote,
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					return fmt.Errorf("unexpected binary: %s", name)
				}
				if len(args) >= 1 && args[0] == "branch" {
					return nil
				}
				switch {
				case len(args) == 4 &&
					args[0] == "checkout" && args[1] == "-B" &&
					args[2] == "existing_branch" && args[3] == "origin/main":
					return fmt.Errorf("already exists")
				case len(args) == 4 &&
					args[0] == "checkout" && args[1] == "-B" &&
					args[2] == "existing_branch" && args[3] == "main":
					return fmt.Errorf("already exists")
				case len(args) == 2 && args[0] == "checkout" && args[1] == "existing_branch":
					return nil
				default:
					return fmt.Errorf("unexpected command: git %v", args)
				}
			},
		}

		if err := checkoutBranch("existing_branch", "main", "", runner); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("updates existing PR head branch (uses headRef)", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: captureNoRemote,
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					return fmt.Errorf("unexpected binary: %s", name)
				}
				if len(args) >= 1 && args[0] == "branch" {
					return nil
				}
				// checkout -B new_br origin/new_br
				if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[2] == "new_br" && args[3] == "origin/new_br" {
					return nil
				}
				return fmt.Errorf("unexpected command: git %v", args)
			},
		}

		if err := checkoutBranch("new_br", "main", "new_br", runner); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("reuses existing remote branch; local changes differ -> stash, checkout, apply_stash_overwrite, drop", func(t *testing.T) {
		exitErr1 := func() error {
			var cmd *exec.Cmd
			if runtime.GOOS == "windows" {
				cmd = exec.Command("cmd", "/C", "exit", "1")
			} else {
				cmd = exec.Command("sh", "-c", "exit 1")
			}
			return cmd.Run()
		}()

		var checkoutAttempts int
		var restored []string
		dropped := false

		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					return "", fmt.Errorf("unexpected binary: %s", name)
				}

				if len(args) >= 1 && args[0] == "ls-remote" {
					return "deadbeef\trefs/heads/lokalise-sync\n", nil
				}

				if len(args) >= 1 && args[0] == "fetch" {
					return "", nil
				}

				// worktreeEqualsRef(): different
				if len(args) >= 3 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "origin/lokalise-sync" {
					return "", exitErr1
				}
				if len(args) >= 4 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "--cached" && args[3] == "origin/lokalise-sync" {
					return "", exitErr1
				}

				// status --porcelain (used here and inside stashIfDirty)
				if len(args) >= 2 && args[0] == "status" && args[1] == "--porcelain" {
					return " M locales/fr.json\n", nil
				}

				// list files in stash
				if len(args) == 5 &&
					args[0] == "stash" && args[1] == "show" &&
					args[2] == "--name-only" &&
					args[3] == "--include-untracked" &&
					args[4] == "stash@{0}" {
					return "locales/fr.json\n", nil
				}

				return "", nil
			},
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					return fmt.Errorf("unexpected binary: %s", name)
				}

				if len(args) >= 1 && args[0] == "branch" {
					return nil
				}

				switch {
				case len(args) == 4 &&
					args[0] == "checkout" && args[1] == "-B" &&
					args[2] == "lokalise-sync" && args[3] == "origin/lokalise-sync":
					checkoutAttempts++
					if checkoutAttempts == 1 {
						return fmt.Errorf("Your local changes would be overwritten by checkout")
					}
					return nil

				case len(args) == 5 &&
					args[0] == "stash" && args[1] == "push" &&
					args[2] == "-u" && args[3] == "-m" && args[4] == "lokalise-temp":
					return nil

				// git checkout stash@{0} -- <file>
				case len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}" && args[2] == "--":
					restored = append(restored, args[3])
					return nil

				// git checkout stash@{0}^3 -- <file>  (untracked)
				case len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}^3" && args[2] == "--":
					restored = append(restored, args[3])
					return nil

				// drop stash
				case (len(args) == 3 && args[0] == "stash" && args[1] == "drop" && args[2] == "stash@{0}") ||
					(len(args) == 2 && args[0] == "stash" && args[1] == "drop"):
					dropped = true
					return nil

				case len(args) == 1 && args[0] == "reset":
					return nil
				default:
					return fmt.Errorf("unexpected command: git %v", args)
				}
			},
		}

		if err := checkoutBranch("lokalise-sync", "master", "", runner); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !slices.Contains(restored, "locales/fr.json") {
			t.Fatalf("expected locales/fr.json to be restored from stash, got %v", restored)
		}
		if !dropped {
			t.Fatalf("expected stash to be dropped")
		}
	})

	t.Run("reuses existing remote branch; local changes block checkout but are identical -> force checkout", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					return "", fmt.Errorf("unexpected binary: %s", name)
				}

				// hasRemote(): remote branch exists
				if len(args) >= 1 && args[0] == "ls-remote" {
					return "deadbeef\trefs/heads/lokalise-sync\n", nil
				}

				// allow fetch noise
				if len(args) >= 1 && args[0] == "fetch" {
					return "", nil
				}

				// repo is dirty (so checkoutRemoteWithLocalChanges won't return cause)
				if len(args) >= 2 && args[0] == "status" && args[1] == "--porcelain" {
					return " M locales/fr.json\n", nil // no "?? " => no untracked
				}

				// worktreeEqualsRef(): identical -> diff exit code 0 (nil error)
				if len(args) >= 3 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "origin/lokalise-sync" {
					return "", nil
				}
				if len(args) >= 4 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "--cached" && args[3] == "origin/lokalise-sync" {
					return "", nil
				}

				return "", nil
			},
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					return fmt.Errorf("unexpected binary: %s", name)
				}

				// allow upstream set
				if len(args) >= 1 && args[0] == "branch" {
					return nil
				}

				switch {
				// first try: normal checkout fails
				case len(args) == 4 &&
					args[0] == "checkout" && args[1] == "-B" &&
					args[2] == "lokalise-sync" && args[3] == "origin/lokalise-sync":
					return fmt.Errorf("Your local changes would be overwritten by checkout")

				// then we expect force checkout
				case len(args) == 5 &&
					args[0] == "checkout" && args[1] == "-f" && args[2] == "-B" &&
					args[3] == "lokalise-sync" && args[4] == "origin/lokalise-sync":
					return nil

				default:
					return fmt.Errorf("unexpected command: git %v", args)
				}
			},
		}

		if err := checkoutBranch("lokalise-sync", "master", "", runner); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestCheckoutBranch_RemoteBranch_StashRestore_MultipleFiles(t *testing.T) {
	const branch = "lokalise-sync"
	const base = "master"

	exitErr1 := func() error {
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/C", "exit", "1")
		} else {
			cmd = exec.Command("sh", "-c", "exit 1")
		}
		return cmd.Run()
	}()

	var checkoutAttempts int
	var restored []string
	dropped := false
	upstreamSet := false

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected binary: %s", name)
			}

			// hasRemote() true
			if len(args) == 5 && args[0] == "ls-remote" {
				return "deadbeef\trefs/heads/" + branch + "\n", nil
			}

			// fetch noise
			if len(args) == 5 && args[0] == "fetch" {
				return "", nil
			}

			// status dirty
			if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain" {
				return " M locales/fr.json\n M i18n/fr.json\n", nil
			}

			// diff says: different
			if len(args) >= 3 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "origin/"+branch {
				return "", exitErr1
			}
			if len(args) >= 4 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "--cached" && args[3] == "origin/"+branch {
				return "", exitErr1
			}

			// stash show lists multiple files
			if len(args) == 5 &&
				args[0] == "stash" && args[1] == "show" &&
				args[2] == "--name-only" &&
				args[3] == "--include-untracked" &&
				args[4] == "stash@{0}" {
				return "locales/fr.json\ni18n/fr.json\n", nil
			}

			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				return fmt.Errorf("unexpected binary: %s", name)
			}

			// normal checkout fails first time, succeeds second (after stash)
			if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[2] == branch && args[3] == "origin/"+branch {
				checkoutAttempts++
				if checkoutAttempts == 1 {
					return fmt.Errorf("Your local changes would be overwritten by checkout")
				}
				return nil
			}

			// stash push
			if len(args) == 5 && args[0] == "stash" && args[1] == "push" && args[2] == "-u" && args[3] == "-m" && args[4] == "lokalise-temp" {
				return nil
			}

			if len(args) == 1 && args[0] == "reset" {
				return nil
			}

			// restore from stash
			if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}" && args[2] == "--" {
				restored = append(restored, args[3])
				return nil
			}

			if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}^3" && args[2] == "--" {
				restored = append(restored, args[3])
				return nil
			}

			// drop stash
			if len(args) == 3 && args[0] == "stash" && args[1] == "drop" && args[2] == "stash@{0}" {
				dropped = true
				return nil
			}

			// set upstream
			if len(args) == 3 && args[0] == "branch" && strings.HasPrefix(args[1], "--set-upstream-to=origin/") && args[2] == branch {
				upstreamSet = true
				return nil
			}

			return fmt.Errorf("unexpected run: git %v", args)
		},
	}

	if err := checkoutBranch(branch, base, "", runner); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Contains(restored, "locales/fr.json") || !slices.Contains(restored, "i18n/fr.json") {
		t.Fatalf("expected both files restored, got %v", restored)
	}
	if !dropped {
		t.Fatalf("expected stash to be dropped")
	}
	if !upstreamSet {
		t.Fatalf("expected upstream to be set")
	}
}

func TestCheckoutRemoteWithLocalChanges_UntrackedRestoredFromThirdParent(t *testing.T) {
	const ref = "lokalise-sync"

	var calls [][]string

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			// dirty with untracked
			if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain" {
				return "?? newfile.json\n", nil
			}

			// worktreeEqualsRef -> pretend "different"
			if len(args) >= 3 && args[0] == "diff" && args[1] == "--quiet" {
				// exit code 1 -> differences
				cmd := exec.Command("sh", "-c", "exit 1")
				if runtime.GOOS == "windows" {
					cmd = exec.Command("cmd", "/C", "exit", "1")
				}
				return "", cmd.Run()
			}

			// stash show includes untracked too
			if len(args) == 5 &&
				args[0] == "stash" && args[1] == "show" &&
				args[2] == "--name-only" && args[3] == "--include-untracked" &&
				args[4] == "stash@{0}" {
				return "newfile.json\n", nil
			}

			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}
			calls = append(calls, args)

			// stash push ok
			if len(args) == 5 && args[0] == "stash" && args[1] == "push" && args[2] == "-u" {
				return nil
			}

			// checkout branch ok
			if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[2] == ref && args[3] == "origin/"+ref {
				return nil
			}

			// restore: stash@{0} fails for untracked
			if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}" && args[2] == "--" && args[3] == "newfile.json" {
				return fmt.Errorf("not in tree")
			}
			// restore: ^3 succeeds
			if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}^3" && args[2] == "--" && args[3] == "newfile.json" {
				return nil
			}

			// reset ok
			if len(args) == 1 && args[0] == "reset" {
				return nil
			}

			// drop ok
			if len(args) == 3 && args[0] == "stash" && args[1] == "drop" && args[2] == "stash@{0}" {
				return nil
			}

			return nil
		},
	}

	cause := fmt.Errorf("blocked by local changes")
	err := checkoutRemoteWithLocalChanges(ref, ref, runner, cause)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	found3 := false
	foundReset := false
	foundDrop := false

	for _, a := range calls {
		if slices.Equal(a, []string{"checkout", "stash@{0}^3", "--", "newfile.json"}) {
			found3 = true
		}
		if slices.Equal(a, []string{"reset"}) {
			foundReset = true
		}
		if slices.Equal(a, []string{"stash", "drop", "stash@{0}"}) {
			foundDrop = true
		}
	}

	if !found3 || !foundReset || !foundDrop {
		t.Fatalf("expected restore(^3)+reset+drop; got calls=%v", calls)
	}
}

func TestCheckoutBranch_RemoteBranch_StashShowFails_NoDrop(t *testing.T) {
	const branch = "lokalise-sync"
	const base = "master"

	exitErr1 := func() error {
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/C", "exit", "1")
		} else {
			cmd = exec.Command("sh", "-c", "exit 1")
		}
		return cmd.Run()
	}()

	var checkoutAttempts int
	dropped := false

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected binary: %s", name)
			}
			if len(args) == 5 && args[0] == "ls-remote" {
				return "deadbeef\trefs/heads/" + branch + "\n", nil
			}
			if len(args) == 5 && args[0] == "fetch" {
				return "", nil
			}
			if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain" {
				return " M locales/fr.json\n", nil
			}
			if len(args) >= 3 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "origin/"+branch {
				return "", exitErr1
			}
			if len(args) >= 4 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "--cached" && args[3] == "origin/"+branch {
				return "", exitErr1
			}
			// stash show fails
			if len(args) == 5 &&
				args[0] == "stash" && args[1] == "show" &&
				args[2] == "--name-only" &&
				args[3] == "--include-untracked" &&
				args[4] == "stash@{0}" {
				return "nope", fmt.Errorf("stash show failed")
			}
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				return fmt.Errorf("unexpected binary: %s", name)
			}

			if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[2] == branch && args[3] == "origin/"+branch {
				checkoutAttempts++
				if checkoutAttempts == 1 {
					return fmt.Errorf("blocked")
				}
				return nil
			}

			if len(args) == 5 && args[0] == "stash" && args[1] == "push" {
				return nil
			}

			if len(args) == 3 && args[0] == "stash" && args[1] == "drop" {
				dropped = true
				return nil
			}

			// upstream set might still be attempted by outer function only on success, so ignore branch calls
			if len(args) >= 1 && args[0] == "branch" {
				return nil
			}

			// any restore/drop should NOT happen after stash show failure (restore won't happen anyway)
			return nil
		},
	}

	err := checkoutBranch(branch, base, "", runner)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to list stashed files") {
		t.Fatalf("expected stash show error, got %v", err)
	}
	if dropped {
		t.Fatalf("stash must not be dropped on stash show failure")
	}
}

func TestCheckoutBranch_RemoteBranch_RestoreFails_NoDrop_StopsEarly(t *testing.T) {
	const branch = "lokalise-sync"
	const base = "master"

	exitErr1 := func() error {
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/C", "exit", "1")
		} else {
			cmd = exec.Command("sh", "-c", "exit 1")
		}
		return cmd.Run()
	}()

	var checkoutAttempts int
	dropped := false
	secondRestoreAttempted := false

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected binary: %s", name)
			}
			if len(args) == 5 && args[0] == "ls-remote" {
				return "deadbeef\trefs/heads/" + branch + "\n", nil
			}
			if len(args) == 5 && args[0] == "fetch" {
				return "", nil
			}
			if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain" {
				return " M locales/fr.json\n M i18n/fr.json\n", nil
			}
			if len(args) >= 3 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "origin/"+branch {
				return "", exitErr1
			}
			if len(args) >= 4 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "--cached" && args[3] == "origin/"+branch {
				return "", exitErr1
			}

			if len(args) == 5 &&
				args[0] == "stash" && args[1] == "show" &&
				args[2] == "--name-only" &&
				args[3] == "--include-untracked" &&
				args[4] == "stash@{0}" {
				return "locales/fr.json\ni18n/fr.json\n", nil
			}

			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				return fmt.Errorf("unexpected binary: %s", name)
			}

			if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[2] == branch && args[3] == "origin/"+branch {
				checkoutAttempts++
				if checkoutAttempts == 1 {
					return fmt.Errorf("blocked")
				}
				return nil
			}

			if len(args) == 5 && args[0] == "stash" && args[1] == "push" {
				return nil
			}

			// first restore fails
			if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}" && args[2] == "--" && args[3] == "locales/fr.json" {
				return fmt.Errorf("restore failed")
			}

			// second restore must NOT happen
			if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}" && args[2] == "--" && args[3] == "i18n/fr.json" {
				secondRestoreAttempted = true
				return nil
			}

			// first restore fails
			if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}^3" && args[2] == "--" && args[3] == "locales/fr.json" {
				return fmt.Errorf("restore failed")
			}

			// second restore must NOT happen
			if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}^3" && args[2] == "--" && args[3] == "i18n/fr.json" {
				secondRestoreAttempted = true
				return nil
			}

			// drop must NOT happen
			if len(args) == 3 && args[0] == "stash" && args[1] == "drop" {
				dropped = true
				return nil
			}

			if len(args) >= 1 && args[0] == "branch" {
				return nil
			}

			return nil
		},
	}

	err := checkoutBranch(branch, base, "", runner)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to restore locales/fr.json") {
		t.Fatalf("expected restore error, got %v", err)
	}
	if dropped {
		t.Fatalf("stash must not be dropped on restore failure")
	}
	if secondRestoreAttempted {
		t.Fatalf("expected restore loop to stop on first failure")
	}
}

func TestCheckoutBranch_RemoteBranch_DropFails_ReturnsError(t *testing.T) {
	const branch = "lokalise-sync"
	const base = "master"

	exitErr1 := func() error {
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/C", "exit", "1")
		} else {
			cmd = exec.Command("sh", "-c", "exit 1")
		}
		return cmd.Run()
	}()

	var checkoutAttempts int
	restoredAny := false

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected binary: %s", name)
			}
			if len(args) == 5 && args[0] == "ls-remote" {
				return "deadbeef\trefs/heads/" + branch + "\n", nil
			}
			if len(args) == 5 && args[0] == "fetch" {
				return "", nil
			}
			if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain" {
				return " M locales/fr.json\n", nil
			}
			if len(args) >= 3 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "origin/"+branch {
				return "", exitErr1
			}
			if len(args) >= 4 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "--cached" && args[3] == "origin/"+branch {
				return "", exitErr1
			}
			if len(args) == 5 &&
				args[0] == "stash" && args[1] == "show" &&
				args[2] == "--name-only" &&
				args[3] == "--include-untracked" &&
				args[4] == "stash@{0}" {
				return "locales/fr.json\n", nil
			}
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				return fmt.Errorf("unexpected binary: %s", name)
			}

			if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[2] == branch && args[3] == "origin/"+branch {
				checkoutAttempts++
				if checkoutAttempts == 1 {
					return fmt.Errorf("blocked")
				}
				return nil
			}

			if len(args) == 5 && args[0] == "stash" && args[1] == "push" {
				return nil
			}

			if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}" && args[2] == "--" {
				restoredAny = true
				return nil
			}

			// drop fails
			if len(args) == 3 && args[0] == "stash" && args[1] == "drop" && args[2] == "stash@{0}" {
				return fmt.Errorf("drop failed")
			}

			if len(args) >= 1 && args[0] == "branch" {
				return nil
			}

			return nil
		},
	}

	err := checkoutBranch(branch, base, "", runner)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to drop stash@{0}") {
		t.Fatalf("expected drop error, got %v", err)
	}
	if !restoredAny {
		t.Fatalf("expected at least one file to be restored before drop")
	}
}

func TestCheckoutBranch_RemoteBranch_EmptyStashFileList_StillDropsAndSucceeds(t *testing.T) {
	const branch = "lokalise-sync"
	const base = "master"

	exitErr1 := func() error {
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/C", "exit", "1")
		} else {
			cmd = exec.Command("sh", "-c", "exit 1")
		}
		return cmd.Run()
	}()

	var checkoutAttempts int
	dropped := false

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected binary: %s", name)
			}
			if len(args) == 5 && args[0] == "ls-remote" {
				return "deadbeef\trefs/heads/" + branch + "\n", nil
			}
			if len(args) == 5 && args[0] == "fetch" {
				return "", nil
			}
			if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain" {
				return " M locales/fr.json\n", nil
			}
			if len(args) >= 3 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "origin/"+branch {
				return "", exitErr1
			}
			if len(args) >= 4 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "--cached" && args[3] == "origin/"+branch {
				return "", exitErr1
			}
			// empty list
			if len(args) == 5 &&
				args[0] == "stash" &&
				args[1] == "show" &&
				args[2] == "--name-only" &&
				args[3] == "--include-untracked" &&
				args[4] == "stash@{0}" {
				return "\n", nil
			}
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				return fmt.Errorf("unexpected binary: %s", name)
			}

			if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[2] == branch && args[3] == "origin/"+branch {
				checkoutAttempts++
				if checkoutAttempts == 1 {
					return fmt.Errorf("blocked")
				}
				return nil
			}
			if len(args) == 5 && args[0] == "stash" && args[1] == "push" {
				return nil
			}

			if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}" && args[2] == "--" {
				return fmt.Errorf("restore should not be called on empty stash list")
			}
			if len(args) == 3 && args[0] == "stash" && args[1] == "drop" && args[2] == "stash@{0}" {
				dropped = true
				return nil
			}
			if len(args) >= 1 && args[0] == "branch" {
				return nil
			}
			return nil
		},
	}

	if err := checkoutBranch(branch, base, "", runner); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dropped {
		t.Fatalf("expected stash to be dropped")
	}
}

func TestCheckoutBranch_FetchesCorrectRefspec(t *testing.T) {
	fetched := ""

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected binary: %s", name)
			}

			// make hasRemote() return false for branchName
			if len(args) >= 1 && args[0] == "ls-remote" {
				return "", &mockExitError{code: 2}
			}

			if len(args) >= 1 && args[0] == "fetch" {
				fetched = strings.Join(args, " ")
				return "", nil
			}

			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				return fmt.Errorf("unexpected binary: %s", name)
			}

			// allow branch noise
			if len(args) >= 1 && args[0] == "branch" {
				return nil
			}

			if len(args) == 4 &&
				args[0] == "checkout" && args[1] == "-B" &&
				args[2] == "new_branch" && args[3] == "origin/main" {
				return nil
			}

			return fmt.Errorf("unexpected: %v", args)
		},
	}

	if err := checkoutBranch("new_branch", "main", "", runner); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(fetched, "+refs/heads/main:refs/remotes/origin/main") {
		t.Errorf("unexpected fetch refspec: %q", fetched)
	}
}

func TestCheckoutBranch_OverrideBranchExistsOnRemote(t *testing.T) {
	const branch = "lokalise-sync"
	const base = "main"

	runner := MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			// hasRemote(...) -> ls-remote --exit-code --heads origin <branch>
			if len(args) == 5 &&
				args[0] == "ls-remote" &&
				args[1] == "--exit-code" &&
				args[2] == "--heads" &&
				args[3] == "origin" &&
				args[4] == branch {
				return "deadbeef\trefs/heads/" + branch + "\n", nil
			}

			// fetch --no-tags --prune origin +refs/heads/<branch>:refs/remotes/origin/<branch>
			if len(args) == 5 &&
				args[0] == "fetch" && args[1] == "--no-tags" && args[2] == "--prune" &&
				args[3] == "origin" &&
				strings.HasPrefix(args[4], "+refs/heads/"+branch) &&
				strings.HasSuffix(args[4], ":refs/remotes/origin/"+branch) {
				return "", nil
			}

			t.Fatalf("unexpected capture: git %v", args)
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				return fmt.Errorf("unexpected binary: %s", name)
			}

			switch {
			// checkout -B lokalise-sync origin/lokalise-sync
			case len(args) == 4 &&
				args[0] == "checkout" && args[1] == "-B" &&
				args[2] == branch && args[3] == "origin/"+branch:
				return nil

			// branch --set-upstream-to=origin/lokalise-sync lokalise-sync   (== 3 args)
			case len(args) == 3 &&
				args[0] == "branch" &&
				args[1] == "--set-upstream-to=origin/"+branch &&
				args[2] == branch:
				return nil
			}

			return fmt.Errorf("unexpected run: git %v", args)
		},
	}

	if err := checkoutBranch(branch, base, "", &runner); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckoutBranch_OverrideMissing_FallbackToOriginBase_UnsetUpstream(t *testing.T) {
	const branch = "lokalise-sync"
	const base = "main"

	failedFirstRemoteCheckout := false
	var fetches []string

	runner := MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected binary: %s", name)
			}

			// hasRemote() -> ls-remote ... branch
			if len(args) >= 1 && args[0] == "ls-remote" {
				return "", &mockExitError{code: 2}
			}

			// fetch --no-tags --prune origin <refspec>  (== 5 args)
			if len(args) == 5 && args[0] == "fetch" {
				fetches = append(fetches, strings.Join(args, " "))
				return "", nil
			}

			return "", fmt.Errorf("unexpected capture: git %v", args)
		},
		RunFunc: func(name string, args ...string) error {
			switch {
			case len(args) == 4 && args[0] == "checkout" && args[2] == branch && args[3] == "origin/"+branch && !failedFirstRemoteCheckout:
				failedFirstRemoteCheckout = true
				return fmt.Errorf("missing remote branch")

			case len(args) == 4 && args[0] == "checkout" && args[2] == branch && args[3] == "origin/"+base:
				return nil

			case len(args) == 3 &&
				args[0] == "branch" &&
				args[1] == "--unset-upstream" &&
				args[2] == branch:
				return nil
			}
			return fmt.Errorf("unexpected run: git %v", args)
		},
	}

	if err := checkoutBranch(branch, base, "", &runner); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fetches) == 0 {
		t.Fatalf("expected fetch to be called at least once")
	}
}

func TestCheckoutBranch_HeadRefMatches_UsesRemoteHeadAndSetsUpstream(t *testing.T) {
	const branch = "feature/x"
	const base = "main"
	const head = "feature/x"

	runner := MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected binary: %s", name)
			}

			// make hasRemote(branchName) == false so code goes to headRef branch
			if len(args) >= 1 && args[0] == "ls-remote" {
				return "", &mockExitError{code: 2}
			}

			// allow fetch noise
			if len(args) >= 1 && args[0] == "fetch" {
				return "", nil
			}

			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				return fmt.Errorf("unexpected binary: %s", name)
			}

			switch {
			// expect checkout from origin/headRef (success)
			case len(args) == 4 &&
				args[0] == "checkout" && args[1] == "-B" &&
				args[2] == branch && args[3] == "origin/"+head:
				return nil

			// set upstream to origin/headRef
			case len(args) == 3 &&
				args[0] == "branch" &&
				args[1] == "--set-upstream-to=origin/"+head &&
				args[2] == branch:
				return nil
			}

			return fmt.Errorf("unexpected run: git %v", args)
		},
	}

	if err := checkoutBranch(branch, base, head, &runner); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckoutBranch_HasRemoteErrorStopsEarly(t *testing.T) {
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}
			if len(args) >= 1 && args[0] == "ls-remote" {
				return "fatal: auth failed", fmt.Errorf("network boom")
			}
			t.Fatalf("unexpected capture: git %v", args)
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			t.Fatalf("run must not be called, got: %s %v", name, args)
			return nil
		},
	}

	err := checkoutBranch("feature/x", "main", "", runner)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "git ls-remote failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckoutBranch_RemoteExists_CleanRepo_CheckoutBlockedReturnsOriginalCause(t *testing.T) {
	const branch = "lokalise-sync"

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			if len(args) >= 1 && args[0] == "ls-remote" {
				return "deadbeef\trefs/heads/" + branch + "\n", nil
			}

			if len(args) >= 1 && args[0] == "fetch" {
				return "", nil
			}

			if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain" {
				return "\n", nil
			}

			t.Fatalf("unexpected capture: git %v", args)
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			if len(args) == 4 &&
				args[0] == "checkout" &&
				args[1] == "-B" &&
				args[2] == branch &&
				args[3] == "origin/"+branch {
				return fmt.Errorf("Your local changes would be overwritten by checkout")
			}

			if len(args) >= 1 && args[0] == "branch" {
				t.Fatalf("upstream must not be set on failure")
			}

			t.Fatalf("unexpected run: git %v", args)
			return nil
		},
	}

	err := checkoutBranch(branch, "main", "", runner)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Your local changes would be overwritten by checkout") {
		t.Fatalf("expected original checkout cause, got %v", err)
	}
}

func TestHasRemote(t *testing.T) {
	t.Run("remote exists", func(t *testing.T) {
		wantRef := "feature/x"

		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					t.Fatalf("unexpected binary: %s", name)
				}

				wantArgs := []string{"ls-remote", "--exit-code", "--heads", "origin", wantRef}
				if len(args) != len(wantArgs) {
					t.Fatalf("unexpected args count: got %v want %v", args, wantArgs)
				}
				for i := range wantArgs {
					if args[i] != wantArgs[i] {
						t.Fatalf("unexpected args: got %v want %v", args, wantArgs)
					}
				}

				return "deadbeef\trefs/heads/feature/x\n", nil
			},
		}

		ok, err := hasRemote(runner, wantRef)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatalf("expected remote branch to exist")
		}
	})

	t.Run("remote does not exist returns false without error", func(t *testing.T) {
		wantRef := "missing-branch"

		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					t.Fatalf("unexpected binary: %s", name)
				}

				wantArgs := []string{"ls-remote", "--exit-code", "--heads", "origin", wantRef}
				if len(args) != len(wantArgs) {
					t.Fatalf("unexpected args count: got %v want %v", args, wantArgs)
				}
				for i := range wantArgs {
					if args[i] != wantArgs[i] {
						t.Fatalf("unexpected args: got %v want %v", args, wantArgs)
					}
				}

				return "", &mockExitError{code: 2}
			},
		}

		ok, err := hasRemote(runner, wantRef)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Fatalf("expected remote branch to be reported missing")
		}
	})

	t.Run("other ls-remote error is returned", func(t *testing.T) {
		wantRef := "feature/x"

		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					t.Fatalf("unexpected binary: %s", name)
				}

				wantArgs := []string{"ls-remote", "--exit-code", "--heads", "origin", wantRef}
				if len(args) != len(wantArgs) {
					t.Fatalf("unexpected args count: got %v want %v", args, wantArgs)
				}
				for i := range wantArgs {
					if args[i] != wantArgs[i] {
						t.Fatalf("unexpected args: got %v want %v", args, wantArgs)
					}
				}

				return "fatal: auth failed", fmt.Errorf("network boom")
			},
		}

		ok, err := hasRemote(runner, wantRef)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if ok {
			t.Fatalf("expected ok=false on ls-remote failure")
		}
		if !strings.Contains(err.Error(), `git ls-remote failed for ref "feature/x"`) {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(err.Error(), "fatal: auth failed") {
			t.Fatalf("expected command output in error, got %v", err)
		}
	})
}
