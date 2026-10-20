package main

import (
	"errors"
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
			if slices.Equal(args, []string{
				"-c",
				"core.quotepath=false",
				"diff",
				"--name-only",
				"-z",
				"HEAD",
			}) {
				return "locales/en.json\x00", nil
			}

			if len(args) == 5 &&
				args[0] == "fetch" &&
				args[1] == "--no-tags" &&
				args[2] == "--prune" &&
				args[3] == "origin" {
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

			if len(args) == 4 && args[0] == "config" && args[1] == "--local" {
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
	t.Setenv("GIT_SIGN_COMMITS", "false")
	t.Setenv("FILE_EXT", "json")
	t.Setenv("BASE_REF", "main")
	t.Setenv("GIT_COMMIT_MESSAGE", "Translations update")
	t.Setenv("OVERRIDE_BRANCH_NAME", "")

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

			switch {
			// Validate generated branch name.
			case len(args) == 3 &&
				args[0] == "check-ref-format" &&
				args[1] == "--branch":

				branchName = args[2]

				if !strings.HasPrefix(branchName, "lok_main_123456_") {
					t.Fatalf("unexpected branch name: %q", branchName)
				}

				return "", nil

			// Generated branch does not exist remotely.
			case slices.Equal(args, []string{
				"ls-remote",
				"--exit-code",
				"--heads",
				"origin",
				"refs/heads/" + branchName,
			}):
				return "", &mockExitError{code: 2}

			// Fetch base branch.
			case slices.Equal(args, []string{
				"fetch",
				"--no-tags",
				"--prune",
				"origin",
				"+refs/heads/main:refs/remotes/origin/main",
			}):
				return "", nil

			// HEAD exists.
			case slices.Equal(args, []string{
				"rev-parse", "--verify", "HEAD",
			}):
				return "ok", nil

			// Changed tracked translation file.
			case slices.Equal(args, []string{
				"-c",
				"core.quotepath=false",
				"diff",
				"--name-only",
				"-z",
				"HEAD",
			}):
				return "locales/fr.json\x00", nil

			// No untracked files.
			case slices.Equal(args, []string{
				"-c",
				"core.quotepath=false",
				"ls-files",
				"--others",
				"--exclude-standard",
				"-z",
			}):
				return "", nil

			// Staged changes exist.
			case slices.Equal(args, []string{
				"diff", "--name-only", "--cached",
			}):
				diffCachedCalled = true
				return "locales/fr.json\n", nil

			// Commit succeeds.
			case slices.Equal(args, []string{
				"commit", "-m", "Translations update",
			}):
				commitCalled = true
				return "ok", nil
			}

			return "", fmt.Errorf("unexpected capture: git %v", args)
		},

		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			switch {
			case slices.Equal(args, []string{
				"config", "--local", "user.name", "test_actor",
			}):
				return nil

			case slices.Equal(args, []string{
				"config", "--local", "user.email",
				"test_actor@users.noreply.github.com",
			}):
				return nil

			// Checkout generated branch from origin/main.
			case slices.Equal(args, []string{
				"checkout", "-B", branchName, "origin/main",
			}):
				return nil

			// Remove upstream for newly generated branch.
			case slices.Equal(args, []string{
				"branch", "--unset-upstream", branchName,
			}):
				return nil

			// Stage changed translation file.
			case slices.Equal(args, []string{
				"add", "-A", "--", "locales/fr.json",
			}):
				addCalled = true
				return nil

			// Push without force.
			case slices.Equal(args, []string{
				"push", "origin", branchName,
			}):
				pushed = slices.Clone(args)
				return nil
			}

			return fmt.Errorf("unexpected run: git %v", args)
		},
	}

	gotBranch, err := commitAndPushChanges(runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotBranch != branchName {
		t.Fatalf("branch mismatch: got %q want %q", gotBranch, branchName)
	}

	if !diffCachedCalled || !commitCalled || !addCalled {
		t.Fatalf(
			"expected staged diff, commit and add; got diffCached=%t commit=%t add=%t",
			diffCachedCalled,
			commitCalled,
			addCalled,
		)
	}

	wantPush := []string{"push", "origin", branchName}
	if !slices.Equal(pushed, wantPush) {
		t.Fatalf("push args mismatch: got %v want %v", pushed, wantPush)
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
	t.Setenv("GIT_SIGN_COMMITS", "false")
	t.Setenv("FILE_EXT", "json")
	t.Setenv("BASE_REF", "main")

	addCalled := false
	commitCalled := false
	pushCalled := false

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			switch {
			case len(args) == 3 &&
				args[0] == "check-ref-format" &&
				args[1] == "--branch":
				return "", nil

			case len(args) == 5 &&
				args[0] == "ls-remote" &&
				args[1] == "--exit-code" &&
				args[2] == "--heads" &&
				args[3] == "origin" &&
				strings.HasPrefix(args[4], "refs/heads/"):
				return "", &mockExitError{code: 2}

			case slices.Equal(args, []string{
				"fetch",
				"--no-tags",
				"--prune",
				"origin",
				"+refs/heads/main:refs/remotes/origin/main",
			}):
				return "", nil

			case slices.Equal(args, []string{
				"rev-parse", "--verify", "HEAD",
			}):
				return "ok", nil

			case slices.Equal(args, []string{
				"-c",
				"core.quotepath=false",
				"diff",
				"--name-only",
				"-z",
				"HEAD",
			}):
				return "", nil

			case slices.Equal(args, []string{
				"-c",
				"core.quotepath=false",
				"ls-files",
				"--others",
				"--exclude-standard",
				"-z",
			}):
				return "", nil

			case len(args) >= 1 && args[0] == "diff":
				t.Fatal("staged diff must not be checked when no managed files changed")

			case len(args) >= 1 && args[0] == "commit":
				commitCalled = true
				t.Fatal("commit must not be called")
			}

			return "", fmt.Errorf("unexpected capture: git %v", args)
		},

		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			switch {
			case len(args) == 4 &&
				args[0] == "config" &&
				args[1] == "--local":
				return nil

			case len(args) == 4 &&
				args[0] == "checkout" &&
				args[1] == "-B" &&
				args[3] == "origin/main":
				return nil

			case len(args) == 3 &&
				args[0] == "branch" &&
				args[1] == "--unset-upstream":
				return nil

			case len(args) >= 1 && args[0] == "add":
				addCalled = true
				t.Fatal("git add must not be called")

			case len(args) >= 1 && args[0] == "push":
				pushCalled = true
				t.Fatal("git push must not be called")
			}

			return fmt.Errorf("unexpected run: git %v", args)
		},
	}

	_, err := commitAndPushChanges(runner)
	if !errors.Is(err, ErrNoChanges) {
		t.Fatalf("want ErrNoChanges, got %v", err)
	}

	if addCalled || commitCalled || pushCalled {
		t.Fatalf(
			"unexpected git operations: add=%t commit=%t push=%t",
			addCalled,
			commitCalled,
			pushCalled,
		)
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
	t.Setenv("GIT_SIGN_COMMITS", "false")
	t.Setenv("FILE_EXT", "json")
	t.Setenv("BASE_REF", "123/merge")
	t.Setenv("GIT_COMMIT_MESSAGE", "Translations update")

	var branchName string
	var fetched []string
	var pushed bool

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			switch {
			// Resolve synthetic base using remote HEAD.
			case slices.Equal(args, []string{
				"ls-remote", "--symref", "origin", "HEAD",
			}):
				return "ref: refs/heads/master\tHEAD\n012345\tHEAD\n", nil

			// Validate generated branch name.
			case len(args) == 3 &&
				args[0] == "check-ref-format" &&
				args[1] == "--branch":

				branchName = args[2]

				if !strings.HasPrefix(branchName, "lok_master_123456_") {
					t.Fatalf("unexpected branch name: %q", branchName)
				}

				return "", nil

			// Generated branch does not exist remotely.
			case slices.Equal(args, []string{
				"ls-remote",
				"--exit-code",
				"--heads",
				"origin",
				"refs/heads/" + branchName,
			}):
				return "", &mockExitError{code: 2}

			// Fetch the resolved base branch.
			case slices.Equal(args, []string{
				"fetch",
				"--no-tags",
				"--prune",
				"origin",
				"+refs/heads/master:refs/remotes/origin/master",
			}):
				fetched = append(fetched, args[4])
				return "", nil

			// HEAD exists.
			case slices.Equal(args, []string{
				"rev-parse", "--verify", "HEAD",
			}):
				return "ok", nil

			// Tracked translation file changed.
			case slices.Equal(args, []string{
				"-c",
				"core.quotepath=false",
				"diff",
				"--name-only",
				"-z",
				"HEAD",
			}):
				return "locales/fr.json\x00", nil

			// No untracked files.
			case slices.Equal(args, []string{
				"-c",
				"core.quotepath=false",
				"ls-files",
				"--others",
				"--exclude-standard",
				"-z",
			}):
				return "", nil

			// Staged changes exist.
			case slices.Equal(args, []string{
				"diff", "--name-only", "--cached",
			}):
				return "locales/fr.json\n", nil

			// Commit succeeds.
			case slices.Equal(args, []string{
				"commit", "-m", "Translations update",
			}):
				return "ok", nil
			}

			return "", fmt.Errorf("unexpected capture: git %v", args)
		},

		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			switch {
			case slices.Equal(args, []string{
				"config", "--local", "user.name", "test_actor",
			}):
				return nil

			case slices.Equal(args, []string{
				"config", "--local", "user.email",
				"test_actor@users.noreply.github.com",
			}):
				return nil

			// Checkout must use origin/master.
			case slices.Equal(args, []string{
				"checkout", "-B", branchName, "origin/master",
			}):
				return nil

			case slices.Equal(args, []string{
				"branch", "--unset-upstream", branchName,
			}):
				return nil

			// Stage the changed translation file.
			case slices.Equal(args, []string{
				"add", "-A", "--", "locales/fr.json",
			}):
				return nil

			// Push the generated branch.
			case slices.Equal(args, []string{
				"push", "origin", branchName,
			}):
				pushed = true
				return nil
			}

			return fmt.Errorf("unexpected run: git %v", args)
		},
	}

	gotBranch, err := commitAndPushChanges(runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotBranch != branchName {
		t.Fatalf("expected branch %q, got %q", branchName, gotBranch)
	}

	if !slices.Contains(
		fetched,
		"+refs/heads/master:refs/remotes/origin/master",
	) {
		t.Fatalf("expected fetch of master, got %v", fetched)
	}

	if !pushed {
		t.Fatal("expected generated branch to be pushed")
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

			if len(args) == 6 &&
				args[0] == "-c" &&
				args[1] == "core.quotepath=false" &&
				args[2] == "diff" &&
				args[3] == "--name-only" &&
				args[4] == "-z" &&
				args[5] == "HEAD" {
				return "locales/fr.json\x00", nil
			}

			if slices.Equal(args, []string{
				"-c",
				"core.quotepath=false",
				"ls-files",
				"--others",
				"--exclude-standard",
				"-z",
			}) {
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

			if len(args) == 4 && args[0] == "config" && args[1] == "--local" {
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

			if len(args) == 6 &&
				args[0] == "-c" &&
				args[1] == "core.quotepath=false" &&
				args[2] == "diff" &&
				args[3] == "--name-only" &&
				args[4] == "-z" &&
				args[5] == "HEAD" {
				return "locales/fr.json\x00", nil
			}

			if len(args) == 5 && args[0] == "fetch" && args[1] == "--no-tags" && args[2] == "--prune" && args[3] == "origin" {
				return "", nil
			}

			if slices.Equal(args, []string{
				"-c",
				"core.quotepath=false",
				"ls-files",
				"--others",
				"--exclude-standard",
				"-z",
			}) {
				return "", nil
			}

			t.Fatalf("unexpected capture: git %v", args)
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			if len(args) == 4 && args[0] == "config" && args[1] == "--local" {
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
