package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	root, original, _ := clarificationFixture(t)
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
	if err := saveSpec("fx"); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "prop-review-tmp", "spec.md")
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, topic := range []string{"承認事項", "未確認", "改訂済み", "別の承認"} {
		if !strings.Contains(string(data), "- "+topic) {
			t.Errorf("missing approved item %s", topic)
		}
	}
	if strings.Contains(string(data), "要改訂") || strings.Contains(string(data), "変更して") {
		t.Fatal("included commented item")
	}
	if err := saveSpec("fx"); err == nil {
		t.Fatal("overwrote existing spec")
	}
}

func TestSaveSpecAllowsClarificationAndLegacyRoot(t *testing.T) {
	root, _, _ := clarificationFixture(t)
	legacy, err := saveProposal(root, []item{{topic: "旧承認"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	writeReview(t, legacy, []item{{topic: "旧承認", answer: "Approved"}})
	inFixtureRepo(t, root)
	t.Setenv("CLARIFY_OUTPUT", `["追加確認"]`)
	if err := saveSpec("fx"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "prop-review-tmp", "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "- 承認事項") || !strings.Contains(string(data), "- 旧承認") || strings.Contains(string(data), "- 未確認") {
		t.Fatalf("wrong approved items: %s", data)
	}
}

func TestSaveSpecRejectsBrokenReview(t *testing.T) {
	root, original, _ := clarificationFixture(t)
	inFixtureRepo(t, root)
	if err := os.WriteFile(filepath.Join(filepath.Dir(original), "proposal.review.txt"), []byte("bad\nApproval/Comment:Approved\n\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := saveSpec("fx"); err == nil {
		t.Fatal("accepted mismatched review")
	}
	if _, err := os.Stat(filepath.Join(root, "prop-review-tmp", "spec.md")); !os.IsNotExist(err) {
		t.Fatalf("unexpected spec: %v", err)
	}
}
