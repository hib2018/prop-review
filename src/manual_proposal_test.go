package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestManualProposalSkillRequestAndCopies(t *testing.T) {
	source, err := os.ReadFile("../skills/prop-review/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, instruction := range []string{
		"Save the original human request, with surrounding whitespace trimmed but otherwise unchanged, as UTF-8 `request.txt` in that directory, without appending a newline.",
		"It must be nonempty, at most 256 KiB in UTF-8 bytes, and contain no U+FFFD or invalid UTF-8; validate it before creating artifacts.",
		"Do not substitute a summary or the skill instructions.",
		"Use mode `0700` for newly created review directories and `0600` for files",
	} {
		if !strings.Contains(string(source), instruction) {
			t.Errorf("manual proposal instructions missing %q", instruction)
		}
	}
	for _, base := range []string{".agents/skills", ".pi/skills"} {
		path := filepath.Join("..", base, "prop-review", "SKILL.md")
		copy, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(copy) != string(source) {
			t.Errorf("%s differs from the source skill", path)
		}
	}
}

func TestManualProposalReviewClarifyReviseContext(t *testing.T) {
	root := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", output, err)
	}
	inFixtureRepo(t, root)
	bin := t.TempDir()
	promptFile := filepath.Join(bin, "prompt")
	if err := os.WriteFile(filepath.Join(bin, "fx"), []byte("#!/bin/sh\ncat > \"$CLARIFY_PROMPT\"\nprintf '%s\\n' \"$CLARIFY_OUTPUT\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CLARIFY_PROMPT", promptFile)
	t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `["追加の確認"]`))

	// Follow the manual skill, not the CLI's proposal-saving helpers.
	base := filepath.Join(root, "prop-review-tmp")
	if err := os.Mkdir(base, 0700); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(base, "review.")
	if err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(dir, "proposal.txt")
	request := strings.TrimSpace(" \n元の依頼そのものを保存する。\n  条件: 日本語と内部の空白を保つ。\n ")
	for name, data := range map[string]string{
		"request.txt":  request,
		"proposal.txt": "承認事項\nApproval/Comment:\n\n未確認事項\nApproval/Comment:\n\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// This exclusion belongs only to the disposable fixture repository.
	if err := os.WriteFile(filepath.Join(root, ".git", "info", "exclude"), []byte("/prop-review-tmp/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	items := clarifyRestructureItems(t, original)
	if err := review(items, strings.NewReader("a\n\n"), io.Discard, nil); err != nil {
		t.Fatal(err)
	}
	if err := saveReview(filepath.Join(dir, "proposal.review.txt"), items); err != nil {
		t.Fatal(err)
	}
	history, gotRequest := clarifyRestructureHistory(t, root, original)
	if gotRequest != request {
		t.Fatalf("manual request changed: %q; want %q", gotRequest, request)
	}
	followup, err := saveClarificationFromHistory(root, history, gotRequest, "fx", io.Discard)
	if err != nil || followup == "" {
		t.Fatalf("clarify manual proposal: %q, %v", followup, err)
	}
	clarifyRestructureMetadata(t, followup, original, false)
	checkPrompt := func(wantReviews int, comment string) {
		t.Helper()
		data, err := os.ReadFile(promptFile)
		if err != nil {
			t.Fatal(err)
		}
		start := strings.IndexByte(string(data), '{')
		if start < 0 {
			t.Fatalf("no context in prompt: %s", data)
		}
		var input reviewGenerationInput
		if err := json.NewDecoder(strings.NewReader(string(data[start:]))).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if input.Request != request || len(input.Reviews) != wantReviews || len(input.Approved) != 1 || input.Approved[0] != "承認事項" {
			t.Fatalf("lost manual request/review context: %+v", input)
		}
		if comment != "" && !strings.Contains(string(data), comment) {
			t.Fatalf("lost review comment %q: %s", comment, data)
		}
	}
	checkPrompt(1, "")
	items = clarifyRestructureItems(t, followup)
	if len(items) != 2 || items[0].topic != "未確認事項" || items[1].topic != "追加の確認" {
		t.Fatalf("clarification lost pending/new questions: %+v", items)
	}
	comment := "元の条件を維持して整理して"
	if err := review(items, strings.NewReader("c\n\n"), io.Discard, func(*bufio.Reader) (string, error) { return comment, nil }); err != nil {
		t.Fatal(err)
	}
	if err := saveReview(filepath.Join(filepath.Dir(followup), "proposal.review.txt"), items); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `["元依頼と履歴に基づく改訂案"]`))
	if err := generate(true, "fx", followup); err != nil {
		t.Fatal(err)
	}
	checkPrompt(2, comment)
	revised, err := latestProposal(root)
	if err != nil {
		t.Fatal(err)
	}
	clarifyRestructureMetadata(t, revised, followup, true)
	items = clarifyRestructureItems(t, revised)
	if len(items) != 1 || items[0].topic != "元依頼と履歴に基づく改訂案" {
		t.Fatalf("unexpected revised proposal: %+v", items)
	}
}
