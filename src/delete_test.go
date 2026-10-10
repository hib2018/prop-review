package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDeleteProposal(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "prop-review-tmp")
	makeProposal := func(name string, reviewed bool) string {
		t.Helper()
		dir := filepath.Join(base, "review."+name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "proposal.txt")
		if err := os.WriteFile(path, []byte(render([]item{{topic: name}, {topic: "Approved topic"}})), 0600); err != nil {
			t.Fatal(err)
		}
		if reviewed {
			result := render([]item{{topic: name, answer: "needs revision", comment: true}, {topic: "Approved topic", answer: "Approved"}})
			if err := os.WriteFile(filepath.Join(dir, "proposal.review.txt"), []byte(result), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return path
	}
	unreviewed := makeProposal("unreviewed", false)
	revisable := makeProposal("revisable", true)
	protected := makeProposal("protected", true)
	malformed := makeProposal("malformed", true)
	if err := os.WriteFile(filepath.Join(filepath.Dir(malformed), "proposal.review.txt"), []byte("invalid result\n"), 0600); err != nil {
		t.Fatal(err)
	}
	child := makeProposal("child", false)
	if err := os.WriteFile(filepath.Join(filepath.Dir(child), "parent.txt"), []byte("review.protected\n"), 0600); err != nil {
		t.Fatal(err)
	}
	paths, err := deletableProposals(root)
	if err != nil || len(paths) != 3 || !strings.Contains(strings.Join(paths, " "), revisable) || strings.Contains(strings.Join(paths, " "), protected) || strings.Contains(strings.Join(paths, " "), malformed) {
		t.Fatalf("deletion candidates: %q, %v", paths, err)
	}
	choice := func(path string) string {
		t.Helper()
		for i, candidate := range paths {
			if candidate == path {
				return strconv.Itoa(i+1) + "\n"
			}
		}
		t.Fatalf("missing candidate %s", path)
		return ""
	}
	for _, input := range []string{"q\n", choice(revisable) + "no\n", choice(revisable)} {
		var output strings.Builder
		if err := deleteProposal(root, strings.NewReader(input), &output); err == nil {
			t.Fatalf("cancel or interruption accepted: %q", input)
		}
		if _, err := os.Stat(revisable); err != nil {
			t.Fatalf("cancel removed review: %v", err)
		}
		if input != "q\n" && (!strings.Contains(output.String(), "Approved topic") || !strings.Contains(output.String(), "Review (including any approvals)")) {
			t.Fatalf("review not shown before confirmation: %q", output.String())
		}
	}
	var output strings.Builder
	if err := deleteProposal(root, strings.NewReader(choice(revisable)+"delete\n"), &output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(revisable)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("review was not deleted: %v", err)
	}
	for _, path := range []string{unreviewed, protected, child} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("other discussion lost %s: %v", path, err)
		}
	}
	paths, err = deletableProposals(root)
	if err != nil {
		t.Fatal(err)
	}
	for i, path := range paths {
		if path == child {
			if err := deleteProposal(root, strings.NewReader(strconv.Itoa(i+1)+"\ndelete\n"), &output); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if _, err := os.Stat(filepath.Dir(child)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("child with parent.txt was not deleted: %v", err)
	}
	paths, err = deletableProposals(root)
	if err != nil || !strings.Contains(strings.Join(paths, " "), protected) {
		t.Fatalf("parent not offered after child deletion: %q, %v", paths, err)
	}
}

func TestDeleteRejectsUnsafeDirectories(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "prop-review-tmp")
	dir := filepath.Join(base, "review.safe")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	proposal := filepath.Join(dir, "proposal.txt")
	if err := os.WriteFile(proposal, []byte("Keep\nApproval/Comment:\n\n"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "proposal.txt")
	if err := os.WriteFile(outside, []byte("Outside\nApproval/Comment:\n\n"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "review.link")
	if err := os.Symlink(filepath.Dir(outside), link); err != nil {
		t.Fatal(err)
	}
	if _, err := deletableProposals(root); err == nil {
		t.Fatal("accepted linked review directory")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(proposal); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, proposal); err != nil {
		t.Fatal(err)
	}
	if _, err := deletableProposals(root); err == nil {
		t.Fatal("accepted linked proposal")
	}
	if err := os.Remove(proposal); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(proposal, []byte("Keep\nApproval/Comment:\n\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := deleteProposal(root, strings.NewReader("1\ndelete\n"), &strings.Builder{}); err == nil {
		t.Fatal("deleted directory with unrelated file")
	}
	for _, path := range []string{proposal, outside, filepath.Join(dir, "notes.txt")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("file lost: %s: %v", path, err)
		}
	}
}
