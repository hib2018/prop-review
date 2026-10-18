package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func clarificationFixture(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", output, err)
	}
	bin := t.TempDir()
	fx := filepath.Join(bin, "fx")
	if err := os.WriteFile(fx, []byte("#!/bin/sh\ncat > \"$CLARIFY_PROMPT\"\nprintf '%s\\n' \"$CLARIFY_OUTPUT\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	prompt := filepath.Join(bin, "prompt")
	t.Setenv("CLARIFY_PROMPT", prompt)
	t.Setenv("CLARIFY_OUTPUT", `["追加で確認する？"]`)
	original, err := saveProposalWithMetadata(root, []item{{topic: "承認事項"}, {topic: "未確認"}, {topic: "要改訂"}}, "", "元の依頼", false)
	if err != nil {
		t.Fatal(err)
	}
	writeReview(t, original, []item{{topic: "承認事項", answer: "Approved"}, {topic: "未確認"}, {topic: "要改訂", answer: "変更して", comment: true}})
	return root, original, prompt
}

func writeReview(t *testing.T, path string, items []item) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "proposal.review.txt"), []byte(render(items)), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestReviewRecommendations(t *testing.T) {
	var out strings.Builder
	clarificationAdvice([]reviewStep{{items: []item{{topic: "Needs revision", answer: "fix", comment: true}, {topic: "Still pending"}}}}, &out)
	if !strings.Contains(out.String(), "prop-review revise") || !strings.Contains(out.String(), "review pending items again") {
		t.Fatalf("clarify advice: %q", out.String())
	}
	out.Reset()
	clarificationAdvice([]reviewStep{{items: []item{{topic: "Needs revision", answer: "fix", comment: true}}}, {items: []item{{topic: "Ready", answer: "Approved"}}}}, &out)
	if strings.Contains(out.String(), "revise") {
		t.Fatalf("superseded comments should not trigger advice: %q", out.String())
	}
	out.Reset()
	clarificationAdvice([]reviewStep{{items: []item{{topic: "Still pending"}}}}, &out)
	if strings.Contains(out.String(), "revise") || !strings.Contains(out.String(), "review pending items again") {
		t.Fatalf("pending-only advice: %q", out.String())
	}
	out.Reset()
	revisePendingAdvice(true, true, &out)
	if !strings.Contains(out.String(), "revised proposal") {
		t.Fatalf("revise advice: %q", out.String())
	}
	out.Reset()
	revisePendingAdvice(true, false, &out)
	if !strings.Contains(out.String(), "only changes commented items") {
		t.Fatalf("pending-only revise advice: %q", out.String())
	}
	out.Reset()
	revisePendingAdvice(false, true, &out)
	if out.Len() != 0 {
		t.Fatalf("unexpected advice: %q", out.String())
	}
}

func TestClarificationHistoryAndGeneration(t *testing.T) {
	root, original, promptFile := clarificationFixture(t)
	other, err := saveProposalWithMetadata(root, []item{{topic: "他の依頼"}}, "", "無関係な依頼", false)
	if err != nil {
		t.Fatal(err)
	}
	writeReview(t, other, []item{{topic: "他の依頼", answer: "Approved"}})
	roots, err := clarificationRoots(root)
	if err != nil || len(roots) != 2 {
		t.Fatalf("roots: %v, %v", roots, err)
	}
	index := 0
	for i, path := range roots {
		if path == original {
			index = i + 1
		}
	}
	if index == 0 {
		t.Fatal("original root not listed")
	}
	var menu strings.Builder
	selected, err := selectClarificationRoot(roots, strings.NewReader(strconv.Itoa(index)+"\n"), &menu)
	if err != nil || selected != original || !strings.Contains(menu.String(), "元の依頼") || !strings.Contains(menu.String(), "無関係な依頼") {
		t.Fatalf("selection: %s, %v, %s", selected, err, menu.String())
	}
	if _, err := selectClarificationRoot(roots, strings.NewReader("q\n"), io.Discard); err == nil {
		t.Fatal("cancel should stop")
	}
	revised, err := saveProposal(root, []item{{topic: "未確認"}, {topic: "改訂済み"}}, original)
	if err != nil {
		t.Fatal(err)
	}
	writeReview(t, revised, []item{{topic: "未確認"}, {topic: "改訂済み", answer: "Approved"}})
	items, parent, err := clarificationQuestions(root, original, "fx")
	if err != nil || len(items) != 2 || items[0].topic != "未確認" || items[1].topic != "追加で確認する？" || parent != revised {
		t.Fatalf("generated: %v, %s, %v", items, parent, err)
	}
	prompt, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"元の依頼", "承認事項", "改訂済み", "未確認", "変更して"} {
		if !strings.Contains(string(prompt), expected) {
			t.Errorf("missing %s", expected)
		}
	}
	if strings.Contains(string(prompt), "他の依頼") {
		t.Fatal("mixed unrelated root")
	}
	followup, err := saveProposalWithMetadata(root, items, parent, "", true)
	if err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(filepath.Dir(followup), "followup.txt"))
	if err != nil || string(marker) != "followup\n" {
		t.Fatalf("marker: %q, %v", marker, err)
	}
	writeReview(t, followup, []item{{topic: items[0].topic}, {topic: items[1].topic, answer: "Approved"}})
	refs, err := deletionReferences(root)
	if err != nil || !refs[filepath.Base(filepath.Dir(revised))] {
		t.Fatalf("followup parent not protected: %v, %v", refs, err)
	}
	if _, _, err := clarificationHistory(root, original); err != nil {
		t.Fatalf("followup history: %v", err)
	}
}

func TestActivityProgress(t *testing.T) {
	root, _, _ := clarificationFixture(t)
	t.Setenv("CLARIFY_OUTPUT", `[]`)
	var progress bytes.Buffer
	_, err := proposalsWithActivity(root, "prompt", "fx", time.Second, &progress, true, "Checking spec clarifications")
	if err != nil || !strings.Contains(progress.String(), "Checking spec clarifications with fx...") || strings.Contains(progress.String(), "Generating proposals") {
		t.Fatalf("progress: %q, %v", progress.String(), err)
	}
}

func TestClarificationNoQuestionsAndInvalidHistory(t *testing.T) {
	root, original, promptFile := clarificationFixture(t)
	writeReview(t, original, []item{{topic: "承認事項", answer: "Approved"}, {topic: "未確認", answer: "Approved"}, {topic: "要改訂", answer: "Approved"}})
	t.Setenv("CLARIFY_OUTPUT", `[]`)
	items, _, err := clarificationQuestions(root, original, "fx")
	if err != nil || len(items) != 0 {
		t.Fatalf("empty: %v, %v", items, err)
	}
	if _, err := os.Stat(promptFile); err != nil {
		t.Fatalf("engine not called: %v", err)
	}
	before, _ := proposalPaths(root)
	if path, err := saveClarification(root, items, original); err != nil || path != "" {
		t.Fatalf("empty clarification saved: %s, %v", path, err)
	}
	after, _ := proposalPaths(root)
	if len(before) != len(after) {
		t.Fatal("empty result created a proposal")
	}
	for _, invalid := range []string{`null`, `not-json`, `["", "bad"]`} {
		t.Setenv("CLARIFY_OUTPUT", invalid)
		if _, _, err := clarificationQuestions(root, original, "fx"); err == nil {
			t.Fatalf("accepted %s", invalid)
		}
	}
	if err := os.Remove(filepath.Join(filepath.Dir(original), "request.txt")); err != nil {
		t.Fatal(err)
	}
	var menu strings.Builder
	if _, err := selectClarificationRoot([]string{original}, strings.NewReader("1\n"), &menu); err != nil || !strings.Contains(menu.String(), "invalid: missing request.txt") {
		t.Fatalf("missing request menu: %q, %v", menu.String(), err)
	}
	if _, _, err := clarificationQuestions(root, original, "fx"); err == nil || !strings.Contains(err.Error(), "invalid root: missing request.txt") {
		t.Fatalf("missing request: %v", err)
	}
}

func TestClarificationCarriesPendingWhenNoNewQuestions(t *testing.T) {
	root, original, _ := clarificationFixture(t)
	t.Setenv("CLARIFY_OUTPUT", `[]`)
	items, _, err := clarificationQuestions(root, original, "fx")
	if err != nil || len(items) != 1 || items[0].topic != "未確認" {
		t.Fatalf("pending was not carried: %v, %v", items, err)
	}
}

func TestRootRequestAndIssueTitleStorage(t *testing.T) {
	root := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", output, err)
	}
	for _, text := range []string{"依頼文そのもの", "GitHub issue のタイトル"} {
		if err := saveAndReview(root, []item{{topic: "確認案"}}, "", text, false); err != nil {
			t.Fatal(err)
		}
		paths, err := proposalPaths(root)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, path := range paths {
			data, err := os.ReadFile(filepath.Join(filepath.Dir(path), "request.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if string(data) == text {
				found = true
			}
		}
		if !found {
			t.Fatalf("root request/title not saved: %q", text)
		}
	}
}

func TestDeleteUnreviewedRootWithRequest(t *testing.T) {
	root := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", output, err)
	}
	path, err := saveProposalWithMetadata(root, []item{{topic: "確認案"}}, "", "保存した依頼文", false)
	if err != nil {
		t.Fatal(err)
	}
	var shown strings.Builder
	if err := deleteProposal(root, strings.NewReader("1\ndelete\n"), &shown); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shown.String(), "保存した依頼文") {
		t.Fatalf("request not shown before deletion: %q", shown.String())
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("review directory remains: %v", err)
	}
}

func TestClarificationRejectsBrokenHistoryBeforeEngine(t *testing.T) {
	for _, kind := range []string{"missing_review", "mismatch", "bad_parent", "fork", "pending_changed", "bad_marker"} {
		t.Run(kind, func(t *testing.T) {
			root, original, promptFile := clarificationFixture(t)
			child, err := saveProposal(root, []item{{topic: "未確認"}, {topic: "改訂済み"}}, original)
			if err != nil {
				t.Fatal(err)
			}
			writeReview(t, child, []item{{topic: "未確認"}, {topic: "改訂済み", answer: "Approved"}})
			switch kind {
			case "missing_review":
				os.Remove(filepath.Join(filepath.Dir(child), "proposal.review.txt"))
			case "mismatch":
				os.WriteFile(filepath.Join(filepath.Dir(child), "proposal.review.txt"), []byte("wrong\nApproval/Comment:Approved\n\n"), 0600)
			case "bad_parent":
				os.WriteFile(filepath.Join(filepath.Dir(child), "parent.txt"), []byte("../../outside\n"), 0600)
			case "fork":
				sibling, e := saveProposal(root, []item{{topic: "未確認"}, {topic: "改訂再び"}}, original)
				if e != nil {
					t.Fatal(e)
				}
				writeReview(t, sibling, []item{{topic: "未確認"}, {topic: "改訂再び", answer: "Approved"}})
			case "pending_changed":
				os.WriteFile(child, []byte(render([]item{{topic: "別の未確認"}, {topic: "改訂済み"}})), 0600)
				writeReview(t, child, []item{{topic: "別の未確認"}, {topic: "改訂済み", answer: "Approved"}})
			case "bad_marker":
				os.WriteFile(filepath.Join(filepath.Dir(child), "followup.txt"), []byte("wrong\n"), 0600)
			}
			if _, _, err := clarificationQuestions(root, original, "fx"); err == nil {
				t.Fatal("accepted broken history")
			}
			if _, err := os.Stat(promptFile); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("engine invoked despite bad history: %v", err)
			}
		})
	}
}
