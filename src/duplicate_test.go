package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDuplicateTopicsRejectedAtEveryEntry(t *testing.T) {
	root, original, promptFile := clarificationFixture(t)
	duplicate := "同じ項目\nApproval/Comment:\n\n同じ項目\nApproval/Comment:\n\n"
	if _, err := parse(strings.NewReader(duplicate)); err == nil || !strings.Contains(err.Error(), "duplicate proposal topic") {
		t.Fatalf("accepted duplicate proposal: %v", err)
	}
	t.Setenv("CLARIFY_OUTPUT", `["同じ項目","同じ項目"]`)
	if _, err := proposals(root, "prompt", "fx"); err == nil || !strings.Contains(err.Error(), "duplicate proposal topic") {
		t.Fatalf("accepted duplicate engine output: %v", err)
	}
	before, err := proposalPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saveProposal(root, []item{{topic: "同じ項目"}, {topic: "同じ項目"}}, ""); err == nil {
		t.Fatal("saved duplicate items")
	}
	after, err := proposalPaths(root)
	if err != nil || len(after) != len(before) {
		t.Fatalf("created proposal on rejection: %v, %v", after, err)
	}
	if err := os.WriteFile(original, []byte(duplicate), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(original), "proposal.review.txt"), []byte("同じ項目\nApproval/Comment:Approved\n\n同じ項目\nApproval/Comment:\n\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readReviewedProposal(original); err == nil {
		t.Fatal("accepted duplicate reviewed proposal")
	}
	if _, _, err := clarificationHistory(root, original); err == nil {
		t.Fatal("accepted duplicate in review history")
	}
	if err := os.Remove(promptFile); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLARIFY_OUTPUT", `[]`)
	if _, _, err := clarificationQuestions(root, original, "fx"); err == nil {
		t.Fatal("accepted approved/pending duplicate when engine would return []")
	}
	if _, err := os.Stat(promptFile); !os.IsNotExist(err) {
		t.Fatalf("engine called despite duplicate history: %v", err)
	}
}

func TestRevisionRejectsDuplicateGeneratedItems(t *testing.T) {
	root, original, _ := clarificationFixture(t)
	inFixtureRepo(t, root)
	t.Setenv("CLARIFY_OUTPUT", `["未確認","未確認"]`)
	if err := generate(true, "fx", original); err == nil || !strings.Contains(err.Error(), "duplicate proposal topic") {
		t.Fatalf("accepted duplicate generated items: %v", err)
	}
	paths, err := proposalPaths(root)
	if err != nil || len(paths) != 1 {
		t.Fatalf("created duplicate revision: %v, %v", paths, err)
	}
}
