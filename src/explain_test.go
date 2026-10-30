package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReviewExplanationPreservesDecisions(t *testing.T) {
	items := []item{{topic: "案1"}, {topic: "案2"}}
	var out bytes.Buffer
	var questions []string
	explain := func(index int, question string) (string, error) {
		if index != 0 {
			t.Fatalf("moved to item %d", index)
		}
		questions = append(questions, question)
		if len(questions) == 2 {
			return "", errors.New("offline")
		}
		return "詳しい説明\n具体例", nil
	}
	comment := func(r *bufio.Reader) (string, error) { return readComment(r, &out, nil) }
	err := reviewWithExplanation(items, strings.NewReader("ｅ\n\ne\n理由は？\na\nc\n修正希望\n"), &out, comment, explain)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(questions, []string{"", "理由は？"}) {
		t.Fatal(questions)
	}
	if items[0].answer != "Approved" || items[0].comment || items[1].answer != "修正希望" || !items[1].comment {
		t.Fatal(items)
	}
	for _, text := range []string{"Answer:\n詳しい説明\n具体例", "Explanation failed:", "offline", "e Explain/Ask"} {
		if !strings.Contains(out.String(), text) {
			t.Fatalf("missing %q: %s", text, out.String())
		}
	}
	if strings.Contains(render(items), "詳しい説明") {
		t.Fatal("explanation saved as review")
	}
}

func TestReviewExplanationInterrupted(t *testing.T) {
	called := false
	err := reviewWithExplanation([]item{{topic: "案"}}, strings.NewReader("e\n"), io.Discard, nil, func(int, string) (string, error) { called = true; return "", nil })
	if err == nil || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

func TestExplainProposalFx(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	capture := filepath.Join(t.TempDir(), "prompt")
	t.Setenv("PROMPT_CAPTURE", capture)
	if err := os.WriteFile(filepath.Join(bin, "fx"), []byte("#!/bin/sh\ncat >\"$PROMPT_CAPTURE\"\nprintf '%s' \"$FX_RESPONSE\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	items := []item{{topic: "対象"}, {topic: "別の案"}}
	t.Setenv("FX_RESPONSE", fxJSON(t, "説明\n\n具体例"))
	got, err := explainProposal(t.TempDir(), items, 0, "なぜ？", "fx", io.Discard)
	if err != nil || got != "説明\n\n具体例" {
		t.Fatalf("%q %v", got, err)
	}
	prompt, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{`"target_index":1`, `"question":"なぜ？"`, `"proposals":["対象","別の案"]`, "do not change files"} {
		if !strings.Contains(string(prompt), text) {
			t.Fatalf("missing %q: %s", text, prompt)
		}
	}
	for _, response := range []string{"", "  ", "\x1b[2Jbad", "bad\x00text", "�"} {
		t.Setenv("FX_RESPONSE", fxJSON(t, response))
		if _, err := explainProposal(t.TempDir(), items, 0, "", "fx", io.Discard); err == nil {
			t.Fatalf("accepted %q", response)
		}
	}
	if _, err := explainProposal(t.TempDir(), items, -1, "", "fx", io.Discard); err == nil {
		t.Fatal("accepted invalid index")
	}
}
