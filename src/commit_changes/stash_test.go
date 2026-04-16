package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestStashIfDirty(t *testing.T) {
	t.Run("clean worktree does not stash", func(t *testing.T) {
		stashCalled := false

		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					t.Fatalf("unexpected binary: %s", name)
				}
				if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain" {
					return "\n", nil
				}
				t.Fatalf("unexpected capture: git %v", args)
				return "", nil
			},
			RunFunc: func(name string, args ...string) error {
				stashCalled = true
				return nil
			},
		}

		didStash, err := stashIfDirty(runner, "lokalise-temp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if didStash {
			t.Fatalf("expected didStash=false for clean worktree")
		}
		if stashCalled {
			t.Fatalf("stash must not be called for clean worktree")
		}
	})

	t.Run("dirty worktree is stashed", func(t *testing.T) {
		stashCalled := false

		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					t.Fatalf("unexpected binary: %s", name)
				}
				if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain" {
					return " M locales/fr.json\n", nil
				}
				t.Fatalf("unexpected capture: git %v", args)
				return "", nil
			},
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					t.Fatalf("unexpected binary: %s", name)
				}
				if len(args) == 5 &&
					args[0] == "stash" &&
					args[1] == "push" &&
					args[2] == "-u" &&
					args[3] == "-m" &&
					args[4] == "lokalise-temp" {
					stashCalled = true
					return nil
				}
				t.Fatalf("unexpected run: git %v", args)
				return nil
			},
		}

		didStash, err := stashIfDirty(runner, "lokalise-temp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !didStash {
			t.Fatalf("expected didStash=true for dirty worktree")
		}
		if !stashCalled {
			t.Fatalf("expected stash push to be called")
		}
	})

	t.Run("status error is returned", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					t.Fatalf("unexpected binary: %s", name)
				}
				return "fatal: broken repo", fmt.Errorf("status failed")
			},
		}

		didStash, err := stashIfDirty(runner, "lokalise-temp")
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if didStash {
			t.Fatalf("expected didStash=false on status error")
		}
		if !strings.Contains(err.Error(), "failed to check git status") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("stash push error is returned", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					t.Fatalf("unexpected binary: %s", name)
				}
				if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain" {
					return " M locales/fr.json\n", nil
				}
				t.Fatalf("unexpected capture: git %v", args)
				return "", nil
			},
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					t.Fatalf("unexpected binary: %s", name)
				}
				if len(args) == 5 && args[0] == "stash" && args[1] == "push" {
					return fmt.Errorf("stash failed")
				}
				t.Fatalf("unexpected run: git %v", args)
				return nil
			},
		}

		didStash, err := stashIfDirty(runner, "lokalise-temp")
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if didStash {
			t.Fatalf("expected didStash=false when stash push fails")
		}
		if !strings.Contains(err.Error(), "failed to stash changes") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestRestoreFileFromStash(t *testing.T) {
	t.Run("restore from stash tree succeeds", func(t *testing.T) {
		var calls [][]string

		runner := &MockCommandRunner{
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					t.Fatalf("unexpected binary: %s", name)
				}
				calls = append(calls, args)
				if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}" && args[2] == "--" && args[3] == "locales/fr.json" {
					return nil
				}
				t.Fatalf("unexpected run: git %v", args)
				return nil
			},
		}

		if err := restoreFileFromStash(runner, "stash@{0}", "locales/fr.json"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(calls) != 1 {
			t.Fatalf("expected 1 restore attempt, got %v", calls)
		}
	})

	t.Run("falls back to third parent for untracked file", func(t *testing.T) {
		var calls [][]string

		runner := &MockCommandRunner{
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					t.Fatalf("unexpected binary: %s", name)
				}
				calls = append(calls, args)

				if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}" && args[2] == "--" && args[3] == "newfile.json" {
					return fmt.Errorf("not in tree")
				}
				if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}^3" && args[2] == "--" && args[3] == "newfile.json" {
					return nil
				}

				t.Fatalf("unexpected run: git %v", args)
				return nil
			},
		}

		if err := restoreFileFromStash(runner, "stash@{0}", "newfile.json"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(calls) != 2 {
			t.Fatalf("expected 2 restore attempts, got %v", calls)
		}
	})

	t.Run("returns error when both restore attempts fail", func(t *testing.T) {
		var calls [][]string

		runner := &MockCommandRunner{
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					t.Fatalf("unexpected binary: %s", name)
				}
				calls = append(calls, args)

				if len(args) == 4 && args[0] == "checkout" && (args[1] == "stash@{0}" || args[1] == "stash@{0}^3") {
					return fmt.Errorf("restore failed")
				}
				t.Fatalf("unexpected run: git %v", args)
				return nil
			},
		}

		err := restoreFileFromStash(runner, "stash@{0}", "newfile.json")
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if len(calls) != 2 {
			t.Fatalf("expected 2 restore attempts, got %v", calls)
		}
	})
}
