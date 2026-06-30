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
				if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain=v1" {
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

		stashRef, didStash, err := stashIfDirty(runner, "lokalise-temp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if stashRef != "" {
			t.Fatalf("expected empty stashRef for clean worktree, got %q", stashRef)
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

				if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain=v1" {
					return " M locales/fr.json\n", nil
				}

				if len(args) == 3 &&
					args[0] == "rev-parse" &&
					args[1] == "--verify" &&
					args[2] == "stash@{0}" {
					return "abc123stash\n", nil
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

		stashRef, didStash, err := stashIfDirty(runner, "lokalise-temp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if stashRef != "abc123stash" {
			t.Fatalf("expected stash ref, got %q", stashRef)
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

		stashRef, didStash, err := stashIfDirty(runner, "lokalise-temp")
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if stashRef != "" {
			t.Fatalf("expected empty stashRef on status error, got %q", stashRef)
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
				if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain=v1" {
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

		stashRef, didStash, err := stashIfDirty(runner, "lokalise-temp")
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if stashRef != "" {
			t.Fatalf("expected empty stashRef when stash push fails, got %q", stashRef)
		}
		if didStash {
			t.Fatalf("expected didStash=false when stash push fails")
		}
		if !strings.Contains(err.Error(), "failed to stash changes") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rev-parse error is returned", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					t.Fatalf("unexpected binary: %s", name)
				}

				if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain=v1" {
					return " M locales/fr.json\n", nil
				}

				if len(args) == 3 &&
					args[0] == "rev-parse" &&
					args[1] == "--verify" &&
					args[2] == "stash@{0}" {
					return "fatal: bad revision", fmt.Errorf("rev-parse failed")
				}

				t.Fatalf("unexpected capture: git %v", args)
				return "", nil
			},
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					t.Fatalf("unexpected binary: %s", name)
				}

				if len(args) == 5 && args[0] == "stash" && args[1] == "push" {
					return nil
				}

				t.Fatalf("unexpected run: git %v", args)
				return nil
			},
		}

		stashRef, didStash, err := stashIfDirty(runner, "lokalise-temp")
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if stashRef != "" {
			t.Fatalf("expected empty stashRef on rev-parse error, got %q", stashRef)
		}
		if didStash {
			t.Fatalf("expected didStash=false on rev-parse error")
		}
		if !strings.Contains(err.Error(), "failed to resolve created stash ref") {
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

func TestFindStashSelectorByHash(t *testing.T) {
	t.Run("finds selector by exact hash", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					return "", fmt.Errorf("unexpected binary: %s", name)
				}

				if len(args) == 3 &&
					args[0] == "stash" &&
					args[1] == "list" &&
					args[2] == "--format=%H%x09%gd" {
					return "aaa111\tstash@{0}\nabc123stash\tstash@{1}\n", nil
				}

				return "", fmt.Errorf("unexpected capture: git %v", args)
			},
		}

		got, err := findStashSelectorByHash(runner, "abc123stash")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got != "stash@{1}" {
			t.Fatalf("expected stash@{1}, got %q", got)
		}
	})

	t.Run("trims input hash", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					return "", fmt.Errorf("unexpected binary: %s", name)
				}

				if len(args) == 3 &&
					args[0] == "stash" &&
					args[1] == "list" &&
					args[2] == "--format=%H%x09%gd" {
					return "abc123stash\tstash@{0}\n", nil
				}

				return "", fmt.Errorf("unexpected capture: git %v", args)
			},
		}

		got, err := findStashSelectorByHash(runner, "  abc123stash  ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got != "stash@{0}" {
			t.Fatalf("expected stash@{0}, got %q", got)
		}
	})

	t.Run("empty hash returns error", func(t *testing.T) {
		runner := &MockCommandRunner{}

		got, err := findStashSelectorByHash(runner, "   ")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if got != "" {
			t.Fatalf("expected empty selector, got %q", got)
		}

		if !strings.Contains(err.Error(), "stash hash is empty") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("stash list error is returned", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					return "", fmt.Errorf("unexpected binary: %s", name)
				}

				if len(args) == 3 &&
					args[0] == "stash" &&
					args[1] == "list" &&
					args[2] == "--format=%H%x09%gd" {
					return "fatal: broken stash", fmt.Errorf("stash list failed")
				}

				return "", fmt.Errorf("unexpected capture: git %v", args)
			},
		}

		got, err := findStashSelectorByHash(runner, "abc123stash")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if got != "" {
			t.Fatalf("expected empty selector, got %q", got)
		}

		if !strings.Contains(err.Error(), "failed to list stashes") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ignores malformed lines", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					return "", fmt.Errorf("unexpected binary: %s", name)
				}

				if len(args) == 3 &&
					args[0] == "stash" &&
					args[1] == "list" &&
					args[2] == "--format=%H%x09%gd" {
					return "malformed-line-without-tab\nabc123stash\tstash@{2}\n", nil
				}

				return "", fmt.Errorf("unexpected capture: git %v", args)
			},
		}

		got, err := findStashSelectorByHash(runner, "abc123stash")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got != "stash@{2}" {
			t.Fatalf("expected stash@{2}, got %q", got)
		}
	})

	t.Run("matched hash with empty selector returns error", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					return "", fmt.Errorf("unexpected binary: %s", name)
				}

				if len(args) == 3 &&
					args[0] == "stash" &&
					args[1] == "list" &&
					args[2] == "--format=%H%x09%gd" {
					return "abc123stash\t   \n", nil
				}

				return "", fmt.Errorf("unexpected capture: git %v", args)
			},
		}

		got, err := findStashSelectorByHash(runner, "abc123stash")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if got != "" {
			t.Fatalf("expected empty selector, got %q", got)
		}

		if !strings.Contains(err.Error(), "selector is empty") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("hash not found returns error", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					return "", fmt.Errorf("unexpected binary: %s", name)
				}

				if len(args) == 3 &&
					args[0] == "stash" &&
					args[1] == "list" &&
					args[2] == "--format=%H%x09%gd" {
					return "aaa111\tstash@{0}\nbbb222\tstash@{1}\n", nil
				}

				return "", fmt.Errorf("unexpected capture: git %v", args)
			},
		}

		got, err := findStashSelectorByHash(runner, "abc123stash")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if got != "" {
			t.Fatalf("expected empty selector, got %q", got)
		}

		if !strings.Contains(err.Error(), "was not found in stash list") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestDropStashByHash(t *testing.T) {
	t.Run("drops resolved stash selector", func(t *testing.T) {
		var droppedSelector string

		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					return "", fmt.Errorf("unexpected binary: %s", name)
				}

				if len(args) == 3 &&
					args[0] == "stash" &&
					args[1] == "list" &&
					args[2] == "--format=%H%x09%gd" {
					return "abc123stash\tstash@{3}\n", nil
				}

				return "", fmt.Errorf("unexpected capture: git %v", args)
			},
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					return fmt.Errorf("unexpected binary: %s", name)
				}

				if len(args) == 3 &&
					args[0] == "stash" &&
					args[1] == "drop" {
					droppedSelector = args[2]
					return nil
				}

				return fmt.Errorf("unexpected run: git %v", args)
			},
		}

		if err := dropStashByHash(runner, "abc123stash"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if droppedSelector != "stash@{3}" {
			t.Fatalf("expected stash@{3} to be dropped, got %q", droppedSelector)
		}
	})

	t.Run("returns resolver error", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					return "", fmt.Errorf("unexpected binary: %s", name)
				}

				if len(args) == 3 &&
					args[0] == "stash" &&
					args[1] == "list" &&
					args[2] == "--format=%H%x09%gd" {
					return "aaa111\tstash@{0}\n", nil
				}

				return "", fmt.Errorf("unexpected capture: git %v", args)
			},
		}

		err := dropStashByHash(runner, "abc123stash")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if !strings.Contains(err.Error(), "was not found in stash list") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("returns drop error", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					return "", fmt.Errorf("unexpected binary: %s", name)
				}

				if len(args) == 3 &&
					args[0] == "stash" &&
					args[1] == "list" &&
					args[2] == "--format=%H%x09%gd" {
					return "abc123stash\tstash@{0}\n", nil
				}

				return "", fmt.Errorf("unexpected capture: git %v", args)
			},
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					return fmt.Errorf("unexpected binary: %s", name)
				}

				if len(args) == 3 &&
					args[0] == "stash" &&
					args[1] == "drop" &&
					args[2] == "stash@{0}" {
					return fmt.Errorf("drop failed")
				}

				return fmt.Errorf("unexpected run: git %v", args)
			},
		}

		err := dropStashByHash(runner, "abc123stash")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if !strings.Contains(err.Error(), "failed to drop stash@{0}") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
