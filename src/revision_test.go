package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHolisticRevision(t *testing.T) {
	root, original, promptFile := clarificationFixture(t)
	inFixtureRepo(t, root)
	// More output items than comments, changed pending wording, and reordered topics.
	t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `["新しい構成","未確認の再設計","分割した案","承認事項の言い換え"]`))
	if err := generate(true, "fx", original); err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"元の依頼", "承認事項", "未確認", "変更して", `"approved":["承認事項"]`, "merge, split, remove, add, or reorder", "binding constraints"} {
		if !strings.Contains(string(prompt), want) {
			t.Fatalf("missing %q in %s", want, prompt)
		}
	}
	child, err := latestProposal(root)
	if err != nil {
		t.Fatal(err)
	}
	if marker, err := reconstructionMarker(child); err != nil || !marker {
		t.Fatalf("marker: %v, %v", marker, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(child), "proposal.review.txt")); !os.IsNotExist(err) {
		t.Fatalf("auto-reviewed revision: %v", err)
	}
	// Deletion previews must recognize the new metadata without treating it as an unexpected file.
	if _, _, err := deletionContents(child); err != nil {
		t.Fatal(err)
	}
	writeReview(t, child, []item{{topic: "新しい構成", answer: "さらに整理", comment: true}, {topic: "未確認の再設計"}, {topic: "分割した案"}, {topic: "承認事項の言い換え"}})
	history, request, err := reviewHistory(root, original, true)
	if err != nil || len(history) != 2 || !history[1].restructure {
		t.Fatalf("history: %v, %v", history, err)
	}
	t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `[]`))
	outstanding, _, err := clarificationQuestionsFromHistory(root, history, request, "fx", "test", io.Discard)
	if err != nil || len(outstanding) != 4 {
		t.Fatalf("outstanding: %v, %v", outstanding, err)
	}
	for _, it := range outstanding {
		if it.topic == "未確認" || it.topic == "要改訂" {
			t.Fatalf("superseded topic carried: %s", it.topic)
		}
	}
	// A later reconstruction must retain decisions approved in older generations.
	nextPrompt, err := reconstructionPrompt(root, child)
	if err != nil || !strings.Contains(nextPrompt, `"approved":["承認事項"]`) || !strings.Contains(nextPrompt, "さらに整理") {
		t.Fatalf("later prompt: %s, %v", nextPrompt, err)
	}
	if _, err := reconstructionPrompt(root, original); err == nil {
		t.Fatal("accepted non-tip revision")
	}
}

func TestReconstructionMarkersAndLegacyValidation(t *testing.T) {
	for _, kind := range []string{"legacy", "invalid", "followup", "root"} {
		t.Run(kind, func(t *testing.T) {
			root, original, _ := clarificationFixture(t)
			if kind == "root" {
				if err := os.WriteFile(filepath.Join(filepath.Dir(original), "revision.txt"), []byte("restructure\n"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				// Old unmarked revisions must still satisfy positional/pending validation.
				child, err := saveProposal(root, []item{{topic: "changed"}}, original)
				if err != nil {
					t.Fatal(err)
				}
				writeReview(t, child, []item{{topic: "changed"}})
				if kind != "legacy" {
					marker := "restructure\n"
					if kind == "invalid" {
						marker = "other\n"
					}
					if err := os.WriteFile(filepath.Join(filepath.Dir(child), "revision.txt"), []byte(marker), 0600); err != nil {
						t.Fatal(err)
					}
					if kind == "followup" {
						if err := os.WriteFile(filepath.Join(filepath.Dir(child), "followup.txt"), []byte("followup\n"), 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			if _, _, err := reviewHistory(root, original, true); err == nil {
				t.Fatalf("accepted %s history", kind)
			}
		})
	}
}

func TestReconstructionDoesNotTransferApproval(t *testing.T) {
	root, original, promptFile := clarificationFixture(t)
	child, err := saveProposalWithMode(root, []item{{topic: "承認事項"}, {topic: "コメントの承認文字列"}}, original, "", false, true)
	if err != nil {
		t.Fatal(err)
	}
	writeReview(t, child, []item{{topic: "承認事項"}, {topic: "コメントの承認文字列", answer: "Approved", comment: true}})
	prompt, err := reconstructionPrompt(root, child)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, `"approved":["承認事項"]`) || !strings.Contains(prompt, `"topic":"コメントの承認文字列","comment":"Approved","status":"commented"`) {
		t.Fatalf("comment mistaken for approval: %s", prompt)
	}
	history, request, err := reviewHistory(root, original, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `[]`))
	outstanding, _, err := clarificationQuestionsFromHistory(root, history, request, "fx", "test", io.Discard)
	if err != nil || len(outstanding) != 2 {
		t.Fatalf("current review suppressed by historical approval: %v, %v", outstanding, err)
	}
	captured, err := os.ReadFile(promptFile)
	if err != nil || !strings.Contains(string(captured), `"approved":["承認事項"]`) {
		t.Fatalf("clarify approvals: %s, %v", captured, err)
	}
}
