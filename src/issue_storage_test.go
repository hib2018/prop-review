package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func storageIssue() githubIssue {
	return githubIssue{Number: 42, Title: " Fix display ", Body: "Context\nwith\ttabs", URL: "https://github.com/acme/app/issues/42"}
}

func TestIssueJSONValidation(t *testing.T) {
	valid := storageIssue()
	for _, tc := range []struct {
		name string
		edit func(*githubIssue)
	}{
		{"zero number", func(i *githubIssue) { i.Number = 0 }},
		{"negative number", func(i *githubIssue) { i.Number = -1 }},
		{"blank title", func(i *githubIssue) { i.Title = "  " }},
		{"control title", func(i *githubIssue) { i.Title = "a\nb" }},
		{"invalid title", func(i *githubIssue) { i.Title = "\xff" }},
		{"replacement title", func(i *githubIssue) { i.Title = "\ufffd" }},
		{"blank URL", func(i *githubIssue) { i.URL = " " }},
		{"invalid URL", func(i *githubIssue) { i.URL = "\xff" }},
		{"control URL", func(i *githubIssue) { i.URL += "\n" }},
		{"invalid body", func(i *githubIssue) { i.Body = "\xff" }},
		{"replacement body", func(i *githubIssue) { i.Body = "\ufffd" }},
		{"oversized", func(i *githubIssue) { i.Body = strings.Repeat("x", maxRequestBytes) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issue := valid
			tc.edit(&issue)
			if _, err := issueJSON(issue); err == nil {
				t.Fatal("invalid issue accepted")
			}
			if path, err := saveIssueProposal(t.TempDir(), []item{{topic: "Topic"}}, issue); err == nil || path != "" {
				t.Fatalf("invalid issue saved: %q, %v", path, err)
			}
			if _, err := issueProposals(t.TempDir(), issue, "unused"); err == nil {
				t.Fatal("invalid issue sent for generation")
			}
		})
	}
	valid.Body = ""
	data, err := issueJSON(valid)
	if err != nil {
		t.Fatal(err)
	}
	valid.Body = strings.Repeat("x", maxRequestBytes-len(data))
	if data, err = issueJSON(valid); err != nil || len(data) != maxRequestBytes {
		t.Fatalf("exact limit: %d, %v", len(data), err)
	}
	valid.Body += "x"
	if _, err := issueJSON(valid); err == nil {
		t.Fatal("over limit accepted")
	}
	// JSON escaping, not just raw body length, counts toward the limit.
	valid.Body = strings.Repeat("\x00", maxRequestBytes/6)
	if _, err := issueJSON(valid); err == nil {
		t.Fatal("escaped JSON size not enforced")
	}
}

func TestReadIssueSnapshotStrict(t *testing.T) {
	dir := t.TempDir()
	proposal := filepath.Join(dir, "proposal.txt")
	path := filepath.Join(dir, "issue.json")
	if issue, err := readIssueSnapshot(proposal); err != nil || issue != nil {
		t.Fatalf("missing snapshot: %+v, %v", issue, err)
	}
	valid := `{"number":42,"title":"Title","body":"","url":"https://example.com/42"}`
	for _, data := range []string{
		"", "null", "[]", "{", valid + "{}", valid + " garbage",
		`{"number":42,"title":"Title","url":"url"}`,
		`{"number":42,"title":"Title","body":null,"url":"url"}`,
		`{"number":"42","title":"Title","body":"","url":"url"}`,
		`{"number":42.0,"title":"Title","body":"","url":"url"}`,
		`{"number":999999999999999999999999,"title":"Title","body":"","url":"url"}`,
		`{"number":42,"title":false,"body":"","url":"url"}`,
		`{"number":42,"title":"Title","body":[],"url":"url"}`,
		`{"number":0,"title":"Title","body":"","url":"url"}`,
		`{"number":42,"title":"Title\n","body":"","url":"url"}`,
		`{"number":42,"title":"Title","body":"\ud800","url":"url"}`,
		strings.Replace(valid, `"body":""`, `"body":"`+"\xff"+`"`, 1),
		strings.Replace(valid, `"body":""`, `"body":"","body":"again"`, 1),
		strings.Replace(valid, `"body":""`, `"Body":""`, 1),
		strings.Replace(valid, `"body":""`, `"body":"","extra":1`, 1),
		strings.Repeat(" ", maxRequestBytes+1),
	} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if issue, err := readIssueSnapshot(proposal); err == nil || issue != nil {
			t.Fatalf("invalid snapshot accepted: %.100q, %+v, %v", data, issue, err)
		}
	}
	for _, data := range []string{valid, " \n" + valid + "\n"} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if issue, err := readIssueSnapshot(proposal); err != nil || issue == nil || issue.Body != "" {
			t.Fatalf("empty body rejected: %+v, %v", issue, err)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := readIssueSnapshot(proposal); err == nil {
		t.Fatal("dangling symlink accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := readIssueSnapshot(proposal); err == nil {
		t.Fatal("directory accepted")
	}
}

func initIssueStorageRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if output, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %s, %v", output, err)
	}
	return root
}

func TestIssueSnapshotSaveAndDelete(t *testing.T) {
	root := initIssueStorageRepo(t)
	issue := storageIssue()
	path, err := saveIssueProposal(root, []item{{topic: "Topic"}}, issue)
	if err != nil {
		t.Fatal(err)
	}
	got, err := readIssueSnapshot(path)
	if err != nil || got == nil || *got != issue {
		t.Fatalf("round trip: %+v, %v", got, err)
	}
	request, err := os.ReadFile(filepath.Join(filepath.Dir(path), "request.txt"))
	if err != nil || string(request) != issue.Title {
		t.Fatalf("request title changed: %q, %v", request, err)
	}
	want, _ := issueJSON(issue)
	actual, err := os.ReadFile(filepath.Join(filepath.Dir(path), "issue.json"))
	if err != nil || !bytes.Equal(actual, want) {
		t.Fatalf("snapshot bytes: %q, %v", actual, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "proposal.review.txt")); !os.IsNotExist(err) {
		t.Fatalf("storage unexpectedly reviewed proposal: %v", err)
	}
	var output strings.Builder
	if err := deleteProposal(root, strings.NewReader("1\ny\n"), &output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("snapshot root not removed: %v", err)
	}
	plain, err := saveProposalWithMetadata(root, []item{{topic: "Ordinary"}}, "", "Request", false)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := readIssueSnapshot(plain); err != nil || got != nil {
		t.Fatalf("ordinary caller gained snapshot: %+v, %v", got, err)
	}
	// Both deletion scanning and content validation reject a snapshot symlink.
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(filepath.Dir(plain), "issue.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := deletableProposals(root); err == nil {
		t.Fatal("deletion scan accepted snapshot symlink")
	}
	if _, _, err := deletionContents(plain); err == nil {
		t.Fatal("deletion contents accepted snapshot symlink")
	}
}

func TestIssueSnapshotWriteFailureCleanup(t *testing.T) {
	// Isolate the file-size limit so a real snapshot write fails without affecting
	// the test runner or relying on permissions (which root can bypass).
	if root := os.Getenv("PROP_REVIEW_SNAPSHOT_FAILURE_ROOT"); root != "" {
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 128, Max: 128}); err != nil {
			t.Fatal(err)
		}
		for _, failSnapshot := range []bool{true, false} {
			issue := storageIssue()
			issue.Body = ""
			topic := strings.Repeat("x", 1024)
			if failSnapshot {
				issue.Body = strings.Repeat("x", 1024)
				topic = "Topic"
			}
			data, err := issueJSON(issue)
			if err != nil || (len(data) > 128) != failSnapshot {
				t.Fatalf("invalid failure fixture: %d, %v", len(data), err)
			}
			if path, err := saveIssueProposal(root, []item{{topic: topic}}, issue); err == nil || path != "" {
				t.Fatalf("write failure not returned: %q, %v", path, err)
			}
			entries, err := os.ReadDir(filepath.Join(root, "prop-review-tmp"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("incomplete directory remains: %v, %v", entries, err)
			}
		}
		return
	}
	root := initIssueStorageRepo(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestIssueSnapshotWriteFailureCleanup$")
	cmd.Env = append(os.Environ(), "PROP_REVIEW_SNAPSHOT_FAILURE_ROOT="+root)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failure cleanup subprocess: %s, %v", output, err)
	}
}
