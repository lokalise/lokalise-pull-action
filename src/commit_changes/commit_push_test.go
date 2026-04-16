package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestCommitAndPushChanges_ForcePush_UsesForceWithLease(t *testing.T) {
	t.Setenv("GITHUB_ACTOR", "test_actor")
	t.Setenv("GITHUB_SHA", "1234567890abcdef")
	t.Setenv("TEMP_BRANCH_PREFIX", "lok")
	t.Setenv("TRANSLATIONS_PATH", "locales")
	t.Setenv("BASE_LANG", "en")
	t.Setenv("FLAT_NAMING", "true")
	t.Setenv("ALWAYS_PULL_BASE", "true")
	t.Setenv("FORCE_PUSH", "true")
	t.Setenv("FILE_EXT", "json")
	t.Setenv("BASE_REF", "main")
	t.Setenv("GIT_COMMIT_MESSAGE", "msg")

	var branchName string
	var pushArgs []string

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			// validate generated branch name
			if len(args) == 3 && args[0] == "check-ref-format" && args[1] == "--branch" {
				return "", nil
			}

			// hasRemote(branchName): not found
			if len(args) == 5 && args[0] == "ls-remote" && args[1] == "--exit-code" && args[2] == "--heads" && args[3] == "origin" {
				return "", &mockExitError{code: 2}
			}

			// managedpaths: HEAD exists
			if len(args) == 3 && args[0] == "rev-parse" && args[1] == "--verify" && args[2] == "HEAD" {
				return "ok", nil
			}

			// managedpaths: changed tracked files
			if len(args) == 5 &&
				args[0] == "-c" &&
				args[1] == "core.quotepath=false" &&
				args[2] == "diff" &&
				args[3] == "--name-only" &&
				args[4] == "HEAD" {
				return "locales/en.json\n", nil
			}

			// managedpaths: no untracked files
			if len(args) == 5 &&
				args[0] == "-c" &&
				args[1] == "core.quotepath=false" &&
				args[2] == "ls-files" &&
				args[3] == "--others" &&
				args[4] == "--exclude-standard" {
				return "", nil
			}

			// commitAndPush: staged diff -> has changes
			if len(args) == 3 && args[0] == "diff" && args[1] == "--name-only" && args[2] == "--cached" {
				return "locales/en.json\n", nil
			}

			// commit ok
			if len(args) >= 1 && args[0] == "commit" {
				return "ok", nil
			}

			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			if len(args) == 4 && args[0] == "config" && args[1] == "--global" {
				return nil
			}

			if len(args) == 5 && args[0] == "fetch" && args[1] == "--no-tags" && args[2] == "--prune" && args[3] == "origin" {
				return nil
			}

			if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[3] == "origin/main" {
				branchName = args[2]
				return nil
			}

			if len(args) == 3 && args[0] == "branch" && args[1] == "--unset-upstream" {
				return nil
			}

			if len(args) >= 4 && args[0] == "add" && args[1] == "-A" && args[2] == "--" {
				if args[3] != "locales/en.json" {
					t.Fatalf("expected locales/en.json to be staged, got args: %v", args)
				}
				return nil
			}

			if len(args) >= 1 && args[0] == "push" {
				pushArgs = args
				return nil
			}

			return fmt.Errorf("unexpected run: git %v", args)
		},
	}

	_, err := commitAndPushChanges(runner)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	want := []string{"push", "--force-with-lease", "origin", branchName}
	if !slices.Equal(pushArgs, want) {
		t.Fatalf("push args mismatch: want %v got %v", want, pushArgs)
	}
}

func TestCommitAndPushChanges_HappyPath_NoForce_NoOverride(t *testing.T) {
	t.Setenv("GITHUB_ACTOR", "test_actor")
	t.Setenv("GITHUB_SHA", "1234567890abcdef")
	t.Setenv("TEMP_BRANCH_PREFIX", "lok")
	t.Setenv("TRANSLATIONS_PATH", "locales")
	t.Setenv("BASE_LANG", "en")
	t.Setenv("FLAT_NAMING", "true")
	t.Setenv("ALWAYS_PULL_BASE", "true")
	t.Setenv("FORCE_PUSH", "false")
	t.Setenv("FILE_EXT", "json")
	t.Setenv("BASE_REF", "main")
	t.Setenv("GIT_COMMIT_MESSAGE", "Translations update")

	var branchName string
	var pushed []string
	commitCalled := false
	diffCachedCalled := false
	addCalled := false

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			// branch name validation
			if len(args) == 3 && args[0] == "check-ref-format" && args[1] == "--branch" {
				return "", nil
			}

			// hasRemote(branchName) -> false
			if len(args) == 5 && args[0] == "ls-remote" && args[1] == "--exit-code" && args[2] == "--heads" && args[3] == "origin" {
				return "", &mockExitError{code: 2}
			}

			// managedpaths: HEAD exists
			if len(args) == 3 && args[0] == "rev-parse" && args[1] == "--verify" && args[2] == "HEAD" {
				return "ok", nil
			}

			// managedpaths: changed tracked files
			if len(args) == 5 &&
				args[0] == "-c" &&
				args[1] == "core.quotepath=false" &&
				args[2] == "diff" &&
				args[3] == "--name-only" &&
				args[4] == "HEAD" {
				return "locales/fr.json\n", nil
			}

			// managedpaths: no untracked files
			if len(args) == 5 &&
				args[0] == "-c" &&
				args[1] == "core.quotepath=false" &&
				args[2] == "ls-files" &&
				args[3] == "--others" &&
				args[4] == "--exclude-standard" {
				return "", nil
			}

			// commitAndPush: inspect staged diff
			if len(args) == 3 && args[0] == "diff" && args[1] == "--name-only" && args[2] == "--cached" {
				diffCachedCalled = true
				return "locales/fr.json\n", nil
			}

			// commit
			if len(args) >= 1 && args[0] == "commit" {
				commitCalled = true

				found := false
				for i := 0; i+1 < len(args); i++ {
					if args[i] == "-m" && args[i+1] == "Translations update" {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected commit message, got args: %v", args)
				}

				return "ok", nil
			}

			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			// git config --global user.*
			if len(args) == 4 && args[0] == "config" && args[1] == "--global" {
				return nil
			}

			// fetch main
			if len(args) == 5 && args[0] == "fetch" && args[1] == "--no-tags" && args[2] == "--prune" && args[3] == "origin" {
				return nil
			}

			// checkout -B <branch> origin/main
			if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[3] == "origin/main" {
				branchName = args[2]
				if !strings.HasPrefix(branchName, "lok_main_123456_") {
					t.Fatalf("unexpected branch name: %q", branchName)
				}
				return nil
			}

			// unset upstream
			if len(args) == 3 && args[0] == "branch" && args[1] == "--unset-upstream" {
				return nil
			}

			// git add -A -- ...
			if len(args) >= 4 && args[0] == "add" && args[1] == "-A" && args[2] == "--" {
				addCalled = true
				if args[3] != "locales/fr.json" {
					t.Fatalf("expected locales/fr.json to be staged, got args: %v", args)
				}
				return nil
			}

			// push origin <branch>
			if len(args) == 3 && args[0] == "push" && args[1] == "origin" {
				pushed = args
				if args[2] != branchName {
					t.Fatalf("push branch mismatch: want %q got %q", branchName, args[2])
				}
				return nil
			}

			return fmt.Errorf("unexpected run: git %v", args)
		},
	}

	gotBranch, err := commitAndPushChanges(runner)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if gotBranch != branchName {
		t.Fatalf("branch mismatch: got %q want %q", gotBranch, branchName)
	}
	if !diffCachedCalled || !commitCalled || !addCalled {
		t.Fatalf("expected staged diff + commit + add; got diffCached=%v commit=%v add=%v", diffCachedCalled, commitCalled, addCalled)
	}
	if len(pushed) == 0 {
		t.Fatalf("expected push")
	}
}

func TestCommitAndPush_PushError(t *testing.T) {
	diffCalled := false
	commitCalled := false

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name == "git" && len(args) >= 1 && args[0] == "diff" {
				if len(args) >= 3 && args[1] == "--name-only" && args[2] == "--cached" {
					diffCalled = true
					return "locales/en.json\n", nil
				}
			}
			if name == "git" && args[0] == "commit" {
				commitCalled = true
				return "Files committed", nil
			}
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name == "git" && args[0] == "push" && args[1] == "origin" {
				return fmt.Errorf("push failed")
			}
			return nil
		},
	}

	config := &Config{}

	err := commitAndPush("test_branch", runner, config)
	if err == nil {
		t.Errorf("Expected error, but got nil")
	} else if !strings.Contains(err.Error(), "push failed") {
		t.Errorf("Expected push error, but got %v", err)
	}

	if !diffCalled || !commitCalled {
		t.Fatalf("Expected both diff --cached and commit to be called")
	}
}

func TestCommitAndPushChanges_NoChanges_ReturnsErrNoChanges_NoPush(t *testing.T) {
	t.Setenv("GITHUB_ACTOR", "test_actor")
	t.Setenv("GITHUB_SHA", "1234567890abcdef")
	t.Setenv("TEMP_BRANCH_PREFIX", "lok")
	t.Setenv("TRANSLATIONS_PATH", "locales")
	t.Setenv("BASE_LANG", "en")
	t.Setenv("FLAT_NAMING", "true")
	t.Setenv("ALWAYS_PULL_BASE", "true")
	t.Setenv("FORCE_PUSH", "false")
	t.Setenv("FILE_EXT", "json")
	t.Setenv("BASE_REF", "main")

	addCalled := false
	pushCalled := false
	commitCalled := false

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			// generated branch validation
			if len(args) == 3 && args[0] == "check-ref-format" && args[1] == "--branch" {
				return "", nil
			}

			// hasRemote(branchName): not found
			if len(args) == 5 &&
				args[0] == "ls-remote" &&
				args[1] == "--exit-code" &&
				args[2] == "--heads" &&
				args[3] == "origin" {
				return "", &mockExitError{code: 2}
			}

			// managedpaths: HEAD exists
			if len(args) == 3 &&
				args[0] == "rev-parse" &&
				args[1] == "--verify" &&
				args[2] == "HEAD" {
				return "ok", nil
			}

			// managedpaths: no tracked changes
			if len(args) == 5 &&
				args[0] == "-c" &&
				args[1] == "core.quotepath=false" &&
				args[2] == "diff" &&
				args[3] == "--name-only" &&
				args[4] == "HEAD" {
				return "", nil
			}

			// managedpaths: no untracked files
			if len(args) == 5 &&
				args[0] == "-c" &&
				args[1] == "core.quotepath=false" &&
				args[2] == "ls-files" &&
				args[3] == "--others" &&
				args[4] == "--exclude-standard" {
				return "", nil
			}

			if len(args) == 5 &&
				args[0] == "fetch" &&
				args[1] == "--no-tags" &&
				args[2] == "--prune" &&
				args[3] == "origin" {
				return "", nil
			}

			// must not reach commitAndPush() stage
			if len(args) == 3 && args[0] == "diff" && args[1] == "--name-only" && args[2] == "--cached" {
				t.Fatalf("diff --cached must not be called when there are no managed files")
			}
			if len(args) >= 1 && args[0] == "commit" {
				commitCalled = true
				t.Fatalf("commit must not be called when there are no managed files")
			}

			t.Fatalf("unexpected capture: git %v", args)
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			if len(args) == 4 && args[0] == "config" && args[1] == "--global" {
				return nil
			}

			if len(args) == 4 &&
				args[0] == "checkout" &&
				args[1] == "-B" &&
				args[3] == "origin/main" {
				return nil
			}

			if len(args) == 3 &&
				args[0] == "branch" &&
				args[1] == "--unset-upstream" {
				return nil
			}

			if len(args) >= 4 &&
				args[0] == "add" &&
				args[1] == "-A" &&
				args[2] == "--" {
				addCalled = true
				t.Fatalf("git add must not be called when there are no managed files")
			}

			if len(args) >= 1 && args[0] == "push" {
				pushCalled = true
				t.Fatalf("push must not be called when there are no managed files")
			}

			t.Fatalf("unexpected run: git %v", args)
			return nil
		},
	}

	_, err := commitAndPushChanges(runner)
	if err != ErrNoChanges {
		t.Fatalf("want ErrNoChanges, got %v", err)
	}
	if addCalled {
		t.Fatalf("git add must not be called")
	}
	if commitCalled {
		t.Fatalf("commit must not be called")
	}
	if pushCalled {
		t.Fatalf("push must not be called")
	}
}

func TestCommitAndPush_CommitFails_NoPush(t *testing.T) {
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name == "git" && len(args) >= 3 && args[0] == "diff" && args[1] == "--name-only" && args[2] == "--cached" {
				return "locales/en.json\n", nil
			}
			if name == "git" && len(args) >= 1 && args[0] == "commit" {
				return "hook said nope", fmt.Errorf("commit hook failed")
			}
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name == "git" && len(args) >= 1 && args[0] == "push" {
				t.Fatalf("push must not be called when commit fails")
			}
			return nil
		},
	}

	err := commitAndPush("branch", runner, &Config{GitCommitMessage: "msg"})
	if err == nil || !strings.Contains(err.Error(), "failed to commit changes") {
		t.Fatalf("Expected commit error, got %v", err)
	}
}

func TestCommitAndPushChanges_SyntheticBase_UsesRemoteHEAD(t *testing.T) {
	t.Setenv("GITHUB_ACTOR", "test_actor")
	t.Setenv("GITHUB_SHA", "1234567890abcdef")
	t.Setenv("TEMP_BRANCH_PREFIX", "lok")
	t.Setenv("TRANSLATIONS_PATH", "locales")
	t.Setenv("BASE_LANG", "en")
	t.Setenv("FLAT_NAMING", "true")
	t.Setenv("ALWAYS_PULL_BASE", "true")
	t.Setenv("FORCE_PUSH", "false")
	t.Setenv("FILE_EXT", "json")
	t.Setenv("BASE_REF", "123/merge") // synthetic

	var fetched []string

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			// resolveRealBase: git ls-remote --symref origin HEAD
			if len(args) == 4 &&
				args[0] == "ls-remote" &&
				args[1] == "--symref" &&
				args[2] == "origin" &&
				args[3] == "HEAD" {
				return "ref: refs/heads/master\tHEAD\n012345\tHEAD\n", nil
			}

			// hasRemote(branchName): not found
			if len(args) == 5 &&
				args[0] == "ls-remote" &&
				args[1] == "--exit-code" &&
				args[2] == "--heads" &&
				args[3] == "origin" {
				return "", &mockExitError{code: 2}
			}

			// fetch master
			if len(args) == 5 &&
				args[0] == "fetch" &&
				args[1] == "--no-tags" &&
				args[2] == "--prune" &&
				args[3] == "origin" {
				fetched = append(fetched, args[4])
				return "", nil
			}

			// managedpaths: HEAD exists
			if len(args) == 3 &&
				args[0] == "rev-parse" &&
				args[1] == "--verify" &&
				args[2] == "HEAD" {
				return "ok", nil
			}

			// managedpaths: tracked changed files
			if len(args) == 5 &&
				args[0] == "-c" &&
				args[1] == "core.quotepath=false" &&
				args[2] == "diff" &&
				args[3] == "--name-only" &&
				args[4] == "HEAD" {
				return "locales/fr.json\n", nil
			}

			// managedpaths: no untracked files
			if len(args) == 5 &&
				args[0] == "-c" &&
				args[1] == "core.quotepath=false" &&
				args[2] == "ls-files" &&
				args[3] == "--others" &&
				args[4] == "--exclude-standard" {
				return "", nil
			}

			// commitAndPush: staged diff -> has changes
			if len(args) == 3 &&
				args[0] == "diff" &&
				args[1] == "--name-only" &&
				args[2] == "--cached" {
				return "locales/fr.json\n", nil
			}

			// commit ok
			if len(args) >= 1 && args[0] == "commit" {
				return "ok", nil
			}

			// validate generated branch name
			if len(args) == 3 &&
				args[0] == "check-ref-format" &&
				args[1] == "--branch" {
				if !strings.HasPrefix(args[2], "lok_master_123456_") {
					t.Fatalf("unexpected branch name for validation: %q", args[2])
				}
				return "", nil
			}

			t.Fatalf("unexpected capture: git %v", args)
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			if len(args) == 4 && args[0] == "config" && args[1] == "--global" {
				return nil
			}

			// checkout must be from origin/master
			if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" {
				if args[3] != "origin/master" {
					t.Fatalf("expected checkout from origin/master, got %q", args[3])
				}
				return nil
			}

			if len(args) == 3 && args[0] == "branch" && args[1] == "--unset-upstream" {
				return nil
			}

			// git add -A -- locales/fr.json
			if len(args) >= 4 && args[0] == "add" && args[1] == "-A" && args[2] == "--" {
				if args[3] != "locales/fr.json" {
					t.Fatalf("expected locales/fr.json to be staged, got args: %v", args)
				}
				return nil
			}

			if len(args) >= 1 && args[0] == "push" {
				return nil
			}

			t.Fatalf("unexpected run: git %v", args)
			return nil
		},
	}

	_, err := commitAndPushChanges(runner)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	foundMaster := false
	for _, refspec := range fetched {
		if strings.Contains(refspec, "+refs/heads/master:refs/remotes/origin/master") {
			foundMaster = true
			break
		}
	}
	if !foundMaster {
		t.Fatalf("expected fetch refspec for master, got %v", fetched)
	}
}

func TestCommitAndPush_SignedCommit_AddsDashS(t *testing.T) {
	var commitArgs []string

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			if len(args) == 3 && args[0] == "diff" && args[1] == "--name-only" && args[2] == "--cached" {
				return "locales/fr.json\n", nil
			}

			if len(args) >= 1 && args[0] == "commit" {
				commitArgs = append([]string{}, args...)
				return "ok", nil
			}

			t.Fatalf("unexpected capture: git %v", args)
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}
			if len(args) == 3 && args[0] == "push" && args[1] == "origin" && args[2] == "branch" {
				return nil
			}
			t.Fatalf("unexpected run: git %v", args)
			return nil
		},
	}

	err := commitAndPush("branch", runner, &Config{
		GitCommitMessage: "msg",
		GitSignCommits:   true,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	want := []string{"commit", "-S", "-m", "msg"}
	if !slices.Equal(commitArgs, want) {
		t.Fatalf("commit args mismatch: want %v got %v", want, commitArgs)
	}
}

func TestCommitAndPush_DiffCachedError(t *testing.T) {
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name == "git" && len(args) == 3 && args[0] == "diff" && args[1] == "--name-only" && args[2] == "--cached" {
				return "fatal: bad revision", fmt.Errorf("diff failed")
			}
			t.Fatalf("unexpected capture: %s %v", name, args)
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			t.Fatalf("run must not be called, got %s %v", name, args)
			return nil
		},
	}

	err := commitAndPush("branch", runner, &Config{GitCommitMessage: "msg"})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to inspect staged changes") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "fatal: bad revision") {
		t.Fatalf("expected output in error, got %v", err)
	}
}

func TestCommitAndPushChanges_StagedDiffEmptyAfterAdd_ReturnsErrNoChanges(t *testing.T) {
	t.Setenv("GITHUB_ACTOR", "test_actor")
	t.Setenv("GITHUB_SHA", "1234567890abcdef")
	t.Setenv("TEMP_BRANCH_PREFIX", "lok")
	t.Setenv("TRANSLATIONS_PATH", "locales")
	t.Setenv("BASE_LANG", "en")
	t.Setenv("FLAT_NAMING", "true")
	t.Setenv("ALWAYS_PULL_BASE", "true")
	t.Setenv("FORCE_PUSH", "false")
	t.Setenv("FILE_EXT", "json")
	t.Setenv("BASE_REF", "main")
	t.Setenv("GIT_COMMIT_MESSAGE", "msg")

	addCalled := false
	pushCalled := false

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			if len(args) == 3 && args[0] == "check-ref-format" && args[1] == "--branch" {
				return "", nil
			}

			if len(args) == 5 && args[0] == "ls-remote" && args[1] == "--exit-code" && args[2] == "--heads" && args[3] == "origin" {
				return "", &mockExitError{code: 2}
			}

			// fetch main now happens via Capture, not Run
			if len(args) == 5 &&
				args[0] == "fetch" &&
				args[1] == "--no-tags" &&
				args[2] == "--prune" &&
				args[3] == "origin" {
				return "", nil
			}

			if len(args) == 3 && args[0] == "rev-parse" && args[1] == "--verify" && args[2] == "HEAD" {
				return "ok", nil
			}

			if len(args) == 5 &&
				args[0] == "-c" &&
				args[1] == "core.quotepath=false" &&
				args[2] == "diff" &&
				args[3] == "--name-only" &&
				args[4] == "HEAD" {
				return "locales/fr.json\n", nil
			}

			if len(args) == 5 &&
				args[0] == "-c" &&
				args[1] == "core.quotepath=false" &&
				args[2] == "ls-files" &&
				args[3] == "--others" &&
				args[4] == "--exclude-standard" {
				return "", nil
			}

			if len(args) == 3 && args[0] == "diff" && args[1] == "--name-only" && args[2] == "--cached" {
				return "", nil
			}

			t.Fatalf("unexpected capture: git %v", args)
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			if len(args) == 4 && args[0] == "config" && args[1] == "--global" {
				return nil
			}

			if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[3] == "origin/main" {
				return nil
			}

			if len(args) == 3 && args[0] == "branch" && args[1] == "--unset-upstream" {
				return nil
			}

			if len(args) >= 4 && args[0] == "add" && args[1] == "-A" && args[2] == "--" {
				addCalled = true
				return nil
			}

			if len(args) >= 1 && args[0] == "push" {
				pushCalled = true
				return nil
			}

			t.Fatalf("unexpected run: git %v", args)
			return nil
		},
	}

	_, err := commitAndPushChanges(runner)
	if err != ErrNoChanges {
		t.Fatalf("want ErrNoChanges, got %v", err)
	}
	if !addCalled {
		t.Fatalf("expected git add to be called before staged diff check")
	}
	if pushCalled {
		t.Fatalf("push must not be called when staged diff is empty")
	}
}

func TestCommitAndPushChanges_AddFails(t *testing.T) {
	t.Setenv("GITHUB_ACTOR", "test_actor")
	t.Setenv("GITHUB_SHA", "1234567890abcdef")
	t.Setenv("TEMP_BRANCH_PREFIX", "lok")
	t.Setenv("TRANSLATIONS_PATH", "locales")
	t.Setenv("BASE_LANG", "en")
	t.Setenv("FLAT_NAMING", "true")
	t.Setenv("ALWAYS_PULL_BASE", "true")
	t.Setenv("FORCE_PUSH", "false")
	t.Setenv("FILE_EXT", "json")
	t.Setenv("BASE_REF", "main")

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			if len(args) == 3 && args[0] == "check-ref-format" && args[1] == "--branch" {
				return "", nil
			}

			if len(args) == 5 && args[0] == "ls-remote" && args[1] == "--exit-code" && args[2] == "--heads" && args[3] == "origin" {
				return "", &mockExitError{code: 2}
			}

			if len(args) == 3 && args[0] == "rev-parse" && args[1] == "--verify" && args[2] == "HEAD" {
				return "ok", nil
			}

			if len(args) == 5 &&
				args[0] == "-c" &&
				args[1] == "core.quotepath=false" &&
				args[2] == "diff" &&
				args[3] == "--name-only" &&
				args[4] == "HEAD" {
				return "locales/fr.json\n", nil
			}

			if len(args) == 5 && args[0] == "fetch" && args[1] == "--no-tags" && args[2] == "--prune" && args[3] == "origin" {
				return "", nil
			}

			if len(args) == 5 &&
				args[0] == "-c" &&
				args[1] == "core.quotepath=false" &&
				args[2] == "ls-files" &&
				args[3] == "--others" &&
				args[4] == "--exclude-standard" {
				return "", nil
			}

			t.Fatalf("unexpected capture: git %v", args)
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			if len(args) == 4 && args[0] == "config" && args[1] == "--global" {
				return nil
			}

			if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[3] == "origin/main" {
				return nil
			}

			if len(args) == 3 && args[0] == "branch" && args[1] == "--unset-upstream" {
				return nil
			}

			if len(args) >= 4 && args[0] == "add" && args[1] == "-A" && args[2] == "--" {
				return fmt.Errorf("add failed")
			}

			t.Fatalf("unexpected run: git %v", args)
			return nil
		},
	}

	_, err := commitAndPushChanges(runner)
	if err == nil || !strings.Contains(err.Error(), "failed to stage files") {
		t.Fatalf("expected add error, got %v", err)
	}
}
