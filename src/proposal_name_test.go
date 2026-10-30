package main

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestIssueProposalNamesAndRequests(t *testing.T) {
	for _, kind := range []string{"saved", "missing request", "mismatched request", "invalid snapshot", "legacy", "legacy missing request"} {
		t.Run(kind, func(t *testing.T) {
			root, original, promptFile, issue := issueHistoryFixture(t)
			inFixtureRepo(t, root)
			requestPath := filepath.Join(filepath.Dir(original), "request.txt")
			want := issue.Title
			switch kind {
			case "missing request", "legacy missing request":
				if err := os.Remove(requestPath); err != nil {
					t.Fatal(err)
				}
			case "mismatched request":
				if err := os.WriteFile(requestPath, []byte("Outdated saved title"), 0600); err != nil {
					t.Fatal(err)
				}
			case "invalid snapshot":
				if err := os.WriteFile(filepath.Join(filepath.Dir(original), "issue.json"), []byte("not JSON"), 0600); err != nil {
					t.Fatal(err)
				}
			case "legacy":
				want = "Legacy request\nAdditional context"
				if err := os.WriteFile(requestPath, []byte(want), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if strings.HasPrefix(kind, "legacy") {
				if err := os.Remove(filepath.Join(filepath.Dir(original), "issue.json")); err != nil {
					t.Fatal(err)
				}
			}
			for _, selectRoot := range []bool{false, true} {
				var menu strings.Builder
				selectFn := selectProposal
				if selectRoot {
					selectFn = selectClarificationRoot
				}
				selected, err := selectFn([]string{original}, strings.NewReader("1\n"), &menu)
				if kind == "invalid snapshot" {
					if err == nil {
						t.Fatal("invalid snapshot accepted by menu")
					}
					continue
				}
				label := strings.SplitN(want, "\n", 2)[0]
				if kind == "legacy missing request" {
					label = "Keep compatibility"
					if selectRoot {
						label = "invalid: missing request.txt"
					}
				}
				if err != nil || selected != original || !strings.Contains(menu.String(), label) || strings.Contains(menu.String(), "Outdated saved title") {
					t.Fatalf("menu: %q, %q, %v", menu.String(), selected, err)
				}
				if kind != "legacy missing request" && strings.Contains(menu.String(), "invalid:") {
					t.Fatalf("valid root marked invalid: %s", menu.String())
				}
			}
			history, request, err := clarificationHistory(root, original)
			if kind == "invalid snapshot" || kind == "legacy missing request" {
				if err == nil {
					t.Fatal("invalid root accepted by clarification")
				}
				return
			}
			if err != nil || request != want || len(history) != 1 {
				t.Fatalf("history: %+v, %q, %v", history, request, err)
			}
			prompt, err := reconstructionPrompt(root, original)
			if err != nil {
				t.Fatal(err)
			}
			var wantIssue *githubIssue
			if kind != "legacy" {
				wantIssue = &issue
			}
			assertIssueHistoryInput(t, prompt, wantIssue, want, 1)
			t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `[]`))
			if _, _, err := clarificationQuestions(root, original, "fx"); err != nil {
				t.Fatal(err)
			}
			assertIssueHistoryInput(t, issueHistoryReadPrompt(t, promptFile), wantIssue, want, 1)
		})
	}
}

func TestIssueProposalNamesFollowParents(t *testing.T) {
	root := initIssueStorageRepo(t)
	inFixtureRepo(t, root)
	issue := storageIssue()
	original, err := saveIssueProposal(root, []item{{topic: "Original topic"}}, issue)
	if err != nil {
		t.Fatal(err)
	}
	child, err := saveProposalWithMode(root, []item{{topic: "Reconstructed topic"}}, original, "", false, true)
	if err != nil {
		t.Fatal(err)
	}
	followup, err := saveProposalWithMetadata(root, []item{{topic: "Independent question"}}, child, "", true)
	if err != nil {
		t.Fatal(err)
	}
	writeReview(t, original, []item{{topic: "Original topic", answer: "Approved"}})
	writeReview(t, child, []item{{topic: "Reconstructed topic", answer: "Approved"}})
	paths := []string{original, child, followup}
	for i, path := range paths {
		var menu strings.Builder
		selected, err := selectProposal(paths, strings.NewReader(strconv.Itoa(i+1)+"\n"), &menu)
		if err != nil || selected != path || strings.Count(menu.String(), issue.Title) != 3 {
			t.Fatalf("linked names: %q, %q, %v", menu.String(), selected, err)
		}
		for _, candidate := range paths {
			if !strings.Contains(menu.String(), displayPath(root, candidate)) {
				t.Fatalf("missing path in menu: %s", menu.String())
			}
		}
		if !strings.Contains(menu.String(), "[awaiting revision]") || !strings.Contains(menu.String(), "[unreviewed]") {
			t.Fatalf("missing status in menu: %s", menu.String())
		}
	}
}

func TestProposalNameRejectsBrokenHistory(t *testing.T) {
	for _, kind := range []string{"cycle", "missing", "invalid parent", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := initIssueStorageRepo(t)
			original, err := saveIssueProposal(root, []item{{topic: "Topic"}}, storageIssue())
			if err != nil {
				t.Fatal(err)
			}
			parent := "review.missing\n"
			switch kind {
			case "cycle":
				parent = filepath.Base(filepath.Dir(original)) + "\n"
			case "invalid parent":
				parent = "../outside\n"
			case "symlink":
				if err := os.Symlink(filepath.Dir(original), filepath.Join(root, "prop-review-tmp", "review.link")); err != nil {
					t.Fatal(err)
				}
				parent = "review.link\n"
			}
			if err := os.WriteFile(filepath.Join(filepath.Dir(original), "parent.txt"), []byte(parent), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := selectProposal([]string{original}, strings.NewReader("1\n"), io.Discard); err == nil {
				t.Fatal("broken history accepted")
			}
		})
	}
}

func TestSpecClarifiesIssueWithoutSavedRequest(t *testing.T) {
	root, original, promptFile, _ := issueHistoryFixture(t)
	inFixtureRepo(t, root)
	if err := os.Remove(filepath.Join(filepath.Dir(original), "request.txt")); err != nil {
		t.Fatal(err)
	}
	specFixtureEngine(t, promptFile, "Approved memo")
	t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `["Additional snapshot question"]`))
	var screen strings.Builder
	if err := saveSpecWithConfirmation("fx", strings.NewReader("y\n"), &screen); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(screen.String(), "Additional questions for") || strings.Contains(screen.String(), "Cannot clarify") || strings.Contains(screen.String(), "Could not check") {
		t.Fatalf("spec did not check issue clarification: %s", screen.String())
	}
}
