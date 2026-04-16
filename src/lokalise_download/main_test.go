package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// Override exitFunc for testing
	exitFunc = func(code int) {
		panic(fmt.Sprintf("Exit called with code %d", code))
	}

	// Run tests
	code := m.Run()

	// Restore exitFunc after testing
	exitFunc = os.Exit

	os.Exit(code)
}

func TestRunWith_Success(t *testing.T) {
	t.Parallel()

	cfg := DownloadConfig{
		DownloadTimeout: 50 * time.Millisecond,
	}

	prepareCalled := false
	validateCalled := false
	downloadCalled := false

	factory := &fakeFactory{}

	prepare := func() DownloadConfig {
		prepareCalled = true
		return cfg
	}

	validate := func(gotCfg DownloadConfig) error {
		validateCalled = true

		if gotCfg != cfg {
			t.Fatalf("validate got unexpected config: %#v", gotCfg)
		}

		return nil
	}

	download := func(ctx context.Context, gotCfg DownloadConfig, gotFactory ClientFactory) error {
		downloadCalled = true

		if gotCfg != cfg {
			t.Fatalf("download got unexpected config: %#v", gotCfg)
		}
		if gotFactory != factory {
			t.Fatalf("download got unexpected factory")
		}

		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("expected context with deadline")
		}
		if time.Until(deadline) <= 0 {
			t.Fatal("expected deadline in the future")
		}

		return nil
	}

	err := runWith(prepare, validate, download, factory)
	if err != nil {
		t.Fatalf("runWith returned unexpected error: %v", err)
	}

	if !prepareCalled {
		t.Fatal("expected prepare to be called")
	}
	if !validateCalled {
		t.Fatal("expected validate to be called")
	}
	if !downloadCalled {
		t.Fatal("expected download to be called")
	}
}

func TestRunWith_ReturnsError_WhenValidationFails(t *testing.T) {
	t.Parallel()

	cfg := DownloadConfig{
		DownloadTimeout: 50 * time.Millisecond,
	}

	prepareCalled := false
	downloadCalled := false

	prepare := func() DownloadConfig {
		prepareCalled = true
		return cfg
	}

	validate := func(gotCfg DownloadConfig) error {
		if gotCfg != cfg {
			t.Fatalf("validate got unexpected config: %#v", gotCfg)
		}

		return errors.New("missing token")
	}

	download := func(ctx context.Context, gotCfg DownloadConfig, gotFactory ClientFactory) error {
		downloadCalled = true
		return nil
	}

	err := runWith(prepare, validate, download, &fakeFactory{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !prepareCalled {
		t.Fatal("expected prepare to be called")
	}
	if downloadCalled {
		t.Fatal("download should not be called when validation fails")
	}
	if !strings.Contains(err.Error(), "invalid download config: missing token") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunWith_ReturnsError_WhenDownloadFails(t *testing.T) {
	t.Parallel()

	cfg := DownloadConfig{
		DownloadTimeout: 50 * time.Millisecond,
	}

	prepare := func() DownloadConfig {
		return cfg
	}

	validate := func(gotCfg DownloadConfig) error {
		if gotCfg != cfg {
			t.Fatalf("validate got unexpected config: %#v", gotCfg)
		}
		return nil
	}

	download := func(ctx context.Context, gotCfg DownloadConfig, gotFactory ClientFactory) error {
		if gotCfg != cfg {
			t.Fatalf("download got unexpected config: %#v", gotCfg)
		}

		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("expected context with deadline")
		}
		if time.Until(deadline) <= 0 {
			t.Fatal("expected deadline in the future")
		}

		return errors.New("download failed")
	}

	err := runWith(prepare, validate, download, &fakeFactory{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "download failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunWith_PassesPreparedConfigToValidateAndDownload(t *testing.T) {
	t.Parallel()

	cfg := DownloadConfig{
		ProjectID:       "proj-123",
		Token:           "token-123",
		FileFormat:      "json",
		DownloadTimeout: 50 * time.Millisecond,
	}

	validateSaw := false
	downloadSaw := false

	prepare := func() DownloadConfig {
		return cfg
	}

	validate := func(gotCfg DownloadConfig) error {
		validateSaw = true

		if gotCfg.ProjectID != "proj-123" {
			t.Fatalf("unexpected ProjectID: %q", gotCfg.ProjectID)
		}
		if gotCfg.Token != "token-123" {
			t.Fatalf("unexpected Token: %q", gotCfg.Token)
		}
		if gotCfg.FileFormat != "json" {
			t.Fatalf("unexpected FileFormat: %q", gotCfg.FileFormat)
		}

		return nil
	}

	download := func(ctx context.Context, gotCfg DownloadConfig, gotFactory ClientFactory) error {
		downloadSaw = true

		if gotCfg.ProjectID != "proj-123" {
			t.Fatalf("unexpected ProjectID: %q", gotCfg.ProjectID)
		}
		if gotCfg.Token != "token-123" {
			t.Fatalf("unexpected Token: %q", gotCfg.Token)
		}
		if gotCfg.FileFormat != "json" {
			t.Fatalf("unexpected FileFormat: %q", gotCfg.FileFormat)
		}

		return nil
	}

	err := runWith(prepare, validate, download, &fakeFactory{})
	if err != nil {
		t.Fatalf("runWith returned unexpected error: %v", err)
	}

	if !validateSaw {
		t.Fatal("expected validate to see config")
	}
	if !downloadSaw {
		t.Fatal("expected download to see config")
	}
}
