package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestWorktreeEqualsRef(t *testing.T) {
	const ref = "origin/feature/x"

	t.Run("worktree and index both match", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					t.Fatalf("unexpected binary: %s", name)
				}
				return "", nil
			},
		}

		same, err := worktreeEqualsRef(ref, runner)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !same {
			t.Fatalf("expected worktree to match ref")
		}
	})

	t.Run("worktree differs", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					t.Fatalf("unexpected binary: %s", name)
				}
				if len(args) == 3 && args[0] == "diff" && args[1] == "--quiet" && args[2] == ref {
					return "", &mockExitError{code: 1}
				}
				t.Fatalf("unexpected capture: git %v", args)
				return "", nil
			},
		}

		same, err := worktreeEqualsRef(ref, runner)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if same {
			t.Fatalf("expected worktree difference to return same=false")
		}
	})

	t.Run("index differs", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					t.Fatalf("unexpected binary: %s", name)
				}
				if len(args) == 3 && args[0] == "diff" && args[1] == "--quiet" && args[2] == ref {
					return "", nil
				}
				if len(args) == 4 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "--cached" && args[3] == ref {
					return "", &mockExitError{code: 1}
				}
				t.Fatalf("unexpected capture: git %v", args)
				return "", nil
			},
		}

		same, err := worktreeEqualsRef(ref, runner)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if same {
			t.Fatalf("expected cached diff to return same=false")
		}
	})

	t.Run("unexpected diff error is returned", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					t.Fatalf("unexpected binary: %s", name)
				}
				if len(args) == 3 && args[0] == "diff" && args[1] == "--quiet" && args[2] == ref {
					return "", fmt.Errorf("diff exploded")
				}
				t.Fatalf("unexpected capture: git %v", args)
				return "", nil
			},
		}

		same, err := worktreeEqualsRef(ref, runner)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if same {
			t.Fatalf("expected same=false on diff error")
		}
		if !strings.Contains(err.Error(), "git diff failed") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestReadWorktreeStatus(t *testing.T) {
	t.Run("clean worktree", func(t *testing.T) {
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
		}

		status, hasUntracked, err := readWorktreeStatus(runner)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status != "" {
			t.Fatalf("expected empty status, got %q", status)
		}
		if hasUntracked {
			t.Fatalf("expected hasUntracked=false for clean worktree")
		}
	})

	t.Run("tracked changes without untracked", func(t *testing.T) {
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
		}

		status, hasUntracked, err := readWorktreeStatus(runner)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status == "" {
			t.Fatalf("expected non-empty status")
		}
		if hasUntracked {
			t.Fatalf("expected hasUntracked=false for tracked-only changes")
		}
	})

	t.Run("untracked files are detected", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					t.Fatalf("unexpected binary: %s", name)
				}
				if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain" {
					return "?? newfile.json\n M locales/fr.json\n", nil
				}
				t.Fatalf("unexpected capture: git %v", args)
				return "", nil
			},
		}

		status, hasUntracked, err := readWorktreeStatus(runner)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status == "" {
			t.Fatalf("expected non-empty status")
		}
		if !hasUntracked {
			t.Fatalf("expected hasUntracked=true when ?? entries exist")
		}
	})

	t.Run("status command error is returned with output", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					t.Fatalf("unexpected binary: %s", name)
				}
				if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain" {
					return "fatal: not a git repository", fmt.Errorf("status failed")
				}
				t.Fatalf("unexpected capture: git %v", args)
				return "", nil
			},
		}

		status, hasUntracked, err := readWorktreeStatus(runner)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if status != "" {
			t.Fatalf("expected empty status on error, got %q", status)
		}
		if hasUntracked {
			t.Fatalf("expected hasUntracked=false on error")
		}
		if !strings.Contains(err.Error(), "failed to check status") {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(err.Error(), "fatal: not a git repository") {
			t.Fatalf("expected command output in error, got %v", err)
		}
	})
}
