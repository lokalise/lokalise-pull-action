package main

import (
	"fmt"
	"testing"
)

func TestResolveRealBase_UsesProvidedBase(t *testing.T) {
	runner := &MockCommandRunner{} // no calls expected
	cfg := &Config{BaseRef: "feature/xyz"}

	got, err := resolveRealBase(runner, cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "feature/xyz" {
		t.Fatalf("want feature/xyz, got %s", got)
	}
}

func TestResolveRealBase_LsRemoteWins(t *testing.T) {
	// ls-remote --symref provides the default branch → should be used first
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected bin: %s", name)
			}
			if len(args) >= 3 && args[0] == "ls-remote" && args[1] == "--symref" && args[2] == "origin" {
				// Note the CRLF and tabs; real git may spit CRLF on Windows.
				return "ref: refs/heads/develop\tHEAD\r\n0123456789abcdef\tHEAD\r\n", nil
			}
			// should not be called, but keep harmless
			return "", fmt.Errorf("unexpected capture: %s %v", name, args)
		},
	}
	cfg := &Config{BaseRef: "123/merge"} // synthetic

	got, err := resolveRealBase(runner, cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "develop" {
		t.Fatalf("want develop, got %s", got)
	}
}

func TestResolveRealBase_SymbolicRefFallback(t *testing.T) {
	// ls-remote fails → symbolic-ref gives "origin/main"
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected bin: %s", name)
			}
			if len(args) >= 3 && args[0] == "ls-remote" && args[1] == "--symref" && args[2] == "origin" {
				return "", fmt.Errorf("boom") // simulate network issue or no symref
			}
			if len(args) >= 4 && args[0] == "symbolic-ref" && args[1] == "--quiet" &&
				args[2] == "--short" && args[3] == "refs/remotes/origin/HEAD" {
				return "origin/main\n", nil
			}
			return "", fmt.Errorf("unexpected capture: %s %v", name, args)
		},
	}
	cfg := &Config{BaseRef: ""}

	got, err := resolveRealBase(runner, cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "main" {
		t.Fatalf("want main, got %s", got)
	}
}

func TestResolveRealBase_RemoteShowAsLastNetworkFallback(t *testing.T) {
	// ls-remote fails, symbolic-ref fails, remote show origin returns HEAD branch
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected bin: %s", name)
			}
			if len(args) >= 3 && args[0] == "ls-remote" && args[1] == "--symref" && args[2] == "origin" {
				return "", fmt.Errorf("no symref here")
			}
			if len(args) >= 4 && args[0] == "symbolic-ref" && args[1] == "--quiet" &&
				args[2] == "--short" && args[3] == "refs/remotes/origin/HEAD" {
				return "", fmt.Errorf("no local origin/HEAD")
			}
			if len(args) >= 2 && args[0] == "remote" && args[1] == "show" {
				return `
* remote origin
  Fetch URL: git@github.com:org/repo.git
  HEAD branch: release
  Remote branches:
    develop tracked
    release tracked
`, nil
			}
			return "", fmt.Errorf("unexpected capture: %s %v", name, args)
		},
	}
	cfg := &Config{BaseRef: "456/merge"}

	got, err := resolveRealBase(runner, cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "release" {
		t.Fatalf("want release, got %s", got)
	}
}

func TestResolveRealBase_FallbackToMainWhenEverythingFails(t *testing.T) {
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			return "", fmt.Errorf("nope")
		},
	}
	cfg := &Config{BaseRef: ""} // empty → synthetic

	got, err := resolveRealBase(runner, cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "main" {
		t.Fatalf("want main, got %s", got)
	}
}

func TestIsSyntheticRef(t *testing.T) {
	t.Parallel() // this test can run alongside other tests

	cases := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"merge", true},
		{"head", true},
		{"123/merge", true},
		{"123/head", true},
		{"refs/pull/45/merge", true},
		{"refs/pull/45/head", true},
		{"pull/99/merge", true},
		{"feature/foo", false},
		{"main", false},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("in=%q", c.in), func(t *testing.T) {
			t.Parallel()

			got := isSyntheticRef(c.in)
			if got != c.want {
				t.Errorf("isSyntheticRef(%q) = %v; want %v", c.in, got, c.want)
			}
		})
	}
}

func TestGetDefaultBranchFromLsRemote_CRLF_AndCutPrefix(t *testing.T) {
	// direct unit test for the helper: ensure CRLF and CutPrefix flow works
	out := "ref: refs/heads/qa\tHEAD\r\n123456\tHEAD\r\n"
	r := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			return out, nil
		},
	}
	br, ok := getDefaultBranchFromLsRemote(r)
	if !ok || br != "qa" {
		t.Fatalf("want qa/true, got %q/%v", br, ok)
	}
}

func TestResolveRealBase_ProvidedBaseIsTrimmed(t *testing.T) {
	runner := &MockCommandRunner{} // no calls expected
	cfg := &Config{BaseRef: "  feature/xyz  "}

	got, err := resolveRealBase(runner, cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "feature/xyz" {
		t.Fatalf("want feature/xyz, got %q", got)
	}
}

func TestResolveRealBase_MalformedLsRemoteFallsBackToSymbolicRef(t *testing.T) {
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected bin: %s", name)
			}

			if len(args) >= 3 && args[0] == "ls-remote" && args[1] == "--symref" && args[2] == "origin" {
				// Non-empty but not parseable as "ref: refs/heads/<branch>\tHEAD"
				return "ref: refs/tags/v1.0\tHEAD\n", nil
			}

			if len(args) >= 4 &&
				args[0] == "symbolic-ref" &&
				args[1] == "--quiet" &&
				args[2] == "--short" &&
				args[3] == "refs/remotes/origin/HEAD" {
				return "origin/main\n", nil
			}

			return "", fmt.Errorf("unexpected capture: %s %v", name, args)
		},
	}

	cfg := &Config{BaseRef: "refs/pull/123/merge"}

	got, err := resolveRealBase(runner, cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "main" {
		t.Fatalf("want main, got %q", got)
	}
}

func TestGetDefaultBranchFromLsRemote_NoMatchingHeadRef(t *testing.T) {
	tests := []struct {
		name string
		out  string
	}{
		{
			name: "empty output",
			out:  "",
		},
		{
			name: "no ref line",
			out:  "0123456789abcdef\tHEAD\n",
		},
		{
			name: "ref is not refs heads",
			out:  "ref: refs/tags/v1.0\tHEAD\n",
		},
		{
			name: "missing HEAD suffix",
			out:  "ref: refs/heads/main\tNOT_HEAD\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &MockCommandRunner{
				CaptureFunc: func(name string, args ...string) (string, error) {
					return tt.out, nil
				},
			}

			br, ok := getDefaultBranchFromLsRemote(r)
			if ok {
				t.Fatalf("expected ok=false, got branch=%q", br)
			}
			if br != "" {
				t.Fatalf("expected empty branch, got %q", br)
			}
		})
	}
}

func TestGetDefaultBranchFromSymbolicRef(t *testing.T) {
	t.Run("origin slash branch returns suffix", func(t *testing.T) {
		r := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				return "origin/feature/x\n", nil
			},
		}

		br, ok := getDefaultBranchFromSymbolicRef(r)
		if !ok || br != "feature/x" {
			t.Fatalf("want feature/x/true, got %q/%v", br, ok)
		}
	})

	t.Run("plain branch name is returned as is", func(t *testing.T) {
		r := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				return "main\n", nil
			},
		}

		br, ok := getDefaultBranchFromSymbolicRef(r)
		if !ok || br != "main" {
			t.Fatalf("want main/true, got %q/%v", br, ok)
		}
	})

	t.Run("empty output returns false", func(t *testing.T) {
		r := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				return "   \n", nil
			},
		}

		br, ok := getDefaultBranchFromSymbolicRef(r)
		if ok || br != "" {
			t.Fatalf("want empty/false, got %q/%v", br, ok)
		}
	})

	t.Run("capture error returns false", func(t *testing.T) {
		r := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				return "", fmt.Errorf("boom")
			},
		}

		br, ok := getDefaultBranchFromSymbolicRef(r)
		if ok || br != "" {
			t.Fatalf("want empty/false, got %q/%v", br, ok)
		}
	})
}

func TestGetDefaultBranchFromRemoteShow(t *testing.T) {
	t.Run("parses head branch line", func(t *testing.T) {
		r := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				return `
* remote origin
  Fetch URL: git@github.com:org/repo.git
  HEAD branch: release
  Remote branches:
    develop tracked
    release tracked
`, nil
			},
		}

		br, ok := getDefaultBranchFromRemoteShow(r)
		if !ok || br != "release" {
			t.Fatalf("want release/true, got %q/%v", br, ok)
		}
	})

	t.Run("empty output returns false", func(t *testing.T) {
		r := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				return "\n", nil
			},
		}

		br, ok := getDefaultBranchFromRemoteShow(r)
		if ok || br != "" {
			t.Fatalf("want empty/false, got %q/%v", br, ok)
		}
	})

	t.Run("capture error returns false", func(t *testing.T) {
		r := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				return "", fmt.Errorf("boom")
			},
		}

		br, ok := getDefaultBranchFromRemoteShow(r)
		if ok || br != "" {
			t.Fatalf("want empty/false, got %q/%v", br, ok)
		}
	})

	t.Run("head branch line with empty value returns false", func(t *testing.T) {
		r := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				return "  HEAD branch:   \n", nil
			},
		}

		br, ok := getDefaultBranchFromRemoteShow(r)
		if ok || br != "" {
			t.Fatalf("want empty/false, got %q/%v", br, ok)
		}
	})
}

func TestIsSyntheticRef_WhitespaceIsTrimmed(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"  merge  ", true},
		{"  head  ", true},
		{"  refs/pull/45/merge  ", true},
		{"  feature/foo  ", false},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("in=%q", c.in), func(t *testing.T) {
			got := isSyntheticRef(c.in)
			if got != c.want {
				t.Fatalf("isSyntheticRef(%q) = %v; want %v", c.in, got, c.want)
			}
		})
	}
}
