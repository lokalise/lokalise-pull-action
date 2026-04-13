package main

import (
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
