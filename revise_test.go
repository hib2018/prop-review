package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fxEnvelope(t *testing.T, output string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"final_output": output, "session_id": "", "exit_code": 0})
	if err != nil { t.Fatal(err) }
	return b
}

func TestParseReview(t *testing.T) {
	original := []item{{topic: "承認済み"}, {topic: "コメント対象"}, {topic: "未確認"}}
	valid := "承認済み\n承認/コメント：承認\n\nコメント対象\n承認/コメント：コメント：\"別の案にして\\n理由も含めて\"\n\n未確認\n承認/コメント:\n"
	got, err := parseReview([]byte(valid), original)
	if err != nil || len(got) != 3 || !got[1].comment || got[1].answer != "別の案にして\n理由も含めて" || got[2].answer != "" { t.Fatalf("parse review: %#v, %v", got, err) }
	for _, text := range []string{
		strings.Replace(valid, "承認済み", "違う項目", 1),
		strings.Replace(valid, "コメント：\"別の案にして\\n理由も含めて\"", "勝手に承認", 1),
		strings.Replace(valid, "コメント：\"別の案にして\\n理由も含めて\"", "コメント：\"\"", 1),
		"承認済み\n承認/コメント：承認\n",
		valid + "余分\n承認/コメント：\n",
		strings.Replace(valid, "別の案", "�", 1),
		strings.Replace(valid, "別の案", "\\xff", 1),
	} { if _, err := parseReview([]byte(text), original); err == nil { t.Fatalf("accepted bad review: %q", text) } }
}

func TestRevisionPromptAndValidation(t *testing.T) {
	source := []item{{topic: "保持する", answer: "承認"}, {topic: "変える", answer: "もっと小さく", comment: true}, {topic: "まだ決めない"}}
	prompt, err := revisionPrompt(source)
	if err != nil || !strings.Contains(prompt, `"status":"approved"`) || !strings.Contains(prompt, `"comment":"もっと小さく"`) { t.Fatalf("prompt: %q, %v", prompt, err) }
	if _, err := revisionPrompt(source[:1]); err == nil { t.Fatal("approval is not a revision request") }
	output := `{"items":[{"source_index":1,"topic":"小さく変更する"}]}`
	got, err := revisedItems(fxEnvelope(t, output), source)
	if err != nil || render(got) != "小さく変更する\n承認/コメント：\n\nまだ決めない\n承認/コメント：\n\n" { t.Fatalf("revision: %#v, %v", got, err) }
	for _, bad := range []string{
		`{"items":[]}`,
		`{"items":[{"source_index":0,"topic":"承認済みを変える"}]}`,
		`{"items":[{"source_index":2,"topic":"未確認を変える"}]}`,
		`{"items":[{"source_index":1,"topic":"改訂"},{"source_index":1,"topic":"重複"}]}`,
		`{"items":[{"topic":"番号なし"}]}`,
		`{"items":[{"source_index":1,"topic":""}]}`,
		`{"items":[{"source_index":1,"topic":"提案\n承認/コメント：承認"}]}`,
		`{"items":[{"source_index":1,"topic":"提案","approval":true}]}`,
		"```json\n" + output + "\n```",
		output + " {}",
	} { if _, err := revisedItems(fxEnvelope(t, bad), source); err == nil { t.Fatalf("accepted invalid output: %s", bad) } }
	for _, bad := range []string{
		`{"final_output":"{}","session_id":"saved","exit_code":0}`,
		`{"final_output":"{}","session_id":"","exit_code":1}`,
		`{"final_output":"{}","session_id":"","error":"provider failed"}`,
		`{"final_output":"{}","session_id":"","interrupted":true}`,
		`not json`,
	} { if _, err := revisedItems([]byte(bad), source); err == nil { t.Fatalf("accepted failed envelope: %s", bad) } }
}

func initReviewRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if b, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil { t.Fatalf("git init: %s, %v", b, err) }
	return root
}

func TestSaveRevisionRetainsHistory(t *testing.T) {
	root := initReviewRepo(t)
	oldDir := filepath.Join(root, "prop-review-tmp", "review.original")
	if err := os.MkdirAll(oldDir, 0700); err != nil { t.Fatal(err) }
	old := filepath.Join(oldDir, "proposal.review.txt")
	if err := os.WriteFile(old, []byte("original review"), 0600); err != nil { t.Fatal(err) }
	newPath, err := saveRevision(root, old, []item{{topic: "改訂"}})
	if err != nil { t.Fatal(err) }
	if b, err := os.ReadFile(old); err != nil || string(b) != "original review" { t.Fatalf("history changed: %q, %v", b, err) }
	if b, err := os.ReadFile(newPath); err != nil || string(b) != "改訂\n承認/コメント：\n\n" { t.Fatalf("new proposal: %q, %v", b, err) }
	if b, err := os.ReadFile(filepath.Join(filepath.Dir(newPath), "source.txt")); err != nil || strings.TrimSpace(string(b)) != old { t.Fatalf("source: %q, %v", b, err) }
	if got, err := latestProposal(root); err != nil || got != newPath { t.Fatalf("latest: %q, %v", got, err) }
	if _, err := latestReview(root); err == nil { t.Fatal("must not reuse an older review when the new proposal is pending") }
	if err := os.WriteFile(strings.TrimSuffix(newPath, ".txt")+".review.txt", []byte("reviewed"), 0600); err != nil { t.Fatal(err) }
	if got, err := latestReview(root); err != nil || got != strings.TrimSuffix(newPath, ".txt")+".review.txt" { t.Fatalf("latest review: %q, %v", got, err) }
	if err := excludeReviewDir(root); err != nil { t.Fatal(err) }
	b, err := os.ReadFile(filepath.Join(root, ".git", "info", "exclude"))
	if err != nil || strings.Count(string(b), "/prop-review-tmp/") != 1 { t.Fatalf("duplicate exclude: %q, %v", b, err) }
}

func TestReadAndOutputLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.txt")
	if err := os.WriteFile(path, make([]byte, maxReviewBytes+1), 0600); err != nil { t.Fatal(err) }
	if _, err := readLimited(path); err == nil { t.Fatal("oversized review accepted") }
	b := &limitedBuffer{limit: 4}
	if _, err := b.Write([]byte("1234")); err != nil { t.Fatal(err) }
	if _, err := b.Write([]byte("5")); err == nil { t.Fatal("oversized fx output accepted") }
}

// Use the test binary as a fake fx executable; no model requests or API keys.
func init() {
	if os.Getenv("PROP_REVIEW_TEST_FX") != "" { fakeFx() }
}

func fakeFx() {
	mode := os.Getenv("PROP_REVIEW_TEST_FX")
	if mode == "" { return }
	if len(os.Args) < 4 || os.Args[1] != "ask" || os.Args[2] != "--json" || os.Args[3] != "--no-save" || os.Getenv("FX_PERMISSION_MODE") != "ask" { os.Exit(7) }
	if mode == "hang" { time.Sleep(time.Minute); os.Exit(0) }
	if mode == "fail" { os.Stderr.WriteString("credential-sentinel"); os.Exit(9) }
	if mode == "large" { os.Stdout.WriteString(strings.Repeat("x", maxFxBytes+1)); os.Exit(0) }
	if len(os.Args) != 6 || os.Args[4] != "--model" || os.Args[5] != "test/model" { os.Exit(8) }
	b, err := io.ReadAll(os.Stdin)
	if err != nil || !strings.Contains(string(b), "commented") { os.Exit(10) }
	dir, err := os.Getwd()
	if err != nil || !strings.HasPrefix(filepath.Base(dir), "prop-review-fx-") { os.Exit(11) }
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 { os.Exit(12) }
	out, _ := json.Marshal(map[string]any{"final_output": `{"items":[{"source_index":0,"topic":"改訂する"}]}`, "session_id": "", "exit_code": 0})
	os.Stdout.Write(out)
	os.Exit(0)
}

func TestAskFx(t *testing.T) {
	bin, err := os.Executable()
	if err != nil { t.Fatal(err) }
	t.Setenv("PROP_REVIEW_FX_BIN", bin)
	t.Setenv("PROP_REVIEW_TEST_FX", "success")
	t.Setenv("FX_PERMISSION_MODE", "yolo")
	b, err := askFx(context.Background(), "test/model", "commented")
	if err != nil { t.Fatal(err) }
	if _, err := revisedItems(b, []item{{topic: "元", answer: "変えて", comment: true}}); err != nil { t.Fatal(err) }
	t.Setenv("PROP_REVIEW_TEST_FX", "fail")
	if _, err := askFx(context.Background(), "test/model", "commented"); err == nil || strings.Contains(err.Error(), "credential-sentinel") { t.Fatalf("failure: %v", err) }
	t.Setenv("PROP_REVIEW_TEST_FX", "large")
	if _, err := askFx(context.Background(), "test/model", "commented"); err == nil { t.Fatal("oversized child output accepted") }
	t.Setenv("PROP_REVIEW_TEST_FX", "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := askFx(ctx, "test/model", "commented"); !errors.Is(err, context.DeadlineExceeded) { t.Fatalf("timeout: %v", err) }
}

func TestRunRevise(t *testing.T) {
	root := initReviewRepo(t)
	t.Chdir(root)
	bin, err := os.Executable()
	if err != nil { t.Fatal(err) }
	t.Setenv("PROP_REVIEW_FX_BIN", bin)
	t.Setenv("PROP_REVIEW_TEST_FX", "success")
	dir := filepath.Join(root, "prop-review-tmp", "review.original")
	if err := os.MkdirAll(dir, 0700); err != nil { t.Fatal(err) }
	proposal := "元\n承認/コメント：\n\n未確認\n承認/コメント：\n\n承認済み\n承認/コメント：\n"
	review := "元\n承認/コメント：コメント：\"変えて\"\n\n未確認\n承認/コメント：\n\n承認済み\n承認/コメント：承認\n"
	path := filepath.Join(dir, "proposal.txt")
	reviewPath := filepath.Join(dir, "proposal.review.txt")
	if err := os.WriteFile(path, []byte(proposal), 0600); err != nil { t.Fatal(err) }
	if err := os.WriteFile(reviewPath, []byte(review), 0600); err != nil { t.Fatal(err) }
	if err := run([]string{"revise", "--model", "test/model"}); err != nil { t.Fatal(err) }
	latest, err := latestProposal(root)
	if err != nil || latest == path { t.Fatalf("new proposal: %q, %v", latest, err) }
	b, err := os.ReadFile(latest)
	if err != nil || string(b) != "改訂する\n承認/コメント：\n\n未確認\n承認/コメント：\n\n" { t.Fatalf("new contents: %q, %v", b, err) }
	if b, err := os.ReadFile(reviewPath); err != nil || string(b) != review { t.Fatalf("original review changed: %q, %v", b, err) }
	if b, err := os.ReadFile(path); err != nil || string(b) != proposal { t.Fatalf("original proposal changed: %q, %v", b, err) }
	if err := run([]string{"revise", "--model", "test/model"}); err == nil { t.Fatal("pending proposal must be reviewed first") }
	t.Setenv("PROP_REVIEW_TEST_FX", "fail")
	if err := run([]string{"revise", "--model", "test/model", reviewPath}); err == nil { t.Fatal("fx failure accepted") }
	paths, err := filepath.Glob(filepath.Join(root, "prop-review-tmp", "review.*"))
	if err != nil || len(paths) != 2 { t.Fatalf("failure created a proposal: %v, %v", paths, err) }
	t.Setenv("PROP_REVIEW_FX_BIN", filepath.Join(root, "missing-fx"))
	if err := run([]string{"revise", reviewPath}); err == nil { t.Fatal("missing fx accepted") }
}
