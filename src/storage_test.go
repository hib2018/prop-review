package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSaveProposalRequiresActualGitExclusion(t *testing.T) {
	for _, tc := range []struct {
		name, ignore string
	}{
		{"probe only", "!/prop-review-tmp/\n/prop-review-tmp/probe\n!/prop-review-tmp/review.*/\n!/prop-review-tmp/review.*/**\n"},
		{"negation overrides exclude", "!/prop-review-tmp/\n!/prop-review-tmp/**\n"},
		{"request exposed", "!/prop-review-tmp/\n/prop-review-tmp/*\n!/prop-review-tmp/review.*/\n/prop-review-tmp/review.*/*\n!/prop-review-tmp/review.*/request.txt\n"},
		{"result exposed", "!/prop-review-tmp/\n/prop-review-tmp/*\n!/prop-review-tmp/review.*/\n/prop-review-tmp/review.*/*\n!/prop-review-tmp/review.*/proposal.review.txt\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := initTestRepo(t)
			ignore := filepath.Join(root, ".gitignore")
			initTestWrite(t, ignore, tc.ignore)
			initTestGit(t, root, "add", ".gitignore")
			existing := filepath.Join(root, "prop-review-tmp", "review.existing", "proposal.review.txt")
			initTestWrite(t, existing, "existing review")
			before := initTestTree(t, filepath.Dir(existing))
			index := initTestGit(t, root, "ls-files", "--stage")
			for range 2 {
				path, err := saveProposalWithSnapshot(root, []item{{topic: "secret proposal"}}, "", "secret request", false, false, []byte("secret issue"))
				if err == nil || path != "" || !strings.Contains(err.Error(), "prop-review-tmp") || !strings.Contains(err.Error(), "check .gitignore") {
					t.Fatalf("save = %q, %v", path, err)
				}
			}
			entries, err := os.ReadDir(filepath.Join(root, "prop-review-tmp"))
			if err != nil || len(entries) != 1 || entries[0].Name() != "review.existing" {
				t.Fatalf("failed save left content: %v, %v", entries, err)
			}
			if !reflect.DeepEqual(before, initTestTree(t, filepath.Dir(existing))) {
				t.Fatal("existing review changed")
			}
			if got, _ := os.ReadFile(ignore); string(got) != tc.ignore {
				t.Fatal(".gitignore changed")
			}
			if initTestGit(t, root, "ls-files", "--stage") != index {
				t.Fatal("index changed")
			}
			exclude, err := os.ReadFile(filepath.Join(root, ".git", "info", "exclude"))
			if err != nil || strings.Count(string(exclude), "/prop-review-tmp/\n") != 1 {
				t.Fatalf("exclude rule duplicated or missing: %q, %v", exclude, err)
			}
		})
	}
}

func TestSaveReviewRechecksGitExclusion(t *testing.T) {
	for _, mode := range []string{"negated", "tracked", "git failure"} {
		t.Run(mode, func(t *testing.T) {
			root, _ := initTestRepo(t)
			path, err := saveProposalWithSnapshot(root, []item{{topic: "secret"}}, "", "request", false, false, []byte("issue"))
			if err != nil {
				t.Fatal(err)
			}
			result := filepath.Join(filepath.Dir(path), "proposal.review.txt")
			switch mode {
			case "negated":
				initTestWrite(t, filepath.Join(root, ".gitignore"), "!/prop-review-tmp/\n!/prop-review-tmp/**\n")
			case "tracked":
				initTestWrite(t, result, "old tracked review")
				initTestGit(t, root, "add", "-f", result)
				if err := os.Remove(result); err != nil {
					t.Fatal(err)
				}
			case "git failure":
				initTestWrite(t, filepath.Join(root, ".gitignore"), "keep\n")
				bin := t.TempDir()
				initTestWrite(t, filepath.Join(bin, "git"), "#!/bin/sh\necho 'verification failed' >&2\nexit 128\n")
				if err := os.Chmod(filepath.Join(bin, "git"), 0700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			}
			before := initTestTree(t, root)
			err = saveReview(result, []item{{topic: "secret", answer: "private comment", comment: true}})
			if err == nil || !strings.Contains(err.Error(), result) {
				t.Fatalf("saveReview = %v", err)
			}
			if !reflect.DeepEqual(before, initTestTree(t, root)) {
				t.Fatal("failed verification changed files")
			}
		})
	}
}

func TestReviewStorageSuccess(t *testing.T) {
	root, _ := initTestRepo(t)
	var excludeBefore string
	for range 2 {
		path, err := saveProposalWithSnapshot(root, []item{{topic: "proposal"}}, "", "request", false, false, []byte("issue"))
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range reviewFiles {
			initTestGit(t, root, "check-ignore", "-q", filepath.Join(filepath.Dir(path), name))
		}
		result := filepath.Join(filepath.Dir(path), "proposal.review.txt")
		items := []item{{topic: "proposal", answer: "Approved"}}
		if err := saveReview(result, items); err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(result); err != nil || string(got) != render(items) {
			t.Fatalf("result = %q, %v", got, err)
		}
		if err := saveReview(result, []item{{topic: "overwrite"}}); !errors.Is(err, os.ErrExist) {
			t.Fatalf("existing review overwritten: %v", err)
		}
		if got, _ := os.ReadFile(result); string(got) != render(items) {
			t.Fatal("existing review changed")
		}
		exclude, err := os.ReadFile(filepath.Join(root, ".git", "info", "exclude"))
		if err != nil || strings.Count(string(exclude), "/prop-review-tmp/\n") != 1 {
			t.Fatalf("exclude = %q, %v", exclude, err)
		}
		if excludeBefore != "" && excludeBefore != string(exclude) {
			t.Fatal("exclude changed on second save")
		}
		excludeBefore = string(exclude)
	}
}

func TestSaveProposalStopsOnGitFailure(t *testing.T) {
	root, _ := initTestRepo(t)
	bin := t.TempDir()
	initTestWrite(t, filepath.Join(bin, "git"), "#!/bin/sh\necho 'verification failed' >&2\nexit 128\n")
	if err := os.Chmod(filepath.Join(bin, "git"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	exclude := filepath.Join(root, ".git", "info", "exclude")
	before := initTestTree(t, exclude)
	if _, err := saveProposalWithMetadata(root, []item{{topic: "secret"}}, "", "private request", false); err == nil || !strings.Contains(err.Error(), "verification failed") {
		t.Fatalf("save = %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "prop-review-tmp"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed save left content: %v, %v", entries, err)
	}
	if !reflect.DeepEqual(before, initTestTree(t, exclude)) {
		t.Fatal("Git failure changed exclude")
	}
}

func TestSaveCustomReviewOutsideGit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.review.txt")
	if err := saveReview(path, []item{{topic: "custom", answer: "Approved"}}); err != nil {
		t.Fatal(err)
	}
}
