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
	t.Setenv("SPEC_OUTPUT", fxJSON(t, output))
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
	t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `[]`))
	var screen strings.Builder
	if err := saveSpecWithConfirmation("fx", strings.NewReader("y\n"), &screen); err != nil {
		t.Fatal(err)
	}
	if strings.Count(screen.String(), "\r\x1b[2K") != 1 || strings.Contains(screen.String(), "Summarizing approved items") || !strings.Contains(screen.String(), "Generated memo for ") {
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
	t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `["追加確認"]`))
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

func TestSpecChecksCommentsWithoutReconstructingOrSavingProposal(t *testing.T) {
	root, _, promptFile := clarificationFixture(t)
	specFixtureEngine(t, promptFile, "Approved memo")
	inFixtureRepo(t, root)
	before, err := proposalPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	// A reconstruction request must fail loudly rather than silently generating a prop.
	fx := filepath.Join(filepath.Dir(promptFile), "fx")
	script, err := os.ReadFile(fx)
	if err != nil {
		t.Fatal(err)
	}
	script = []byte(strings.Replace(string(script), "case \"$prompt\" in", "case \"$prompt\" in\n  *'Reconstruct the proposal'*) exit 19 ;;", 1))
	if err := os.WriteFile(fx, script, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `[]`))
	var screen strings.Builder
	if err := saveSpecWithConfirmation("fx", strings.NewReader("y\n"), &screen); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(screen.String(), "Could not check additional questions") || !strings.Contains(screen.String(), "Additional questions for") {
		t.Fatalf("spec must check unresolved items without reconstructing: %s", screen.String())
	}
	after, err := proposalPaths(root)
	if err != nil || len(after) != len(before) {
		t.Fatalf("spec generated a proposal: %v, %v", after, err)
	}
	for _, path := range after {
		for _, name := range []string{"revision.txt", "followup.txt"} {
			if _, err := os.Stat(filepath.Join(filepath.Dir(path), name)); !os.IsNotExist(err) {
				t.Fatalf("spec wrote %s: %v", name, err)
			}
		}
	}
}

func TestSaveSpecRejectsEmptySummary(t *testing.T) {
	root, original, promptFile := clarificationFixture(t)
	specFixtureEngine(t, promptFile, "")
	writeReview(t, original, []item{{topic: "承認事項", answer: "Approved"}, {topic: "未確認"}, {topic: "要改訂", answer: "変更して", comment: true}})
	inFixtureRepo(t, root)
	t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `[]`))
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

func TestSaveSpecExcludesBrokenChainBeforeConfirmation(t *testing.T) {
	root, broken, promptFile := clarificationFixture(t)
	specFixtureEngine(t, promptFile, "正常な承認の要約")
	valid, err := saveProposalWithMetadata(root, []item{{topic: "正常な承認"}}, "", "正常な依頼", false)
	if err != nil {
		t.Fatal(err)
	}
	writeReview(t, valid, []item{{topic: "正常な承認", answer: "Approved"}})
	if err := os.WriteFile(filepath.Join(filepath.Dir(broken), "proposal.review.txt"), []byte("wrong\nApproval/Comment:Approved\n\n"), 0600); err != nil {
		t.Fatal(err)
	}
	inFixtureRepo(t, root)
	t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `[]`))
	output := filepath.Join(root, "prop-review-tmp", "spec.md")
	var preview strings.Builder
	if err := saveSpecWithConfirmation("fx", strings.NewReader("N\n"), &preview); err == nil {
		t.Fatal("saved after N")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("cancelled spec created: %v", err)
	}
	shown := preview.String()
	if !strings.Contains(shown, "正常な承認の要約") || !strings.Contains(shown, filepath.Base(filepath.Dir(broken))) || strings.Index(shown, "Warnings (excluded") > strings.Index(shown, "[y/N]") {
		t.Fatalf("missing warning before approval: %s", shown)
	}
	prompt, err := os.ReadFile(promptFile)
	if err != nil || !strings.Contains(string(prompt), "正常な承認") || strings.Contains(string(prompt), "承認事項") {
		t.Fatalf("invalid chain leaked into summary: %s, %v", prompt, err)
	}
	if err := saveSpecWithConfirmation("fx", strings.NewReader("y\n"), io.Discard); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil || !strings.Contains(string(data), "正常な承認の要約") {
		t.Fatalf("valid chain was not saved: %s, %v", data, err)
	}
}

func TestSaveSpecChecksValidChainDespiteBrokenUnrelatedLink(t *testing.T) {
	root, valid, promptFile := clarificationFixture(t)
	specFixtureEngine(t, promptFile, "承認事項のメモ")
	broken, err := saveProposal(root, []item{{topic: "孤立した承認"}}, valid)
	if err != nil {
		t.Fatal(err)
	}
	writeReview(t, broken, []item{{topic: "孤立した承認", answer: "Approved"}})
	if err := os.WriteFile(filepath.Join(filepath.Dir(broken), "parent.txt"), []byte("../../invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	inFixtureRepo(t, root)
	t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `["追加確認事項"]`))
	var preview strings.Builder
	if err := saveSpecWithConfirmation("fx", strings.NewReader("N\n"), &preview); err == nil {
		t.Fatal("saved after N")
	}
	shown := preview.String()
	if !strings.Contains(shown, "Additional questions for") || !strings.Contains(shown, "Excluded ") || strings.Contains(shown, "Could not check additional questions") {
		t.Fatalf("valid chain was not checked independently: %s", shown)
	}
	prompt, err := os.ReadFile(promptFile)
	if err != nil || !strings.Contains(string(prompt), "承認事項") || strings.Contains(string(prompt), "孤立した承認") {
		t.Fatalf("excluded chain leaked into summary: %s, %v", prompt, err)
	}
}

func TestSaveSpecContinuesAfterOneClarificationFailure(t *testing.T) {
	root, first, promptFile := clarificationFixture(t)
	writeReview(t, first, []item{{topic: "承認事項", answer: "Approved"}, {topic: "未確認"}, {topic: "要改訂", answer: "直して", comment: true}})
	second, err := saveProposalWithMetadata(root, []item{{topic: "別の承認"}}, "", "別の依頼", false)
	if err != nil {
		t.Fatal(err)
	}
	writeReview(t, second, []item{{topic: "別の承認", answer: "Approved"}})
	script := "#!/bin/sh\nprompt=$(cat)\nprintf '%s' \"$prompt\" > \"$CLARIFY_PROMPT\"\ncase \"$prompt\" in\n  *'Write a clear, concise Markdown reference memo'*) printf '%s\\n' '" + fxJSON(t, "承認事項と別の承認のメモ") + "' ;;\n  *'元の依頼'*) printf 'invalid-json\\n' ;;\n  *) printf '%s\\n' '" + fxJSON(t, "[]") + "' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(filepath.Dir(promptFile), "fx"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	inFixtureRepo(t, root)
	var preview strings.Builder
	if err := saveSpecWithConfirmation("fx", strings.NewReader("y\n"), &preview); err != nil {
		t.Fatal(err)
	}
	shown := preview.String()
	if !strings.Contains(shown, "Could not check additional questions") || !strings.Contains(shown, "retry with `prop-review clarify`") || strings.Index(shown, "Warnings (excluded") > strings.Index(shown, "[y/N]") {
		t.Fatalf("failure was not shown before approval: %s", shown)
	}
	prompt, err := os.ReadFile(promptFile)
	if err != nil || !strings.Contains(string(prompt), "承認事項") || !strings.Contains(string(prompt), "別の承認") {
		t.Fatalf("valid approvals missing from summary: %s, %v", prompt, err)
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
