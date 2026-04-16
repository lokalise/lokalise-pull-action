package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

type MockCommandRunner struct {
	RunFunc     func(name string, args ...string) error
	CaptureFunc func(name string, args ...string) (string, error)
}

func (m *MockCommandRunner) Run(name string, args ...string) error {
	if m.RunFunc != nil {
		return m.RunFunc(name, args...)
	}
	return nil
}

func (m *MockCommandRunner) Capture(name string, args ...string) (string, error) {
	if m.CaptureFunc != nil {
		return m.CaptureFunc(name, args...)
	}
	return "", nil
}

type mockExitError struct{ code int }

func (e *mockExitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }
func (e *mockExitError) ExitCode() int { return e.code }

func TestRunWith_Success(t *testing.T) {
	t.Parallel()

	runner := &MockCommandRunner{}

	commitCalled := false
	writeCalls := 0

	var gotBranchName string
	outputs := map[string]string{}

	commit := func(gotRunner CommandRunner) (string, error) {
		commitCalled = true

		if gotRunner != runner {
			t.Fatalf("commit got unexpected runner")
		}

		gotBranchName = "lokalise/update-translations"
		return gotBranchName, nil
	}

	write := func(key, value string) bool {
		writeCalls++
		outputs[key] = value
		return true
	}

	err := runWith(commit, write, runner)
	if err != nil {
		t.Fatalf("runWith returned unexpected error: %v", err)
	}

	if !commitCalled {
		t.Fatal("expected commit to be called")
	}

	if gotBranchName != "lokalise/update-translations" {
		t.Fatalf("unexpected branch name: %q", gotBranchName)
	}

	if writeCalls != 2 {
		t.Fatalf("expected 2 write calls, got %d", writeCalls)
	}

	if outputs["branch_name"] != "lokalise/update-translations" {
		t.Fatalf("unexpected branch_name output: %q", outputs["branch_name"])
	}

	if outputs["commit_created"] != "true" {
		t.Fatalf("unexpected commit_created output: %q", outputs["commit_created"])
	}
}

func TestRunWith_Success_WhenNoChanges(t *testing.T) {
	t.Parallel()

	runner := &MockCommandRunner{}

	commitCalled := false
	writeCalled := false

	commit := func(gotRunner CommandRunner) (string, error) {
		commitCalled = true

		if gotRunner != runner {
			t.Fatalf("commit got unexpected runner")
		}

		return "", ErrNoChanges
	}

	write := func(key, value string) bool {
		writeCalled = true
		return true
	}

	err := runWith(commit, write, runner)
	if err != nil {
		t.Fatalf("runWith returned unexpected error: %v", err)
	}

	if !commitCalled {
		t.Fatal("expected commit to be called")
	}

	if writeCalled {
		t.Fatal("write should not be called when there are no changes")
	}
}

func TestRunWith_ReturnsError_WhenCommitFails(t *testing.T) {
	t.Parallel()

	runner := &MockCommandRunner{}

	commit := func(gotRunner CommandRunner) (string, error) {
		if gotRunner != runner {
			t.Fatalf("commit got unexpected runner")
		}

		return "", errors.New("push failed")
	}

	write := func(key, value string) bool {
		t.Fatal("write should not be called")
		return true
	}

	err := runWith(commit, write, runner)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "error committing and pushing changes: push failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunWith_ReturnsError_WhenWriteFails(t *testing.T) {
	t.Parallel()

	runner := &MockCommandRunner{}

	commit := func(gotRunner CommandRunner) (string, error) {
		if gotRunner != runner {
			t.Fatalf("commit got unexpected runner")
		}

		return "lokalise/update-translations", nil
	}

	write := func(key, value string) bool {
		return false
	}

	err := runWith(commit, write, runner)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "failed to write to GitHub output") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPerformCommit_Success(t *testing.T) {
	t.Parallel()

	runner := &MockCommandRunner{}

	commit := func(gotRunner CommandRunner) (string, error) {
		if gotRunner != runner {
			t.Fatalf("commit got unexpected runner")
		}

		return "feature/generated-translations", nil
	}

	branchName, err := performCommit(commit, runner)
	if err != nil {
		t.Fatalf("performCommit returned unexpected error: %v", err)
	}

	if branchName != "feature/generated-translations" {
		t.Fatalf("unexpected branch name: %q", branchName)
	}
}

func TestPerformCommit_NoChanges(t *testing.T) {
	t.Parallel()

	runner := &MockCommandRunner{}

	commit := func(gotRunner CommandRunner) (string, error) {
		if gotRunner != runner {
			t.Fatalf("commit got unexpected runner")
		}

		return "", ErrNoChanges
	}

	branchName, err := performCommit(commit, runner)
	if err != nil {
		t.Fatalf("performCommit returned unexpected error: %v", err)
	}

	if branchName != "" {
		t.Fatalf("expected empty branch name, got %q", branchName)
	}
}

func TestPerformCommit_ReturnsWrappedError(t *testing.T) {
	t.Parallel()

	runner := &MockCommandRunner{}

	commit := func(gotRunner CommandRunner) (string, error) {
		if gotRunner != runner {
			t.Fatalf("commit got unexpected runner")
		}

		return "", errors.New("git status failed")
	}

	branchName, err := performCommit(commit, runner)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if branchName != "" {
		t.Fatalf("expected empty branch name, got %q", branchName)
	}

	if !strings.Contains(err.Error(), "error committing and pushing changes: git status failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteOutputs_Success(t *testing.T) {
	t.Parallel()

	outputs := map[string]string{}
	writeCalls := 0

	write := func(key, value string) bool {
		writeCalls++
		outputs[key] = value
		return true
	}

	err := writeOutputs("feature/generated-translations", write)
	if err != nil {
		t.Fatalf("writeOutputs returned unexpected error: %v", err)
	}

	if writeCalls != 2 {
		t.Fatalf("expected 2 write calls, got %d", writeCalls)
	}

	if outputs["branch_name"] != "feature/generated-translations" {
		t.Fatalf("unexpected branch_name output: %q", outputs["branch_name"])
	}

	if outputs["commit_created"] != "true" {
		t.Fatalf("unexpected commit_created output: %q", outputs["commit_created"])
	}
}

func TestWriteOutputs_Success_WithEmptyBranchName(t *testing.T) {
	t.Parallel()

	writeCalled := false

	write := func(key, value string) bool {
		writeCalled = true
		return true
	}

	err := writeOutputs("", write)
	if err != nil {
		t.Fatalf("writeOutputs returned unexpected error: %v", err)
	}

	if writeCalled {
		t.Fatal("write should not be called for empty branch name")
	}
}

func TestWriteOutputs_ReturnsError_WhenWriteFails(t *testing.T) {
	t.Parallel()

	write := func(key, value string) bool {
		return false
	}

	err := writeOutputs("feature/generated-translations", write)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "failed to write to GitHub output") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSanitizeString(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		maxLength int
		expected  string
	}{
		{
			name:      "Valid characters",
			input:     "Valid_Characters-123",
			maxLength: 50,
			expected:  "Valid_Characters-123",
		},
		{
			name:      "Invalid characters",
			input:     "Invalid!@#Characters$$",
			maxLength: 50,
			expected:  "InvalidCharacters",
		},
		{
			name:      "Mixed characters",
			input:     "Valid_123!@#Invalid-456$$",
			maxLength: 50,
			expected:  "Valid_123Invalid-456",
		},
		{
			name:      "Exceeds maxLength",
			input:     "Valid_123_Invalid_456_Valid_789_Extra_Characters",
			maxLength: 20,
			expected:  "Valid_123_Invalid_45",
		},
		{
			name:      "Empty input",
			input:     "",
			maxLength: 10,
			expected:  "",
		},
		{
			name:      "Only invalid characters",
			input:     "!@#$%^&*()",
			maxLength: 10,
			expected:  "",
		},
		{
			name:      "Input equals maxLength",
			input:     "ValidInput123",
			maxLength: 13,
			expected:  "ValidInput123",
		},
		{
			name:      "Input shorter than maxLength",
			input:     "Short",
			maxLength: 10,
			expected:  "Short",
		},
		{
			name:      "Allow branch folders",
			input:     "feature/valid-branch-name",
			maxLength: 50,
			expected:  "feature/valid-branch-name",
		},
		{
			name:      "Allow dots underscores slashes and hyphens together",
			input:     "feature/foo.bar_baz-123",
			maxLength: 50,
			expected:  "feature/foo.bar_baz-123",
		},
		{
			name:      "Keeps repeated dots",
			input:     "...",
			maxLength: 10,
			expected:  "...",
		},
		{
			name:      "Keeps double slash",
			input:     "foo//bar",
			maxLength: 20,
			expected:  "foo//bar",
		},
		{
			name:      "Keeps leading slash",
			input:     "/leading/slash",
			maxLength: 20,
			expected:  "/leading/slash",
		},
		{
			name:      "Zero maxLength returns empty",
			input:     "feature/branch",
			maxLength: 0,
			expected:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := sanitizeString(tt.input, tt.maxLength)
			if result != tt.expected {
				t.Errorf("sanitizeString(%q, %d) = %q; want %q", tt.input, tt.maxLength, result, tt.expected)
			}
		})
	}
}

func TestSplitNonEmptyLines(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty input",
			input:    "",
			expected: nil,
		},
		{
			name:     "single line",
			input:    "one",
			expected: []string{"one"},
		},
		{
			name:     "multiple lines",
			input:    "one\ntwo\nthree",
			expected: []string{"one", "two", "three"},
		},
		{
			name:     "skips empty lines",
			input:    "\none\n\n\ntwo\n",
			expected: []string{"one", "two"},
		},
		{
			name:     "trims whitespace around lines",
			input:    "  one  \n\t two\t\n   \n three ",
			expected: []string{"one", "two", "three"},
		},
		{
			name:     "trailing newline does not create empty item",
			input:    "one\ntwo\n",
			expected: []string{"one", "two"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitNonEmptyLines(tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("splitNonEmptyLines(%q) len = %d, want %d (%v)", tt.input, len(got), len(tt.expected), got)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("splitNonEmptyLines(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestIsExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code int
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			code: 1,
			want: false,
		},
		{
			name: "plain error without exit code",
			err:  fmt.Errorf("boom"),
			code: 1,
			want: false,
		},
		{
			name: "matching custom exit code",
			err:  &mockExitError{code: 1},
			code: 1,
			want: true,
		},
		{
			name: "non matching custom exit code",
			err:  &mockExitError{code: 1},
			code: 2,
			want: false,
		},
		{
			name: "wrapped custom exit code still matches",
			err:  fmt.Errorf("wrapped: %w", &mockExitError{code: 42}),
			code: 42,
			want: true,
		},
		{
			name: "wrapped custom exit code non matching",
			err:  fmt.Errorf("wrapped: %w", &mockExitError{code: 42}),
			code: 1,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isExitCode(tt.err, tt.code)
			if got != tt.want {
				t.Fatalf("isExitCode(%v, %d) = %v, want %v", tt.err, tt.code, got, tt.want)
			}
		})
	}
}

// containsSubstring checks if a string contains a substring
func containsSubstring(s, substr string) bool {
	return strings.Contains(s, substr)
}
