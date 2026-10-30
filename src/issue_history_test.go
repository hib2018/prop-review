package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Keep body-only requirements out of generated topics and review comments: later
// prompts can recover them only from the persisted original issue.
func issueHistoryFixture(t *testing.T) (string, string, string, githubIssue) {
	t.Helper()
	root := initIssueStorageRepo(t)
	bin := t.TempDir()
	prompt := filepath.Join(bin, "prompt")
	ghCalls := filepath.Join(bin, "gh-calls")
	for name, script := range map[string]string{
		"fx": "#!/bin/sh\ncat > \"$CLARIFY_PROMPT\"\nprintf '%s\\n' \"$CLARIFY_OUTPUT\"\n",
		"gh": "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$ISSUE_HISTORY_GH_CALLS\"\nprintf '%s\\n' \"$GH_OUTPUT\"\nexit 97\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CLARIFY_PROMPT", prompt)
	t.Setenv("ISSUE_HISTORY_GH_CALLS", ghCalls)
	t.Cleanup(func() {
		if data, err := os.ReadFile(ghCalls); !os.IsNotExist(err) {
			t.Errorf("gh must not be called: %q, %v", data, err)
		}
	})
	issue := githubIssue{
		Number: 731, Title: "Preserve export behavior",
		Body: "Constraint BODY_ONLY_9c2: preserve leading zeroes and never round decimals.\nExample: input 0007,1.2300 => output 0007,1.2300\n日本語の例: 空欄は空欄のまま。",
		URL:  "https://github.com/acme/export/issues/731",
	}
	remote, err := json.Marshal([]githubIssue{issue})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_OUTPUT", string(remote))
	t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `["Keep compatibility","Choose export layout"]`))
	items, err := issueProposals(root, issue, "fx")
	if err != nil {
		t.Fatal(err)
	}
	var first githubIssue
	decodeIssueHistoryPrompt(t, issueHistoryReadPrompt(t, prompt), &first)
	if first != issue {
		t.Fatalf("first issue generation input: %+v; want %+v", first, issue)
	}
	original, err := saveIssueProposal(root, items, issue)
	if err != nil {
		t.Fatal(err)
	}
	writeReview(t, original, []item{{topic: "Keep compatibility", answer: "Approved"}, {topic: "Choose export layout", answer: "Reorganize layout", comment: true}})
	// Simulate a remote edit after the first generation and local snapshot save.
	t.Setenv("GH_OUTPUT", `[{"number":731,"title":"Changed remotely","body":"Removed original constraints","url":"https://example.com/changed"}]`)
	return root, original, prompt, issue
}

func issueHistoryReadPrompt(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func decodeIssueHistoryPrompt(t *testing.T, prompt string, dest interface{}) {
	t.Helper()
	start := strings.Index(prompt, "{")
	if start < 0 {
		t.Fatalf("missing JSON input: %s", prompt)
	}
	if err := json.NewDecoder(strings.NewReader(prompt[start:])).Decode(dest); err != nil {
		t.Fatalf("decode prompt input: %v\n%s", err, prompt)
	}
}

func assertIssueHistoryInput(t *testing.T, prompt string, issue *githubIssue, request string, generations int) {
	t.Helper()
	var input reviewGenerationInput
	decodeIssueHistoryPrompt(t, prompt, &input)
	if input.Request != request || len(input.Reviews) != generations {
		t.Fatalf("request/history changed: %+v", input)
	}
	if issue == nil {
		var fields map[string]json.RawMessage
		decodeIssueHistoryPrompt(t, prompt, &fields)
		if _, exists := fields["original_issue"]; exists || input.Issue != nil {
			t.Fatalf("legacy/manual input must omit original_issue: %s", prompt)
		}
	} else if input.Issue == nil || *input.Issue != *issue {
		t.Fatalf("original issue lost or replaced: %+v; want %+v", input.Issue, issue)
	}
}

func TestIssueHistoryAcrossRevisionAndClarifyGenerations(t *testing.T) {
	root, original, promptFile, issue := issueHistoryFixture(t)
	inFixtureRepo(t, root)
	for _, requireRequest := range []bool{false, true} {
		history, request, err := reviewHistory(root, original, requireRequest)
		if err != nil || len(history) != 1 || history[0].issue == nil || *history[0].issue != issue || request != issue.Title {
			t.Fatalf("initial history (%v): %+v, %q, %v", requireRequest, history, request, err)
		}
	}
	if snapshot, err := readIssueSnapshot(original); err != nil || snapshot == nil || *snapshot != issue {
		t.Fatalf("saved snapshot: %+v, %v", snapshot, err)
	}
	var menu strings.Builder
	selected, err := selectClarificationRoot([]string{original}, strings.NewReader("1\n"), &menu)
	if err != nil || selected != original || !strings.Contains(menu.String(), issue.Title) || strings.Contains(menu.String(), "BODY_ONLY_9c2") || strings.Contains(menu.String(), "0007") {
		t.Fatalf("root label must show title, not body: %q, %v", menu.String(), err)
	}
	menu.Reset()
	if _, err := selectIssue([]githubIssue{issue}, strings.NewReader("731\n"), &menu); err != nil || !strings.Contains(menu.String(), issue.Title) || strings.Contains(menu.String(), "BODY_ONLY_9c2") {
		t.Fatalf("issue label must show title, not body: %q, %v", menu.String(), err)
	}

	latest := original
	// Two full rounds exercise explicit revise, clarify's comment reconstruction,
	// and clarify's ordinary followup, reloading validated history every time.
	for generation := 1; generation <= 6; generation++ {
		prompt, err := reconstructionPrompt(root, latest)
		if err != nil {
			t.Fatal(err)
		}
		assertIssueHistoryInput(t, prompt, &issue, issue.Title, generation)
		topic := fmt.Sprintf("Generation %d decision", generation)
		output, err := json.Marshal([]string{topic})
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("CLARIFY_OUTPUT", fxJSON(t, string(output)))
		var child string
		if generation%3 == 1 {
			if err := generate(true, "fx", latest); err != nil {
				t.Fatal(err)
			}
			child, err = latestProposal(root)
		} else {
			history, request, historyErr := clarificationHistory(root, original)
			if historyErr != nil {
				t.Fatal(historyErr)
			}
			child, err = saveClarificationFromHistory(root, history, request, "fx", io.Discard)
		}
		if err != nil || child == "" {
			t.Fatalf("generation %d: %q, %v", generation, child, err)
		}
		assertIssueHistoryInput(t, issueHistoryReadPrompt(t, promptFile), &issue, issue.Title, generation)
		clarifyRestructureMetadata(t, child, latest, generation%3 != 0)
		if snapshot, err := readIssueSnapshot(child); err != nil || snapshot != nil {
			t.Fatalf("child gained issue snapshot: %+v, %v", snapshot, err)
		}
		items := clarifyRestructureItems(t, child)
		if len(items) != 1 || items[0].topic != topic {
			t.Fatalf("unexpected generated items: %+v", items)
		}
		items[0].answer = "Approved"
		if generation%3 != 2 { // Leave a comment for the next reconstruction.
			items[0].answer, items[0].comment = "Reconsider grouping", true
		}
		writeReview(t, child, items)
		latest = child
		history, request, err := clarificationHistory(root, original)
		if err != nil || len(history) != generation+1 {
			t.Fatalf("reloaded history: %+v, %v", history, err)
		}
		input, _ := reviewInput(history, request)
		if history[0].issue == nil || input.Issue != history[0].issue || *input.Issue != issue {
			t.Fatalf("shared input lost root issue: %+v", input)
		}
		for _, step := range history[1:] {
			if step.issue != nil {
				t.Fatal("issue attached to non-root history step")
			}
		}
	}
}

func TestIssueHistoryManualAndLegacyCompatibility(t *testing.T) {
	for _, kind := range []string{"manual", "legacy-title", "legacy-missing-request"} {
		t.Run(kind, func(t *testing.T) {
			root, original, promptFile, _ := issueHistoryFixture(t)
			if err := os.Remove(filepath.Join(filepath.Dir(original), "issue.json")); err != nil {
				t.Fatal(err)
			}
			request := "Preserve export behavior"
			if kind == "manual" {
				request = "A manual request\nwith additional context"
				if err := os.WriteFile(filepath.Join(filepath.Dir(original), "request.txt"), []byte(request), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "legacy-missing-request" {
				if err := os.Remove(filepath.Join(filepath.Dir(original), "request.txt")); err != nil {
					t.Fatal(err)
				}
				request = ""
				if _, _, err := clarificationHistory(root, original); err == nil || !strings.Contains(err.Error(), "missing request.txt") {
					t.Fatalf("clarify must still require request: %v", err)
				}
			}
			prompt, err := reconstructionPrompt(root, original)
			if err != nil {
				t.Fatal(err)
			}
			assertIssueHistoryInput(t, prompt, nil, request, 1)
			history, gotRequest, err := reviewHistory(root, original, false)
			if err != nil || gotRequest != request || history[0].issue != nil {
				t.Fatalf("legacy history: %+v, %q, %v", history, gotRequest, err)
			}
			if kind == "legacy-missing-request" {
				return
			}
			history, gotRequest, err = clarificationHistory(root, original)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `["Manual reconstruction"]`))
			if path, err := saveClarificationFromHistory(root, history, gotRequest, "fx", io.Discard); err != nil || path == "" {
				t.Fatalf("legacy clarification: %q, %v", path, err)
			}
			assertIssueHistoryInput(t, issueHistoryReadPrompt(t, promptFile), nil, request, 1)
		})
	}
}

func TestIssueHistoryMalformedSnapshotPropagates(t *testing.T) {
	root, original, promptFile, _ := issueHistoryFixture(t)
	if err := os.WriteFile(filepath.Join(filepath.Dir(original), "issue.json"), []byte(`{"number":731,"title":"broken"`), 0600); err != nil {
		t.Fatal(err)
	}
	_, snapshotErr := readIssueSnapshot(original)
	if snapshotErr == nil {
		t.Fatal("malformed snapshot accepted")
	}
	for _, requireRequest := range []bool{false, true} {
		if history, _, err := reviewHistory(root, original, requireRequest); err == nil || err.Error() != snapshotErr.Error() || history != nil {
			t.Fatalf("snapshot error not propagated by history: %+v, %v", history, err)
		}
	}
	if prompt, err := reconstructionPrompt(root, original); err == nil || err.Error() != snapshotErr.Error() || prompt != "" {
		t.Fatalf("snapshot error not propagated by revision: %q, %v", prompt, err)
	}
	before := issueHistoryReadPrompt(t, promptFile)
	if _, _, err := clarificationQuestions(root, original, "fx"); err == nil || err.Error() != snapshotErr.Error() {
		t.Fatalf("snapshot error not propagated by clarify: %v", err)
	}
	if after := issueHistoryReadPrompt(t, promptFile); after != before {
		t.Fatal("clarify called engine despite invalid snapshot")
	}
}

// Existing marker/legacy tests cover the other structural rules. A snapshot must
// not bypass chain validation, and malformed snapshots must not mask those errors.
func TestIssueHistoryValidatesChainBeforeSnapshot(t *testing.T) {
	for _, kind := range []string{"branches", "mismatch", "cycle"} {
		t.Run(kind, func(t *testing.T) {
			root, original, _, _ := issueHistoryFixture(t)
			latest := original
			switch kind {
			case "branches", "mismatch":
				count := 1
				if kind == "branches" {
					count = 2
				}
				for i := 0; i < count; i++ {
					child, err := saveProposal(root, []item{{topic: "Wrong count one"}, {topic: "Wrong count two"}}, original)
					if err != nil {
						t.Fatal(err)
					}
					writeReview(t, child, []item{{topic: "Wrong count one"}, {topic: "Wrong count two"}})
					latest = child
				}
			case "cycle":
				if err := os.WriteFile(filepath.Join(filepath.Dir(original), "parent.txt"), []byte(filepath.Base(filepath.Dir(original))+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			checks := []func() error{
				func() error { _, _, err := reviewHistory(root, original, false); return err },
				func() error { _, _, err := clarificationHistory(root, original); return err },
				func() error { _, err := reconstructionPrompt(root, latest); return err },
			}
			var errorsBefore []string
			for _, check := range checks {
				err := check()
				if err == nil {
					t.Fatalf("accepted %s with valid snapshot", kind)
				}
				errorsBefore = append(errorsBefore, err.Error())
			}
			want := map[string]string{"branches": "ambiguous review branches", "mismatch": "revision item count mismatch", "cycle": "cyclic review history"}[kind]
			if !strings.Contains(errorsBefore[2], want) {
				t.Fatalf("wrong structural failure: %q; want %q", errorsBefore[2], want)
			}
			if err := os.WriteFile(filepath.Join(filepath.Dir(original), "issue.json"), []byte("not JSON"), 0600); err != nil {
				t.Fatal(err)
			}
			for i, check := range checks {
				if err := check(); err == nil || err.Error() != errorsBefore[i] {
					t.Fatalf("snapshot masked structural validation: %v; want %s", err, errorsBefore[i])
				}
			}
		})
	}
}
