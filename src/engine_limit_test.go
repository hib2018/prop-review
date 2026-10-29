package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func limitScript(t *testing.T, dir, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
}

func TestEngineStdoutAndStderrLimitsPreserveReviews(t *testing.T) {
	root, original, promptFile := clarificationFixture(t)
	inFixtureRepo(t, root)
	bin := filepath.Dir(promptFile)
	before, err := os.ReadFile(filepath.Join(filepath.Dir(original), "proposal.review.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, script, want string }{
		{"stdout", "head -c " + strconv.Itoa(maxEngineOutputBytes+1) + " /dev/zero", "stdout exceeds"},
		{"stderr", "printf 'diagnostic from fx\\n' >&2\nhead -c " + strconv.Itoa(maxEngineErrorBytes+1) + " /dev/zero >&2", "stderr exceeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limitScript(t, bin, "fx", tc.script)
			start := time.Now()
			if err := generate(true, "fx", original); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected size error, got %v", err)
			} else if tc.name == "stderr" && !strings.Contains(err.Error(), "diagnostic from fx") {
				t.Fatalf("lost bounded stderr diagnostic: %v", err)
			}
			if time.Since(start) > 5*time.Second {
				t.Fatal("oversized stream was not stopped promptly")
			}
			after, err := os.ReadFile(filepath.Join(filepath.Dir(original), "proposal.review.txt"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("original review changed: %v", err)
			}
			paths, err := proposalPaths(root)
			if err != nil || len(paths) != 1 {
				t.Fatalf("failed generation saved a revision: %v, %v", paths, err)
			}
		})
	}
}

func TestEngineStopsInfiniteOutput(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	limitScript(t, bin, "fx", "while :; do head -c 65536 /dev/zero; done")
	start := time.Now()
	if _, err := proposalsWithTimeout(t.TempDir(), "prompt", "fx", 10*time.Second, io.Discard); err == nil || !strings.Contains(err.Error(), "stdout exceeds") {
		t.Fatalf("did not stop output: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("engine kept running after exceeding output limit")
	}
}

func TestPiNpmAndNodeLimits(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	limitScript(t, bin, "npm", "head -c "+strconv.Itoa(maxNpmOutputBytes+1)+" /dev/zero")
	limitScript(t, bin, "node", "exit 99")
	if _, err := proposalsWithTimeout(t.TempDir(), "prompt", "pi", 10*time.Second, io.Discard); err == nil || !strings.Contains(err.Error(), "npm root -g output exceeds") {
		t.Fatalf("unbounded npm response: %v", err)
	}
	limitScript(t, bin, "npm", "printf '/fake/global/node_modules\\n'\nhead -c "+strconv.Itoa(maxEngineErrorBytes+1)+" /dev/zero >&2")
	if _, err := proposalsWithTimeout(t.TempDir(), "prompt", "pi", 10*time.Second, io.Discard); err == nil || !strings.Contains(err.Error(), "npm root -g stderr exceeds") {
		t.Fatalf("unbounded npm stderr: %v", err)
	}
	limitScript(t, bin, "npm", "printf '/fake/global/node_modules\\n'")
	for _, tc := range []struct{ name, script, want string }{
		{"stdout", "head -c " + strconv.Itoa(maxEngineOutputBytes+1) + " /dev/zero", "stdout exceeds"},
		{"stderr", "head -c " + strconv.Itoa(maxEngineErrorBytes+1) + " /dev/zero >&2", "stderr exceeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limitScript(t, bin, "node", tc.script)
			if _, err := proposalsWithTimeout(t.TempDir(), "prompt", "pi", 10*time.Second, io.Discard); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unbounded Pi response: %v", err)
			}
		})
	}
}
