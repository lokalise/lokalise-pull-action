package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectChangedFiles(t *testing.T) {
	paths := []string{"path/to/translations"}
	fileExts := []string{"json"}
	flat := true

	mockRunner := newMockCommandRunner(
		map[string]string{
			makeKey([]string{"rev-parse", "--verify", "HEAD"}): "ok",
			makeKey([]string{"-c", "core.quotepath=false", "diff", "--name-only", "HEAD"}): joinLines(
				filepath.ToSlash("path/to/translations/file1.json"),
				filepath.ToSlash("path/to/translations/file2.json"),
			),
			makeKey([]string{"-c", "core.quotepath=false", "ls-files", "--others", "--exclude-standard"}): joinLines(
				filepath.ToSlash("path/to/translations/file3.json"),
			),
		},
		nil,
	)

	config := &Config{
		Paths:          paths,
		FileExt:        fileExts,
		FlatNaming:     flat,
		AlwaysPullBase: false,
		BaseLang:       "en",
	}

	changed, err := detectChangedFiles(config, mockRunner)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !changed {
		t.Fatalf("Expected changes, but got none")
	}
}

func TestDetectChangedFiles_NoChanges(t *testing.T) {
	paths := []string{"path/to/translations"}
	fileExts := []string{"json"}
	flat := true

	mockRunner := newMockCommandRunner(
		map[string]string{
			makeKey([]string{"rev-parse", "--verify", "HEAD"}):                                            "ok",
			makeKey([]string{"-c", "core.quotepath=false", "diff", "--name-only", "HEAD"}):                "",
			makeKey([]string{"-c", "core.quotepath=false", "ls-files", "--others", "--exclude-standard"}): "",
		},
		nil,
	)

	config := &Config{
		Paths:          paths,
		FileExt:        fileExts,
		FlatNaming:     flat,
		AlwaysPullBase: true,
		BaseLang:       "en",
	}

	changed, err := detectChangedFiles(config, mockRunner)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if changed {
		t.Fatalf("Expected no changes, but got changes")
	}
}

func TestDetectChangedFiles_AllChangesExcluded_Flat_PerExt(t *testing.T) {
	paths := []string{"ios/Loc"}
	fileExts := []string{"strings", "stringsdict"}
	flat := true

	mockRunner := newMockCommandRunner(
		map[string]string{
			makeKey([]string{"rev-parse", "--verify", "HEAD"}): "ok",
			makeKey([]string{"-c", "core.quotepath=false", "diff", "--name-only", "HEAD"}): joinLines(
				filepath.ToSlash("ios/Loc/en.strings"),
				filepath.ToSlash("ios/Loc/en.stringsdict"),
			),
			makeKey([]string{"-c", "core.quotepath=false", "ls-files", "--others", "--exclude-standard"}): "",
		},
		nil,
	)

	config := &Config{
		Paths:          paths,
		FileExt:        fileExts,
		FlatNaming:     flat,
		AlwaysPullBase: false,
		BaseLang:       "en",
	}

	changed, err := detectChangedFiles(config, mockRunner)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if changed {
		t.Fatalf("Expected no changes (all changes excluded), but got changes")
	}
}

func TestDetectChangedFiles_Nested_BaseDirExcluded(t *testing.T) {
	paths := []string{"ios/App"}
	fileExts := []string{"strings", "stringsdict"}
	flat := false

	mockRunner := newMockCommandRunner(
		map[string]string{
			makeKey([]string{"rev-parse", "--verify", "HEAD"}): "ok",
			makeKey([]string{"-c", "core.quotepath=false", "diff", "--name-only", "HEAD"}): joinLines(
				filepath.ToSlash("ios/App/en/Localizable.strings"),
				filepath.ToSlash("ios/App/en/Plurals.stringsdict"),
				filepath.ToSlash("ios/App/de/Localizable.strings"),
			),
			makeKey([]string{"-c", "core.quotepath=false", "ls-files", "--others", "--exclude-standard"}): joinLines(
				filepath.ToSlash("ios/App/en/Untracked.strings"),
			),
		},
		nil,
	)

	config := &Config{
		Paths:          paths,
		FileExt:        fileExts,
		FlatNaming:     flat,
		AlwaysPullBase: false,
		BaseLang:       "en",
	}

	changed, err := detectChangedFiles(config, mockRunner)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !changed {
		t.Fatalf("Expected changes (non-base dir files remain), but got none")
	}
}

func TestDetectChangedFiles_GitDiffError(t *testing.T) {
	paths := []string{"path/to/translations"}
	fileExts := []string{"json"}
	flat := true

	mockRunner := newMockCommandRunner(
		map[string]string{
			makeKey([]string{"rev-parse", "--verify", "HEAD"}):                             "ok",
			makeKey([]string{"-c", "core.quotepath=false", "diff", "--name-only", "HEAD"}): "git diff error output",
		},
		map[string]error{
			makeKey([]string{"-c", "core.quotepath=false", "diff", "--name-only", "HEAD"}): fmt.Errorf("git diff error"),
		},
	)

	config := &Config{
		Paths:          paths,
		FileExt:        fileExts,
		FlatNaming:     flat,
		AlwaysPullBase: true,
		BaseLang:       "en",
	}

	_, err := detectChangedFiles(config, mockRunner)
	if err == nil || !strings.Contains(err.Error(), "git diff error") {
		t.Fatalf("Expected git diff error, but got %v", err)
	}
}

func TestDetectChangedFiles_GitLsFilesError(t *testing.T) {
	paths := []string{"path/to/translations"}
	fileExts := []string{"json"}
	flat := true

	mockRunner := newMockCommandRunner(
		map[string]string{
			makeKey([]string{"rev-parse", "--verify", "HEAD"}):                                            "ok",
			makeKey([]string{"-c", "core.quotepath=false", "diff", "--name-only", "HEAD"}):                "",
			makeKey([]string{"-c", "core.quotepath=false", "ls-files", "--others", "--exclude-standard"}): "git ls-files error output",
		},
		map[string]error{
			makeKey([]string{"-c", "core.quotepath=false", "ls-files", "--others", "--exclude-standard"}): fmt.Errorf("git ls-files error"),
		},
	)

	config := &Config{
		Paths:          paths,
		FileExt:        fileExts,
		FlatNaming:     flat,
		AlwaysPullBase: true,
		BaseLang:       "en",
	}

	_, err := detectChangedFiles(config, mockRunner)
	if err == nil || !strings.Contains(err.Error(), "git ls-files error") {
		t.Fatalf("Expected git ls-files error, but got %v", err)
	}
}

func TestDetectChangedFiles_DeletedManagedFileStillCounts(t *testing.T) {
	mockRunner := newMockCommandRunner(
		map[string]string{
			makeKey([]string{"rev-parse", "--verify", "HEAD"}): "ok",
			makeKey([]string{"-c", "core.quotepath=false", "diff", "--name-only", "HEAD"}): joinLines(
				filepath.ToSlash("path/to/translations/fr/file.json"),
			),
			makeKey([]string{"-c", "core.quotepath=false", "ls-files", "--others", "--exclude-standard"}): "",
		},
		nil,
	)

	config := &Config{
		Paths:          []string{"path/to/translations"},
		FileExt:        []string{"json"},
		FlatNaming:     false,
		AlwaysPullBase: false,
		BaseLang:       "en",
	}

	changed, err := detectChangedFiles(config, mockRunner)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !changed {
		t.Fatalf("Expected deleted managed file to count as change")
	}
}

func TestDetectChangedFiles_NoHeadFallback(t *testing.T) {
	mockRunner := newMockCommandRunner(
		map[string]string{
			makeKey([]string{"rev-parse", "--verify", "HEAD"}): "",
			makeKey([]string{"-c", "core.quotepath=false", "diff", "--name-only", "--cached"}): joinLines(
				filepath.ToSlash("path/to/translations/fr.json"),
			),
			makeKey([]string{"-c", "core.quotepath=false", "diff", "--name-only"}): joinLines(
				filepath.ToSlash("other/ignore.json"),
			),
			makeKey([]string{"-c", "core.quotepath=false", "ls-files", "--others", "--exclude-standard"}): "",
		},
		map[string]error{
			makeKey([]string{"rev-parse", "--verify", "HEAD"}): fmt.Errorf("no HEAD"),
		},
	)

	config := &Config{
		Paths:          []string{"path/to/translations"},
		FileExt:        []string{"json"},
		FlatNaming:     true,
		AlwaysPullBase: false,
		BaseLang:       "en",
	}

	changed, err := detectChangedFiles(config, mockRunner)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !changed {
		t.Fatalf("Expected changes from no-HEAD fallback path, but got none")
	}
}

func TestDetectChangedFiles_AlwaysPullBaseIncludesBaseFile_Flat(t *testing.T) {
	mockRunner := newMockCommandRunner(
		map[string]string{
			makeKey([]string{"rev-parse", "--verify", "HEAD"}): "ok",
			makeKey([]string{"-c", "core.quotepath=false", "diff", "--name-only", "HEAD"}): joinLines(
				filepath.ToSlash("locales/en.json"),
			),
			makeKey([]string{"-c", "core.quotepath=false", "ls-files", "--others", "--exclude-standard"}): "",
		},
		nil,
	)

	config := &Config{
		Paths:          []string{"locales"},
		FileExt:        []string{"json"},
		FlatNaming:     true,
		AlwaysPullBase: true,
		BaseLang:       "en",
	}

	changed, err := detectChangedFiles(config, mockRunner)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !changed {
		t.Fatalf("Expected base-language file to count when AlwaysPullBase=true")
	}
}

func TestDetectChangedFiles_Nested_BaseOnlyChangesExcluded(t *testing.T) {
	mockRunner := newMockCommandRunner(
		map[string]string{
			makeKey([]string{"rev-parse", "--verify", "HEAD"}): "ok",
			makeKey([]string{"-c", "core.quotepath=false", "diff", "--name-only", "HEAD"}): joinLines(
				filepath.ToSlash("ios/App/en/Localizable.strings"),
				filepath.ToSlash("ios/App/en/Plurals.stringsdict"),
			),
			makeKey([]string{"-c", "core.quotepath=false", "ls-files", "--others", "--exclude-standard"}): joinLines(
				filepath.ToSlash("ios/App/en/Untracked.strings"),
			),
		},
		nil,
	)

	config := &Config{
		Paths:          []string{"ios/App"},
		FileExt:        []string{"strings", "stringsdict"},
		FlatNaming:     false,
		AlwaysPullBase: false,
		BaseLang:       "en",
	}

	changed, err := detectChangedFiles(config, mockRunner)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if changed {
		t.Fatalf("Expected only base-language nested changes to be excluded")
	}
}

func TestDetectChangedFiles_OnlyNonManagedChanges(t *testing.T) {
	mockRunner := newMockCommandRunner(
		map[string]string{
			makeKey([]string{"rev-parse", "--verify", "HEAD"}): "ok",
			makeKey([]string{"-c", "core.quotepath=false", "diff", "--name-only", "HEAD"}): joinLines(
				filepath.ToSlash("README.md"),
				filepath.ToSlash("scripts/sync.sh"),
				filepath.ToSlash("other/path/file.json"),
			),
			makeKey([]string{"-c", "core.quotepath=false", "ls-files", "--others", "--exclude-standard"}): joinLines(
				filepath.ToSlash("tmp/generated.txt"),
			),
		},
		nil,
	)

	config := &Config{
		Paths:          []string{"path/to/translations"},
		FileExt:        []string{"json"},
		FlatNaming:     true,
		AlwaysPullBase: true,
		BaseLang:       "en",
	}

	changed, err := detectChangedFiles(config, mockRunner)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if changed {
		t.Fatalf("Expected non-managed changes to be ignored")
	}
}

func TestDetectChangedFiles_UntrackedManagedOnly(t *testing.T) {
	mockRunner := newMockCommandRunner(
		map[string]string{
			makeKey([]string{"rev-parse", "--verify", "HEAD"}):                             "ok",
			makeKey([]string{"-c", "core.quotepath=false", "diff", "--name-only", "HEAD"}): "",
			makeKey([]string{"-c", "core.quotepath=false", "ls-files", "--others", "--exclude-standard"}): joinLines(
				filepath.ToSlash("path/to/translations/fr.json"),
			),
		},
		nil,
	)

	config := &Config{
		Paths:          []string{"path/to/translations"},
		FileExt:        []string{"json"},
		FlatNaming:     true,
		AlwaysPullBase: false,
		BaseLang:       "en",
	}

	changed, err := detectChangedFiles(config, mockRunner)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !changed {
		t.Fatalf("Expected untracked managed file to count as change")
	}
}

func TestDetectChangedFiles_NoHeadCachedError(t *testing.T) {
	mockRunner := newMockCommandRunner(
		map[string]string{
			makeKey([]string{"rev-parse", "--verify", "HEAD"}):                                 "",
			makeKey([]string{"-c", "core.quotepath=false", "diff", "--name-only", "--cached"}): "git diff --cached error output",
		},
		map[string]error{
			makeKey([]string{"rev-parse", "--verify", "HEAD"}):                                 fmt.Errorf("no HEAD"),
			makeKey([]string{"-c", "core.quotepath=false", "diff", "--name-only", "--cached"}): fmt.Errorf("git diff --cached error"),
		},
	)

	config := &Config{
		Paths:          []string{"path/to/translations"},
		FileExt:        []string{"json"},
		FlatNaming:     true,
		AlwaysPullBase: false,
		BaseLang:       "en",
	}

	_, err := detectChangedFiles(config, mockRunner)
	if err == nil || !strings.Contains(err.Error(), "git diff --cached") {
		t.Fatalf("Expected no-HEAD cached diff error, but got %v", err)
	}
}

func TestDetectChangedFiles_MultipleRoots_SecondRootMatches(t *testing.T) {
	mockRunner := newMockCommandRunner(
		map[string]string{
			makeKey([]string{"rev-parse", "--verify", "HEAD"}): "ok",
			makeKey([]string{"-c", "core.quotepath=false", "diff", "--name-only", "HEAD"}): joinLines(
				filepath.ToSlash("packages/app/locales/fr.json"),
			),
			makeKey([]string{"-c", "core.quotepath=false", "ls-files", "--others", "--exclude-standard"}): "",
		},
		nil,
	)

	config := &Config{
		Paths: []string{
			"packages/pkg/locales",
			"packages/app/locales",
		},
		FileExt:        []string{"json"},
		FlatNaming:     true,
		AlwaysPullBase: false,
		BaseLang:       "en",
	}

	changed, err := detectChangedFiles(config, mockRunner)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !changed {
		t.Fatalf("Expected managed file under second translation root to count as change")
	}
}

func TestDetectChangedFiles_NoHeadWorktreeError(t *testing.T) {
	mockRunner := newMockCommandRunner(
		map[string]string{
			makeKey([]string{"rev-parse", "--verify", "HEAD"}):                                 "",
			makeKey([]string{"-c", "core.quotepath=false", "diff", "--name-only", "--cached"}): "",
			makeKey([]string{"-c", "core.quotepath=false", "diff", "--name-only"}):             "git diff worktree error output",
		},
		map[string]error{
			makeKey([]string{"rev-parse", "--verify", "HEAD"}):                     fmt.Errorf("no HEAD"),
			makeKey([]string{"-c", "core.quotepath=false", "diff", "--name-only"}): fmt.Errorf("git diff worktree error"),
		},
	)

	config := &Config{
		Paths:          []string{"path/to/translations"},
		FileExt:        []string{"json"},
		FlatNaming:     true,
		AlwaysPullBase: false,
		BaseLang:       "en",
	}

	_, err := detectChangedFiles(config, mockRunner)
	if err == nil || !strings.Contains(err.Error(), "git diff (worktree)") {
		t.Fatalf("Expected no-HEAD worktree diff error, but got %v", err)
	}
}
