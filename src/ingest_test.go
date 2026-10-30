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
		{"fx", []string{"fx", "ask", "--json", "--prompt-permissions", "Invoke prop-review-ingest", "fx", "--resume", "ingest-created-id"}},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			root, log := ingestFixture(t)
			checkLaunch := func() {
				t.Helper()
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
				if !strings.Contains(string(data), "same language as the selected proposal text") {
					t.Fatalf("missing proposal-language instruction: %s", data)
				}
				if tc.engine == "fx" {
					lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
					if len(lines) != 11 || lines[0] != "fx" || lines[2] != "ask" || lines[3] != "--json" || lines[4] != "--prompt-permissions" || lines[5] != "--" || !strings.HasPrefix(lines[6], "Invoke prop-review-ingest") || lines[7] != "fx" || lines[8] != lines[1] || lines[9] != "--resume" || lines[10] != "ingest-created-id" {
						t.Fatalf("fx ingest must create a saved session then resume its exact ID: %q", data)
					}
				}
			}
			if err := setEngine(tc.engine); err != nil {
				t.Fatal(err)
			}
			if err := run([]string{"ingest"}); err != nil {
				t.Fatal(err)
			}
			checkLaunch()
			if err := os.Remove(log); err != nil {
				t.Fatal(err)
			}
			if err := setEngine("pi"); err != nil {
				t.Fatal(err)
			}
			if err := run([]string{"ingest", tc.engine}); err != nil {
				t.Fatalf("explicit engine: %v", err)
			}
			checkLaunch()
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
	if err != nil || strings.Count(string(logData), "fx\n") != 1 || strings.Contains(string(logData), "--resume") {
		t.Fatalf("fx should not resume after failure: %q, %v", logData, err)
	}
}

func TestIngestRejectsMissingOrInvalidSessionID(t *testing.T) {
	for _, response := range []string{
		`not JSON`,
		`{"exit_code":0,"final_output":"完了"}`,
		`{"session_id":null,"exit_code":0,"final_output":"完了"}`,
		`{"session_id":42,"exit_code":0,"final_output":"完了"}`,
		`{"session_id":"last","exit_code":0,"final_output":"完了"}`,
		`{"session_id":"--continue","exit_code":0,"final_output":"完了"}`,
		`{"session_id":"bad\nID","exit_code":0,"final_output":"完了"}`,
		`{"session_id":"created","exit_code":1,"final_output":"失敗"}`,
		`{"session_id":"created","exit_code":0,"final_output":""}`,
	} {
		_, log := ingestFixture(t)
		t.Setenv("INGEST_RESPONSE", response)
		if err := run([]string{"ingest", "fx"}); err == nil {
			t.Fatalf("accepted response: %s", response)
		}
		data, err := os.ReadFile(log)
		if err != nil || strings.Count(string(data), "fx\n") != 1 || strings.Contains(string(data), "--resume") || strings.Contains(string(data), "--continue") {
			t.Fatalf("must not resume on invalid response: %q, %v", data, err)
		}
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
	for _, engine := range []string{"pi", "fx"} {
		program := "#!/bin/sh\nprintf '%s\\n' '" + engine + "' \"$PWD\" \"$@\" >> \"$INGEST_LOG\"\n[ \"$INGEST_FAIL\" != 1 ] || exit 1\nif [ \"$1\" = ask ]; then printf '%s' \"$INGEST_RESPONSE\"; fi\n"
		if err := os.WriteFile(filepath.Join(bin, engine), []byte(program), 0700); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(t.TempDir(), "launch.log")
	t.Setenv("INGEST_LOG", log)
	t.Setenv("INGEST_RESPONSE", `{"session_id":"ingest-created-id","final_output":"取り込み完了","exit_code":0}`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return root, log
}
