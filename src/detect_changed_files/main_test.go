package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

type MockCommandRunner struct {
	Output map[string]string
	Err    map[string]error
}

func (m MockCommandRunner) Capture(name string, args ...string) (string, error) {
	key := filepath.ToSlash(name + " " + strings.Join(args, " "))

	if err, ok := m.Err[key]; ok {
		return m.Output[key], err
	}
	if output, ok := m.Output[key]; ok {
		return output, nil
	}
	return "", fmt.Errorf("command %q not mocked", key)
}

func makeKey(args []string) string {
	return filepath.ToSlash("git " + strings.Join(args, " "))
}

func joinLines(lines ...string) string {
	return strings.Join(lines, "\n")
}

func newMockCommandRunner(output map[string]string, err map[string]error) MockCommandRunner {
	return MockCommandRunner{
		Output: output,
		Err:    err,
	}
}

func TestRunWith_Success_WhenChangesDetected(t *testing.T) {
	t.Parallel()

	cfg := &Config{}

	prepareCalled := false
	detectCalled := false
	writeCalled := false

	var gotKey string
	var gotValue string

	prepare := func() (*Config, error) {
		prepareCalled = true
		return cfg, nil
	}

	detect := func(gotCfg *Config, runner CommandRunner) (bool, error) {
		detectCalled = true

		if gotCfg != cfg {
			t.Fatalf("detect got unexpected config pointer")
		}

		_, ok := runner.(MockCommandRunner)
		if !ok {
			t.Fatalf("detect got unexpected runner type: %T", runner)
		}

		return true, nil
	}

	write := func(key, value string) bool {
		writeCalled = true
		gotKey = key
		gotValue = value
		return true
	}

	runner := newMockCommandRunner(nil, nil)

	err := runWith(prepare, detect, write, runner)
	if err != nil {
		t.Fatalf("runWith returned unexpected error: %v", err)
	}

	if !prepareCalled {
		t.Fatal("expected prepare to be called")
	}
	if !detectCalled {
		t.Fatal("expected detect to be called")
	}
	if !writeCalled {
		t.Fatal("expected write to be called")
	}
	if gotKey != "has_changes" {
		t.Fatalf("unexpected output key: %q", gotKey)
	}
	if gotValue != "true" {
		t.Fatalf("unexpected output value: %q", gotValue)
	}
}

func TestRunWith_Success_WhenNoChangesDetected(t *testing.T) {
	t.Parallel()

	cfg := &Config{}

	var gotKey string
	var gotValue string

	prepare := func() (*Config, error) {
		return cfg, nil
	}

	detect := func(gotCfg *Config, runner CommandRunner) (bool, error) {
		if gotCfg != cfg {
			t.Fatalf("detect got unexpected config pointer")
		}
		return false, nil
	}

	write := func(key, value string) bool {
		gotKey = key
		gotValue = value
		return true
	}

	runner := newMockCommandRunner(nil, nil)

	err := runWith(prepare, detect, write, runner)
	if err != nil {
		t.Fatalf("runWith returned unexpected error: %v", err)
	}

	if gotKey != "has_changes" {
		t.Fatalf("unexpected output key: %q", gotKey)
	}
	if gotValue != "false" {
		t.Fatalf("unexpected output value: %q", gotValue)
	}
}

func TestRunWith_ReturnsError_WhenPrepareFails(t *testing.T) {
	t.Parallel()

	prepare := func() (*Config, error) {
		return nil, errors.New("bad config")
	}

	detect := func(_ *Config, _ CommandRunner) (bool, error) {
		t.Fatal("detect should not be called")
		return false, nil
	}

	write := func(_, _ string) bool {
		t.Fatal("write should not be called")
		return false
	}

	runner := newMockCommandRunner(nil, nil)

	err := runWith(prepare, detect, write, runner)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "error preparing configuration: bad config") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunWith_ReturnsError_WhenDetectFails(t *testing.T) {
	t.Parallel()

	cfg := &Config{}

	prepare := func() (*Config, error) {
		return cfg, nil
	}

	detect := func(gotCfg *Config, runner CommandRunner) (bool, error) {
		if gotCfg != cfg {
			t.Fatalf("detect got unexpected config pointer")
		}
		return false, errors.New("git failure")
	}

	write := func(_, _ string) bool {
		t.Fatal("write should not be called")
		return false
	}

	runner := newMockCommandRunner(nil, nil)

	err := runWith(prepare, detect, write, runner)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "error detecting changes: git failure") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunWith_ReturnsError_WhenWriteFails(t *testing.T) {
	t.Parallel()

	cfg := &Config{}

	prepare := func() (*Config, error) {
		return cfg, nil
	}

	detect := func(gotCfg *Config, runner CommandRunner) (bool, error) {
		if gotCfg != cfg {
			t.Fatalf("detect got unexpected config pointer")
		}
		return true, nil
	}

	write := func(_, _ string) bool {
		return false
	}

	runner := newMockCommandRunner(nil, nil)

	err := runWith(prepare, detect, write, runner)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "failed to write to GitHub output") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDetectChanges_Success(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	runner := newMockCommandRunner(nil, nil)

	detect := func(gotCfg *Config, gotRunner CommandRunner) (bool, error) {
		if gotCfg != cfg {
			t.Fatalf("detect got unexpected config pointer")
		}

		if _, ok := gotRunner.(MockCommandRunner); !ok {
			t.Fatalf("detect got unexpected runner type: %T", gotRunner)
		}

		return true, nil
	}

	changed, err := detectChanges(cfg, detect, runner)
	if err != nil {
		t.Fatalf("detectChanges returned unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
}

func TestDetectChanges_ReturnsWrappedError(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	runner := newMockCommandRunner(nil, nil)

	detect := func(_ *Config, _ CommandRunner) (bool, error) {
		return false, errors.New("diff failed")
	}

	changed, err := detectChanges(cfg, detect, runner)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if changed {
		t.Fatal("expected changed=false")
	}
	if !strings.Contains(err.Error(), "error detecting changes: diff failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteChangesOutput_WritesTrue(t *testing.T) {
	t.Parallel()

	var gotKey string
	var gotValue string

	write := func(key, value string) bool {
		gotKey = key
		gotValue = value
		return true
	}

	err := writeChangesOutput(true, write)
	if err != nil {
		t.Fatalf("writeChangesOutput returned unexpected error: %v", err)
	}

	if gotKey != "has_changes" {
		t.Fatalf("unexpected output key: %q", gotKey)
	}
	if gotValue != "true" {
		t.Fatalf("unexpected output value: %q", gotValue)
	}
}

func TestWriteChangesOutput_WritesFalse(t *testing.T) {
	t.Parallel()

	var gotKey string
	var gotValue string

	write := func(key, value string) bool {
		gotKey = key
		gotValue = value
		return true
	}

	err := writeChangesOutput(false, write)
	if err != nil {
		t.Fatalf("writeChangesOutput returned unexpected error: %v", err)
	}

	if gotKey != "has_changes" {
		t.Fatalf("unexpected output key: %q", gotKey)
	}
	if gotValue != "false" {
		t.Fatalf("unexpected output value: %q", gotValue)
	}
}

func TestWriteChangesOutput_ReturnsError_WhenWriteFails(t *testing.T) {
	t.Parallel()

	write := func(_, _ string) bool {
		return false
	}

	err := writeChangesOutput(true, write)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "failed to write to GitHub output") {
		t.Fatalf("unexpected error: %v", err)
	}
}
