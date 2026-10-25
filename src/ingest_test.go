package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIngestLaunchesConfiguredOrExplicitEngine(t *testing.T) {
	for _, tc := range []struct {
		engine string
		want   []string
	}{
		{"pi", []string{"pi", "--skill", "SKILL.md", "Invoke prop-review-ingest"}},
		{"claude", []string{"claude", "Invoke prop-review-ingest"}},
		{"codex", []string{"codex", "Invoke prop-review-ingest"}},
		{"agy", []string{"agy", "--prompt-interactive", "Invoke prop-review-ingest"}},
		{"opencode", []string{"opencode", "--prompt", "Invoke prop-review-ingest"}},
		{"fx", []string{"fx", "ask", "--prompt-permissions", "Invoke prop-review-ingest", "fx", "--continue"}},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			root, log := ingestFixture(t)
			if err := setEngine(tc.engine); err != nil {
				t.Fatal(err)
			}
			if err := run([]string{"ingest"}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			for _, part := range tc.want {
				if !strings.Contains(string(data), part) {
					t.Errorf("launch missing %q: %s", part, data)
				}
			}
			if !strings.Contains(string(data), root) || !strings.Contains(string(data), "SKILL.md") {
				t.Errorf("launch lacks root or skill: %s", data)
			}
			if err := setEngine("pi"); err != nil {
				t.Fatal(err)
			}
			if err := run([]string{"ingest", tc.engine}); err != nil {
				t.Fatalf("explicit engine: %v", err)
			}
		})
	}
}

func TestIngestRejectsInvalidArgumentsAndRepo(t *testing.T) {
	_, log := ingestFixture(t)
	for _, args := range [][]string{{"ingest", "bad"}, {"ingest", "pi", "extra"}} {
		if err := run(args); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Fatalf("args %v: %v", args, err)
		}
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("agent launched on invalid args: %v", err)
	}
	t.Chdir(t.TempDir())
	if err := run([]string{"ingest"}); err == nil || !strings.Contains(err.Error(), "repository root") {
		t.Fatalf("non-repository: %v", err)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("agent launched outside repository: %v", err)
	}
}

func TestIngestFailureKeepsReview(t *testing.T) {
	root, log := ingestFixture(t)
	result := filepath.Join(root, "prop-review-tmp", "review.1", "proposal.review.txt")
	if err := os.MkdirAll(filepath.Dir(result), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("Approval/Comment: Approved\n")
	if err := os.WriteFile(result, original, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INGEST_FAIL", "1")
	if err := run([]string{"ingest", "fx"}); err == nil || !strings.Contains(err.Error(), "fx failed") {
		t.Fatalf("failed fx ask: %v", err)
	}
	data, err := os.ReadFile(result)
	if err != nil || string(data) != string(original) {
		t.Fatalf("review changed: %q, %v", data, err)
	}
	logData, err := os.ReadFile(log)
	if err != nil || strings.Count(string(logData), "fx\n") != 1 || strings.Contains(string(logData), "--continue") {
		t.Fatalf("fx should not resume after failure: %q, %v", logData, err)
	}
}

func ingestFixture(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	skill := filepath.Join(home, ".pi", "agent", "skills", "prop-review-ingest", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skill, []byte("# test skill\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if output, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	t.Chdir(root)
	bin := t.TempDir()
	for _, engine := range []string{"pi", "claude", "codex", "agy", "opencode", "fx"} {
		program := "#!/bin/sh\nprintf '%s\\n' '" + engine + "' \"$PWD\" \"$@\" >> \"$INGEST_LOG\"\n[ \"$INGEST_FAIL\" != 1 ]\n"
		if err := os.WriteFile(filepath.Join(bin, engine), []byte(program), 0700); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(t.TempDir(), "launch.log")
	t.Setenv("INGEST_LOG", log)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return root, log
}
