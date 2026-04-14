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

func TestGenerateBranchName(t *testing.T) {
	tests := []struct {
		name            string
		config          *Config
		expectedError   bool
		expectedStart   string // prefix for generated names; exact for overrides
		expectValidator bool   // whether we expect git check-ref-format to be called
		expectExact     bool   // whether to check exact match instead of prefix
	}{
		{
			name: "Valid inputs",
			config: &Config{
				GitHubSHA:        "1234567890abcdef",
				BaseRef:          "feature_branch",
				TempBranchPrefix: "temp",
			},
			expectedError:   false,
			expectedStart:   "temp_feature_branch_123456_",
			expectValidator: true,
			expectExact:     false,
		},
		{
			name: "Override branch name is empty after trimming",
			config: &Config{
				GitHubSHA:          "1234567890abcdef",
				BaseRef:            "main",
				TempBranchPrefix:   "temp",
				OverrideBranchName: "   \t   ",
			},
			expectedError:   true,
			expectValidator: false,
		},
		{
			name: "Blank temp branch prefix falls back to lok",
			config: &Config{
				GitHubSHA:        "1234567890abcdef",
				BaseRef:          "main",
				TempBranchPrefix: "   ",
			},
			expectedError:   false,
			expectedStart:   "lok_main_123456_",
			expectValidator: true,
			expectExact:     false,
		},
		{
			name: "Sanitized empty base ref falls back to base",
			config: &Config{
				GitHubSHA:        "abcdef123456",
				BaseRef:          "!@#",
				TempBranchPrefix: "temp",
			},
			expectedError:   false,
			expectedStart:   "temp_base_abcdef_",
			expectValidator: true,
			expectExact:     false,
		},
		{
			name: "Valid inputs with branch override (simple)",
			config: &Config{
				GitHubSHA:          "1234567890abcdef",
				BaseRef:            "feature_branch",
				TempBranchPrefix:   "temp",
				OverrideBranchName: "custom_branch",
			},
			expectedError:   false,
			expectedStart:   "custom_branch",
			expectValidator: true,
			expectExact:     true, // override should be returned as-is
		},
		{
			name: "Valid inputs with branch override (keeps + and other valid chars)",
			config: &Config{
				GitHubSHA:          "1234567890abcdef",
				BaseRef:            "feature_branch",
				TempBranchPrefix:   "temp",
				OverrideBranchName: "feature/foo+bar",
			},
			expectedError:   false,
			expectedStart:   "feature/foo+bar",
			expectValidator: true,
			expectExact:     true,
		},
		{
			name: "Invalid override branch (space) should fail validation",
			config: &Config{
				GitHubSHA:          "1234567890abcdef",
				BaseRef:            "feature_branch",
				TempBranchPrefix:   "temp",
				OverrideBranchName: "bad branch",
			},
			expectedError:   true,
			expectValidator: true,
		},
		{
			name: "GITHUB_SHA too short",
			config: &Config{
				GitHubSHA:        "123",
				BaseRef:          "main",
				TempBranchPrefix: "temp",
			},
			expectedError:   true,
			expectValidator: false, // should error before validation call
		},
		{
			name: "BASE_REF with invalid characters (sanitized in generated name)",
			config: &Config{
				GitHubSHA:        "abcdef123456",
				BaseRef:          "feature/branch!@#",
				TempBranchPrefix: "temp",
			},
			expectedError:   false,
			expectedStart:   "temp_feature/branch_abcdef_",
			expectValidator: true,
			expectExact:     false,
		},
		{
			name: "BASE_REF exceeding 50 characters",
			config: &Config{
				GitHubSHA:        "abcdef123456",
				BaseRef:          strings.Repeat("a", 60),
				TempBranchPrefix: "temp",
			},
			expectedError:   false,
			expectedStart:   "temp_" + strings.Repeat("a", 50) + "_abcdef_",
			expectValidator: true,
			expectExact:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var validated []string

			// simple validator stub: fail only on the specific invalid override we test
			runner := &MockCommandRunner{
				CaptureFunc: func(name string, args ...string) (string, error) {
					if name != "git" {
						return "", errors.New("unexpected command: " + name)
					}
					if len(args) != 3 || args[0] != "check-ref-format" || args[1] != "--branch" {
						return "", errors.New("unexpected args: " + strings.Join(args, " "))
					}
					branch := args[2]
					validated = append(validated, branch)

					// emulate git validation failure for a known-bad branch
					if branch == "bad branch" {
						return "fatal: invalid branch name", errors.New("exit status 1")
					}
					return "", nil
				},
			}

			branchName, err := generateBranchName(tt.config, runner)

			if tt.expectedError {
				if err == nil {
					t.Fatalf("expected error but got nil (branchName=%q)", branchName)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				if tt.expectExact {
					if branchName != tt.expectedStart {
						t.Fatalf("expected branch name %q, got %q", tt.expectedStart, branchName)
					}
				} else {
					if !strings.HasPrefix(branchName, tt.expectedStart) {
						t.Fatalf("expected branch name to start with %q, got %q", tt.expectedStart, branchName)
					}
				}
			}

			// Assert validator call behavior
			if tt.expectValidator {
				if len(validated) == 0 {
					t.Fatalf("expected git check-ref-format to be called, but it wasn't")
				}
				// For successful cases, the validated name should match returned branchName
				if !tt.expectedError {
					last := validated[len(validated)-1]
					if last != branchName {
						t.Fatalf("expected validated branch %q to equal returned branch %q", last, branchName)
					}
				}
			} else {
				if len(validated) != 0 {
					t.Fatalf("did not expect validation call, but got %d call(s): %v", len(validated), validated)
				}
			}
		})
	}
}

func TestSetGitUser(t *testing.T) {
	runner := &MockCommandRunner{
		RunFunc: func(name string, args ...string) error {
			if name == "git" && args[0] == "config" && args[1] == "--global" {
				if args[2] == "user.name" && args[3] == "test_actor" {
					return nil
				}
				if args[2] == "user.email" && args[3] == "test_actor@users.noreply.github.com" {
					return nil
				}
			}
			return fmt.Errorf("unexpected command: %s %v", name, args)
		},
	}

	config := &Config{
		GitHubActor: "test_actor",
	}

	err := setGitUser(config, runner)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestSetGitUser_WithCustomValues(t *testing.T) {
	runner := &MockCommandRunner{
		RunFunc: func(name string, args ...string) error {
			if name == "git" && args[0] == "config" && args[1] == "--global" {
				if args[2] == "user.name" && args[3] == "custom_user" {
					return nil
				}
				if args[2] == "user.email" && args[3] == "custom_email@example.com" {
					return nil
				}
			}
			return fmt.Errorf("unexpected command: %s %v", name, args)
		},
	}

	config := &Config{
		GitHubActor:  "ignored_actor",
		GitUserName:  "custom_user",
		GitUserEmail: "custom_email@example.com",
	}

	err := setGitUser(config, runner)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
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

func TestResolveGitIdentity(t *testing.T) {
	tests := []struct {
		name          string
		config        *Config
		wantUserName  string
		wantUserEmail string
	}{
		{
			name: "default actor and noreply email",
			config: &Config{
				GitHubActor: "test_actor",
			},
			wantUserName:  "test_actor",
			wantUserEmail: "test_actor@users.noreply.github.com",
		},
		{
			name: "custom name and custom email",
			config: &Config{
				GitHubActor:  "ignored_actor",
				GitUserName:  "custom_user",
				GitUserEmail: "custom_email@example.com",
			},
			wantUserName:  "custom_user",
			wantUserEmail: "custom_email@example.com",
		},
		{
			name: "custom name only gets custom noreply email",
			config: &Config{
				GitHubActor: "ignored_actor",
				GitUserName: "custom_user",
			},
			wantUserName:  "custom_user",
			wantUserEmail: "custom_user@users.noreply.github.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotUser, gotEmail := resolveGitIdentity(tt.config)

			if gotUser != tt.wantUserName {
				t.Fatalf("username mismatch: got %q want %q", gotUser, tt.wantUserName)
			}
			if gotEmail != tt.wantUserEmail {
				t.Fatalf("email mismatch: got %q want %q", gotEmail, tt.wantUserEmail)
			}
		})
	}
}
