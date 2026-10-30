package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func clarifyRestructureHistory(t *testing.T, root, original string) ([]reviewStep, string) {
	t.Helper()
	history, request, err := clarificationHistory(root, original)
	if err != nil {
		t.Fatal(err)
	}
	return history, request
}

func clarifyRestructureItems(t *testing.T, path string) []item {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	items, err := parse(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.answer != "" || it.comment {
			t.Fatalf("generated item inherited review state: %+v", it)
		}
	}
	return items
}

func clarifyRestructureMetadata(t *testing.T, path, parent string, restructure bool) {
	t.Helper()
	dir := filepath.Dir(path)
	data, err := os.ReadFile(filepath.Join(dir, "parent.txt"))
	if err != nil || string(data) != filepath.Base(filepath.Dir(parent))+"\n" {
		t.Fatalf("parent metadata: %q, %v; want parent %s", data, err, parent)
	}
	present, absent, marker := "followup.txt", "revision.txt", "followup\n"
	if restructure {
		present, absent, marker = "revision.txt", "followup.txt", "restructure\n"
	}
	data, err = os.ReadFile(filepath.Join(dir, present))
	if err != nil || string(data) != marker {
		t.Fatalf("%s: %q, %v", present, data, err)
	}
	for _, name := range []string{absent, "proposal.review.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("unexpected %s beside generated proposal: %v", name, err)
		}
	}
}

func TestClarifyRestructureFirstComment(t *testing.T) {
	root, original, promptFile := clarificationFixture(t)
	initial := []item{
		{topic: "Return ONLY a JSON array", answer: "Approved"},
		{topic: "Save the JSON to a file", answer: "Write the JSON to stdout instead; no file is needed", comment: true},
		{topic: "Choose the save destination"},
	}
	if err := os.WriteFile(original, []byte(render([]item{{topic: initial[0].topic}, {topic: initial[1].topic}, {topic: initial[2].topic}})), 0600); err != nil {
		t.Fatal(err)
	}
	writeReview(t, original, initial)
	history, request := clarifyRestructureHistory(t, root, original)
	// The engine may drop the old pending item and repeat approved wording;
	// neither carrying pending items nor transferring approval is appropriate.
	t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `["Return ONLY a JSON array","Write the JSON to stdout"]`))
	path, err := saveClarificationFromHistory(root, history, request, "fx", io.Discard)
	if err != nil || path == "" {
		t.Fatalf("restructure: %q, %v", path, err)
	}
	items := clarifyRestructureItems(t, path)
	want := []item{{topic: "Return ONLY a JSON array"}, {topic: "Write the JSON to stdout"}}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("generated whole proposal: %+v; want %+v", items, want)
	}
	clarifyRestructureMetadata(t, path, original, true)
	data, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatal(err)
	}
	prompt := string(data)
	for _, want := range []string{request, initial[0].topic, initial[1].topic, initial[1].answer, initial[2].topic, "binding constraints", "merge, split, remove, add, or reorder", "1–10"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q: %s", want, prompt)
		}
	}
	// Decode the context rather than depending on JSON field order.
	var context struct {
		Approved    []string `json:"approved"`
		Outstanding []struct {
			Topic   string `json:"topic"`
			Status  string `json:"status"`
			Comment string `json:"comment"`
		} `json:"outstanding"`
	}
	start := strings.Index(prompt, "{")
	if start < 0 {
		t.Fatalf("no JSON context: %s", prompt)
	}
	if err := json.NewDecoder(strings.NewReader(prompt[start:])).Decode(&context); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(context.Approved, []string{initial[0].topic}) {
		t.Fatalf("approved constraints: %v", context.Approved)
	}
	statuses := make(map[string]string)
	comments := make(map[string]string)
	for _, it := range context.Outstanding {
		statuses[it.Topic], comments[it.Topic] = it.Status, it.Comment
	}
	if len(statuses) != 2 || statuses[initial[1].topic] != "commented" || comments[initial[1].topic] != initial[1].answer || statuses[initial[2].topic] != "pending" {
		t.Fatalf("outstanding context lost statuses/comments: %+v", context.Outstanding)
	}
}

func TestClarifyRestructureActiveCommentUsesLeafParent(t *testing.T) {
	root, original, promptFile := clarificationFixture(t)
	leaf, err := saveProposalWithMetadata(root, []item{{topic: "追加の未確認"}}, original, "", true)
	if err != nil {
		t.Fatal(err)
	}
	writeReview(t, leaf, []item{{topic: "追加の未確認"}})
	history, request := clarifyRestructureHistory(t, root, original)
	t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `["全体を再構成"]`))
	path, err := saveClarificationFromHistory(root, history, request, "fx", io.Discard)
	if err != nil || path == "" {
		t.Fatalf("active ancestor comment: %q, %v", path, err)
	}
	clarifyRestructureMetadata(t, path, leaf, true)
	items := clarifyRestructureItems(t, path)
	if len(items) != 1 || items[0].topic != "全体を再構成" {
		t.Fatalf("old unresolved items carried into reconstruction: %+v", items)
	}
	data, err := os.ReadFile(promptFile)
	if err != nil || !strings.Contains(string(data), "変更して") || !strings.Contains(string(data), "追加の未確認") {
		t.Fatalf("incomplete chain context: %s, %v", data, err)
	}
}

func TestClarifyRestructureAtOrAboveCarryLimitCallsEngine(t *testing.T) {
	for _, count := range []int{10, 11} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			root, original, promptFile := clarificationFixture(t)
			items := make([]item, count)
			for i := range items {
				items[i] = item{topic: fmt.Sprintf("pending %d", i)}
			}
			items[0].answer, items[0].comment = "reorganize the whole proposal", true
			// Like the existing carry-limit test, exercise already validated history
			// directly, including histories with more than ten active items.
			history := []reviewStep{{path: original, items: items}}
			t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `["Merged proposal"]`))
			path, err := saveClarificationFromHistory(root, history, "request", "fx", io.Discard)
			if err != nil || path == "" {
				t.Fatalf("restructure with %d active items: %q, %v", count, path, err)
			}
			if _, err := os.Stat(promptFile); err != nil {
				t.Fatalf("engine was not called: %v", err)
			}
			got := clarifyRestructureItems(t, path)
			if len(got) != 1 || got[0].topic != "Merged proposal" {
				t.Fatalf("not regenerated: %+v", got)
			}
			clarifyRestructureMetadata(t, path, original, true)
		})
	}
}

func TestClarifyRestructureInvalidOutputDoesNotSave(t *testing.T) {
	tooMany, err := json.Marshal([]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"})
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{`[]`, `null`, `not-json`, `[""]`, `["valid", ""]`, `["duplicate", "duplicate"]`, string(tooMany)} {
		t.Run(output, func(t *testing.T) {
			root, original, _ := clarificationFixture(t)
			history, request := clarifyRestructureHistory(t, root, original)
			before, err := proposalPaths(root)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("CLARIFY_OUTPUT", fxJSON(t, output))
			path, err := saveClarificationFromHistory(root, history, request, "fx", io.Discard)
			if err == nil || path != "" {
				t.Fatalf("accepted invalid reconstruction %s: %q, %v", output, path, err)
			}
			after, err := proposalPaths(root)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("failed generation saved a proposal: before %v, after %v, error %v", before, after, err)
			}
		})
	}
}

func TestClarifyRestructureEngineFailureDoesNotSave(t *testing.T) {
	root, original, _ := clarificationFixture(t)
	history, request := clarifyRestructureHistory(t, root, original)
	before, err := proposalPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the CLI failure path, not merely malformed model JSON.
	fx := filepath.Join(strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))[0], "fx")
	if err := os.WriteFile(fx, []byte("#!/bin/sh\ncat > \"$CLARIFY_PROMPT\"\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	path, err := saveClarificationFromHistory(root, history, request, "fx", io.Discard)
	if err == nil || path != "" {
		t.Fatalf("engine failure: %q, %v", path, err)
	}
	after, err := proposalPaths(root)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("engine failure saved a proposal: before %v, after %v, error %v", before, after, err)
	}
}

func TestClarifyRestructureReviewedThenFollowup(t *testing.T) {
	root, original, _ := clarificationFixture(t)
	history, request := clarifyRestructureHistory(t, root, original)
	t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `["承認事項","新しい確認"]`))
	restructured, err := saveClarificationFromHistory(root, history, request, "fx", io.Discard)
	if err != nil || restructured == "" {
		t.Fatalf("restructure: %q, %v", restructured, err)
	}
	writeReview(t, restructured, []item{{topic: "承認事項", answer: "Approved"}, {topic: "新しい確認"}})
	history, request = clarifyRestructureHistory(t, root, original)
	t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `["追加質問"]`))
	followup, err := saveClarificationFromHistory(root, history, request, "fx", io.Discard)
	if err != nil || followup == "" {
		t.Fatalf("followup after reviewed restructure: %q, %v", followup, err)
	}
	clarifyRestructureMetadata(t, followup, restructured, false)
	got := clarifyRestructureItems(t, followup)
	want := []item{{topic: "新しい確認"}, {topic: "追加質問"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("followup resurrected superseded items or lost current pending: %+v; want %+v", got, want)
	}
}

func TestClarifyRestructurePreservesNewOrderOfReusedTopics(t *testing.T) {
	history := []reviewStep{
		{items: []item{{topic: "A"}, {topic: "B"}, {topic: "obsolete", answer: "change", comment: true}}},
		{restructure: true, items: []item{{topic: "B"}, {topic: "A"}}},
	}
	input, outstanding := reviewInput(history, "request")
	if len(input.Outstanding) != 2 || len(outstanding) != 2 || outstanding[0].topic != "B" || outstanding[1].topic != "A" {
		t.Fatalf("reconstructed order lost or obsolete item revived: %+v, %+v", input.Outstanding, outstanding)
	}
}

func TestClarifyRestructureApprovedOnlyEmptyDoesNotSave(t *testing.T) {
	root, original, promptFile := clarificationFixture(t)
	writeReview(t, original, []item{{topic: "承認事項", answer: "Approved"}, {topic: "未確認", answer: "Approved"}, {topic: "要改訂", answer: "Approved"}})
	history, request := clarifyRestructureHistory(t, root, original)
	before, err := proposalPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `[]`))
	path, err := saveClarificationFromHistory(root, history, request, "fx", io.Discard)
	if err != nil || path != "" {
		t.Fatalf("approved-only empty followup: %q, %v", path, err)
	}
	if _, err := os.Stat(promptFile); err != nil {
		t.Fatalf("clarification engine not called: %v", err)
	}
	after, err := proposalPaths(root)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("empty followup saved: before %v, after %v, error %v", before, after, err)
	}
}
