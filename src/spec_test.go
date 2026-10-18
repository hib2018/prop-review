package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func specFixtureEngine(t *testing.T, promptFile, output string) {
	t.Helper()
	bin := filepath.Join(filepath.Dir(promptFile), "fx")
	script := "#!/bin/sh\nprompt=$(cat)\nprintf '%s' \"$prompt\" > \"$CLARIFY_PROMPT\"\ncase \"$prompt\" in\n  *'Write a clear, concise Markdown reference memo'*) printf '%s\\n' \"$SPEC_OUTPUT\" ;;\n  *) printf '%s\\n' \"$CLARIFY_OUTPUT\" ;;\nesac\n"
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPEC_OUTPUT", output)
}

func inFixtureRepo(t *testing.T, root string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
}

func TestSaveSpecAllReviewedChains(t *testing.T) {
	root, original, promptFile := clarificationFixture(t)
	specFixtureEngine(t, promptFile, "## まとめ\n\n承認された内容を整理したメモ。")
	child, err := saveProposal(root, []item{{topic: "未確認"}, {topic: "改訂済み"}}, original)
	if err != nil {
		t.Fatal(err)
	}
	writeReview(t, child, []item{{topic: "未確認", answer: "Approved"}, {topic: "改訂済み", answer: "Approved"}})
	other, err := saveProposalWithMetadata(root, []item{{topic: "別の承認"}}, "", "別の依頼", false)
	if err != nil {
		t.Fatal(err)
	}
	writeReview(t, other, []item{{topic: "別の承認", answer: "Approved"}})
	inFixtureRepo(t, root)
	t.Setenv("CLARIFY_OUTPUT", `[]`)
	var screen strings.Builder
	if err := saveSpecWithConfirmation("fx", strings.NewReader("y\n"), &screen); err != nil {
		t.Fatal(err)
	}
	if strings.Count(screen.String(), "Checking spec clarifications with fx...\n") != 1 || strings.Contains(screen.String(), "Summarizing approved items") || !strings.Contains(screen.String(), "Generated memo for ") {
		t.Fatalf("unexpected spec progress: %s", screen.String())
	}
	output := filepath.Join(root, "prop-review-tmp", "spec.md")
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "## まとめ") || strings.Contains(string(data), "review.") {
		t.Fatalf("unexpected memo: %s", data)
	}
	prompt, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, topic := range []string{"承認事項", "未確認", "改訂済み", "別の承認"} {
		if !strings.Contains(string(prompt), topic) {
			t.Errorf("missing approved input %s", topic)
		}
	}
	if strings.Contains(string(prompt), "要改訂") || strings.Contains(string(prompt), "変更して") || strings.Contains(string(prompt), "review.") {
		t.Fatalf("unapproved material or provenance in summary prompt: %s", prompt)
	}
	if err := saveSpecWithConfirmation("fx", strings.NewReader("save\n"), io.Discard); err == nil {
		t.Fatal("overwrote without y confirmation")
	}
}

func TestSaveSpecAllowsClarificationAndLegacyRoot(t *testing.T) {
	root, _, promptFile := clarificationFixture(t)
	specFixtureEngine(t, promptFile, "承認事項と旧承認をまとめたメモ。")
	legacy, err := saveProposal(root, []item{{topic: "旧承認"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	writeReview(t, legacy, []item{{topic: "旧承認", answer: "Approved"}})
	inFixtureRepo(t, root)
	t.Setenv("CLARIFY_OUTPUT", `["追加確認"]`)
	if err := saveSpecWithConfirmation("fx", strings.NewReader("y\n"), io.Discard); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "prop-review-tmp", "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "承認事項と旧承認をまとめたメモ。") {
		t.Fatalf("wrong memo: %s", data)
	}
	prompt, err := os.ReadFile(promptFile)
	if err != nil || !strings.Contains(string(prompt), "旧承認") || strings.Contains(string(prompt), "未確認") {
		t.Fatalf("wrong summary input: %s, %v", prompt, err)
	}
}

func TestSaveSpecRejectsEmptySummary(t *testing.T) {
	root, original, promptFile := clarificationFixture(t)
	specFixtureEngine(t, promptFile, "")
	writeReview(t, original, []item{{topic: "承認事項", answer: "Approved"}, {topic: "未確認"}, {topic: "要改訂", answer: "変更して", comment: true}})
	inFixtureRepo(t, root)
	t.Setenv("CLARIFY_OUTPUT", `[]`)
	if err := saveSpecWithConfirmation("fx", strings.NewReader("y\n"), io.Discard); err == nil {
		t.Fatal("accepted empty summary")
	}
	if _, err := os.Stat(filepath.Join(root, "prop-review-tmp", "spec.md")); !os.IsNotExist(err) {
		t.Fatalf("unexpected spec: %v", err)
	}
}

func TestConfirmSpecRequiresApprovalForCreateAndOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.md")
	var preview strings.Builder
	if err := confirmSpec(path, "new memo", strings.NewReader("no\n"), &preview); err == nil {
		t.Fatal("saved without confirmation")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cancelled create wrote file: %v", err)
	}
	if !strings.Contains(preview.String(), "new memo") || !strings.Contains(preview.String(), "Save new file? [y/N]") {
		t.Fatalf("missing preview: %s", preview.String())
	}
	if err := confirmSpec(path, "first", strings.NewReader("y\n"), io.Discard); err != nil {
		t.Fatal(err)
	}
	preview.Reset()
	if err := confirmSpec(path, "second", strings.NewReader("\n"), &preview); err == nil {
		t.Fatal("overwrote on default N")
	}
	if !strings.Contains(preview.String(), "second") || !strings.Contains(preview.String(), "Overwrite existing file? [y/N]") {
		t.Fatalf("missing overwrite preview: %s", preview.String())
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "first" {
		t.Fatalf("cancelled overwrite changed file: %q, %v", data, err)
	}
	if err := confirmSpec(path, "second", strings.NewReader("Y\n"), io.Discard); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "second" {
		t.Fatalf("approved overwrite: %q, %v", data, err)
	}
}

func TestConfirmSpecRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	if err := os.WriteFile(outside, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "spec.md")
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if err := confirmSpec(path, "bad", strings.NewReader("y\n"), io.Discard); err == nil {
		t.Fatal("accepted symlink")
	}
	data, _ := os.ReadFile(outside)
	if string(data) != "safe" {
		t.Fatal("changed symlink target")
	}
}

func TestSaveSpecRejectsBrokenReview(t *testing.T) {
	root, original, _ := clarificationFixture(t)
	inFixtureRepo(t, root)
	if err := os.WriteFile(filepath.Join(filepath.Dir(original), "proposal.review.txt"), []byte("bad\nApproval/Comment:Approved\n\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := saveSpecWithConfirmation("fx", strings.NewReader("y\n"), io.Discard); err == nil {
		t.Fatal("accepted mismatched review")
	}
	if _, err := os.Stat(filepath.Join(root, "prop-review-tmp", "spec.md")); !os.IsNotExist(err) {
		t.Fatalf("unexpected spec: %v", err)
	}
}
