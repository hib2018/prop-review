package main

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFXAndRevision(t *testing.T) {
	config := t.TempDir()
	t.Setenv("HOME", config)
	t.Setenv("XDG_CONFIG_HOME", config)
	if err := run([]string{"--engine", "fx"}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", out, err)
	}
	bin := t.TempDir()
	fx := filepath.Join(bin, "fx")
	if err := os.WriteFile(fx, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$FX_ARGS\"\nprintf '%s\\n' \"$FX_OUTPUT\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	argsPath := filepath.Join(bin, "args")
	t.Setenv("FX_ARGS", argsPath)
	t.Setenv("FX_OUTPUT", `["改訂案"]`)
	items, err := proposals(root, "prompt", "fx")
	if err != nil || len(items) != 1 || items[0].topic != "改訂案" {
		t.Fatalf("fx: %v, %v", items, err)
	}
	t.Setenv("FX_OUTPUT", `[`+strings.Repeat(`"提案",`, 9)+`"提案"]`)
	if items, err := proposals(root, "prompt", "fx"); err != nil || len(items) != 10 {
		t.Fatalf("10 proposals should be valid: %d, %v", len(items), err)
	}
	for _, bad := range []string{`[]`, `[` + strings.Repeat(`"提案",`, 10) + `"提案"]`, `["bad\nline"]`, `["承認", ""]`, `not-json`, `["文字化け�"]`} {
		t.Setenv("FX_OUTPUT", bad)
		if _, err := proposals(root, "prompt", "fx"); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	t.Setenv("FX_OUTPUT", `["改訂案"]`)
	original := []item{{topic: "承認案"}, {topic: "コメント案"}, {topic: "保留案"}}
	path, err := saveProposal(root, original)
	if err != nil {
		t.Fatal(err)
	}
	result := []item{{topic: "承認案", answer: "承認"}, {topic: "コメント案", answer: "直して", comment: true}, {topic: "保留案"}}
	resultPath := filepath.Join(filepath.Dir(path), "proposal.review.txt")
	if err := os.WriteFile(resultPath, []byte(render(result)), 0600); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	if err := run([]string{"revise"}); err != nil {
		t.Fatal(err)
	}
	newPath, err := latestProposal(root)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(newPath)
	if err != nil || string(data) != "改訂案\n承認/コメント：\n\n保留案\n承認/コメント：\n\n" {
		t.Fatalf("revision: %q, %v", data, err)
	}
	if _, err := os.Stat(resultPath); err != nil {
		t.Fatalf("original result lost: %v", err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil || !strings.Contains(string(args), `"topic":"コメント案","comment":"直して"`) {
		t.Fatalf("fx did not receive feedback: %s, %v", args, err)
	}
	countBefore, _ := filepath.Glob(filepath.Join(root, "prop-review-tmp", "review.*"))
	t.Setenv("FX_OUTPUT", `not-json`)
	if err := generate(true, "fx"); err == nil {
		t.Fatal("bad fx output should fail")
	}
	countAfter, _ := filepath.Glob(filepath.Join(root, "prop-review-tmp", "review.*"))
	if len(countBefore) != len(countAfter) {
		t.Fatal("failed generation created a proposal")
	}
	if out, err := exec.Command("git", "check-ignore", filepath.Join(root, "prop-review-tmp", "probe")).CombinedOutput(); err != nil {
		t.Fatalf("not ignored: %s: %v", out, err)
	}
}

func TestPiEngine(t *testing.T) {
	config := t.TempDir()
	t.Setenv("HOME", config)
	t.Setenv("XDG_CONFIG_HOME", config)
	if got, err := selectedEngine(); err != nil || got != "fx" {
		t.Fatalf("default engine: %q, %v", got, err)
	}
	if err := run([]string{"--engine", "pi"}); err != nil {
		t.Fatal(err)
	}
	if got, err := selectedEngine(); err != nil || got != "pi" {
		t.Fatalf("saved engine: %q, %v", got, err)
	}
	root := t.TempDir()
	bin := t.TempDir()
	argsPath := filepath.Join(bin, "args")
	pi := filepath.Join(bin, "pi")
	if err := os.WriteFile(pi, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$PI_ARGS\"\nprintf '%s\\n' \"$PI_OUTPUT\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PI_ARGS", argsPath)
	t.Setenv("PI_OUTPUT", `["Pi案"]`)
	engine, err := selectedEngine()
	if err != nil {
		t.Fatal(err)
	}
	items, err := proposals(root, "prompt", engine)
	if err != nil || len(items) != 1 || items[0].topic != "Pi案" {
		t.Fatalf("pi: %v, %v", items, err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil || !strings.Contains(string(args), "--print --no-session --no-extensions --no-skills --no-prompt-templates --tools read,grep,find,ls -- prompt") {
		t.Fatalf("pi args: %q, %v", args, err)
	}
	t.Setenv("PI_OUTPUT", "not-json")
	if _, err := proposals(root, "prompt", "pi"); err == nil {
		t.Fatal("bad pi output should fail")
	}
	for _, args := range [][]string{{"--engine", "other"}, {"--engine"}, {"generate", "--engine", "fx"}, {"revise", "--engine", "pi"}} {
		if err := run(args); err == nil {
			t.Fatalf("accepted args %v", args)
		}
	}
	if err := run([]string{"--engine", "fx"}); err != nil {
		t.Fatal(err)
	}
	if got, err := selectedEngine(); err != nil || got != "fx" {
		t.Fatalf("switched back: %q, %v", got, err)
	}
	if err := os.WriteFile(mustEnginePath(t), []byte("invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := selectedEngine(); err == nil {
		t.Fatal("invalid config should fail")
	}
}

func mustEnginePath(t *testing.T) string {
	t.Helper()
	path, err := enginePath()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLatestProposal(t *testing.T) {
	root := t.TempDir()
	makeProposal := func(name string, when time.Time) string {
		t.Helper()
		dir := filepath.Join(root, "prop-review-tmp", name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "proposal.txt")
		if err := os.WriteFile(path, []byte("変更する\n承認/コメント：\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if _, err := latestProposal(root); err == nil {
		t.Fatal("empty directory should fail")
	}
	older := makeProposal("review.old", time.Now().Add(-time.Hour))
	newer := makeProposal("review.new", time.Now())
	if got, err := latestProposal(root); err != nil || got != newer {
		t.Fatalf("latest: %q, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(newer), "proposal.review.txt"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := latestProposal(root); err != nil || got != older {
		t.Fatalf("skip reviewed: %q, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(older), "proposal.review.txt"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := latestProposal(root); err == nil {
		t.Fatal("all reviewed should fail")
	}
}

func TestReview(t *testing.T) {
	items, err := parse(strings.NewReader("変更する\n承認/コメント：\n\n変更しない\n承認/コメント：\n"))
	if err != nil {
		t.Fatal(err)
	}
	comment := func(r *bufio.Reader) (string, error) {
		return readComment(r, io.Discard, nil)
	}
	// Approve, go back, reapprove, then comment on the next item.
	if err := review(items, strings.NewReader("abacここは修正して\n"), io.Discard, comment); err != nil {
		t.Fatal(err)
	}
	want := "変更する\n承認/コメント：承認\n\n変更しない\n承認/コメント：コメント：\"ここは修正して\"\n\n"
	if got := render(items); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCommentAndFullwidthKeys(t *testing.T) {
	items, err := parse(strings.NewReader("変更する\n承認/コメント:\n\n変更しない\n承認/コメント：\n"))
	if err != nil {
		t.Fatal(err)
	}
	comment := func(r *bufio.Reader) (string, error) {
		return readComment(r, io.Discard, func(r *bufio.Reader) (bool, error) {
			key, _, err := r.ReadRune()
			return key == '\n', err
		})
	}
	// Empty comment cancelled with Esc; then comment "承認" without approving.
	if err := review(items, strings.NewReader("ｃ\n\x1bc承認\nａ"), io.Discard, comment); err != nil {
		t.Fatal(err)
	}
	want := "変更する\n承認/コメント：コメント：\"承認\"\n\n変更しない\n承認/コメント：承認\n\n"
	if got := render(items); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNoImplicitApproval(t *testing.T) {
	items, err := parse(strings.NewReader("変更する\n承認/コメント：\n"))
	if err != nil {
		t.Fatal(err)
	}
	comment := func(*bufio.Reader) (string, error) { return "", nil }
	if err := review(items, strings.NewReader("\n"), io.Discard, comment); err != nil {
		t.Fatal(err)
	}
	if legacy, err := parse(strings.NewReader("何について：変更する\n承認/コメント：\n")); err != nil || legacy[0].topic != "変更する" {
		t.Fatalf("legacy proposal: %v, %v", legacy, err)
	}
	if items[0].answer != "" {
		t.Fatal("blank answer should remain pending")
	}
	if err := review(items, strings.NewReader(""), io.Discard, comment); err == nil {
		t.Fatal("interrupted review should fail")
	}
	if err := review(items, strings.NewReader("q"), io.Discard, comment); err == nil {
		t.Fatal("cancelled review should fail")
	}
	if _, err := parse(strings.NewReader("変更する\n承認/コメント：承認\n")); err == nil {
		t.Fatal("prefilled approval should be rejected")
	}
	for _, topic := range []string{"\xff", "文字化け�"} {
		if _, err := parse(strings.NewReader(topic + "\n承認/コメント：\n")); err == nil {
			t.Fatal("corrupted topic should be rejected")
		}
	}
	answer, err := readComment(bufio.NewReader(strings.NewReader("文字化け�\n正常\n")), io.Discard, nil)
	if err != nil || answer != "正常" {
		t.Fatalf("corrupted comment should be retried: %q, %v", answer, err)
	}
	answer, err = readComment(bufio.NewReader(strings.NewReader("\n\n")), io.Discard, func(r *bufio.Reader) (bool, error) {
		key, _, err := r.ReadRune()
		return key == '\n', err
	})
	if err != nil || answer != "" {
		t.Fatalf("empty comment confirmation: %q, %v", answer, err)
	}
	var output strings.Builder
	line, err := inputLine(bufio.NewReader(strings.NewReader("あい\bう\n")), &output, "コメント：")
	if err != nil || line != "あう" || !strings.Contains(output.String(), "\x1b[2D\x1b[0K") || strings.Contains(output.String(), "\r") {
		t.Fatalf("Japanese deletion: %q, %v, %q", line, err, output.String())
	}
	var menu strings.Builder
	if err := review([]item{{topic: "test"}}, strings.NewReader("xyzq"), &menu, comment); err == nil {
		t.Fatal("q should cancel")
	}
	if strings.Count(menu.String(), "[1/1]") != 1 {
		t.Fatalf("invalid keys should not scroll menu: %q", menu.String())
	}
}
