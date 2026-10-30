package main

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Opt-in integration exercise: model output needs human semantic review.
// PROP_REVIEW_LIVE_ENGINES=pi,fx go test ./src -run '^TestClarifyLiveEngines$' -v -count=1
func TestClarifyLiveEngines(t *testing.T) {
	engines := os.Getenv("PROP_REVIEW_LIVE_ENGINES")
	if engines == "" {
		t.Skip("set PROP_REVIEW_LIVE_ENGINES=pi,fx to exercise real engines")
	}
	for _, engine := range strings.Split(engines, ",") {
		t.Run(engine, func(t *testing.T) {
			if !validEngine(engine) {
				t.Fatalf("invalid engine %q", engine)
			}
			root := t.TempDir()
			if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
				t.Fatalf("git init: %s: %v", output, err)
			}
			initial := []item{{topic: "出力形式はJSONにする"}, {topic: "結果をファイルに保存する"}, {topic: "保存先を設定できるようにする"}}
			original, err := saveProposalWithMetadata(root, initial, "", "JSON形式で結果を出力するCLIの出力方法を整理する", false)
			if err != nil {
				t.Fatal(err)
			}
			initial[0].answer = "Approved"
			initial[1].answer, initial[1].comment = "ファイル保存は不要。結果を標準出力に出してほしい", true
			writeReview(t, original, initial)
			history, request, err := clarificationHistory(root, original)
			if err != nil {
				t.Fatal(err)
			}
			path, err := saveClarificationFromHistory(root, history, request, engine, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			items := clarifyRestructureItems(t, path)
			clarifyRestructureMetadata(t, path, original, true)
			t.Logf("%s generated unapproved proposal (human check: JSON preserved, stdout replaces files, save destination no longer needed):\n%s", engine, render(items))
		})
	}
}
