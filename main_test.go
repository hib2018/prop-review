package main

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLatestProposal(t *testing.T) {
	root := t.TempDir()
	makeProposal := func(name string, when time.Time) string {
		t.Helper()
		dir := filepath.Join(root, "prop-review-tmp", name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "proposal.txt")
		if err := os.WriteFile(path, []byte("変更する\n承認/コメント：\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if _, err := latestProposal(root); err == nil {
		t.Fatal("empty directory should fail")
	}
	older := makeProposal("review.old", time.Now().Add(-time.Hour))
	newer := makeProposal("review.new", time.Now())
	if got, err := latestProposal(root); err != nil || got != newer {
		t.Fatalf("latest: %q, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(newer), "proposal.review.txt"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := latestProposal(root); err != nil || got != older {
		t.Fatalf("skip reviewed: %q, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(older), "proposal.review.txt"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := latestProposal(root); err == nil {
		t.Fatal("all reviewed should fail")
	}
}

func TestReview(t *testing.T) {
	items, err := parse(strings.NewReader("変更する\n承認/コメント：\n\n変更しない\n承認/コメント：\n"))
	if err != nil {
		t.Fatal(err)
	}
	comment := func(r *bufio.Reader) (string, error) {
		return readComment(r, io.Discard, nil)
	}
	// Approve, go back, reapprove, then comment on the next item.
	if err := review(items, strings.NewReader("abacここは修正して\n"), io.Discard, comment); err != nil {
		t.Fatal(err)
	}
	want := "変更する\n承認/コメント：承認\n\n変更しない\n承認/コメント：コメント：\"ここは修正して\"\n\n"
	if got := render(items); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCommentAndFullwidthKeys(t *testing.T) {
	items, err := parse(strings.NewReader("変更する\n承認/コメント:\n\n変更しない\n承認/コメント：\n"))
	if err != nil {
		t.Fatal(err)
	}
	comment := func(r *bufio.Reader) (string, error) {
		return readComment(r, io.Discard, func(r *bufio.Reader) (bool, error) {
			key, _, err := r.ReadRune()
			return key == '\n', err
		})
	}
	// Empty comment cancelled with Esc; then comment "承認" without approving.
	if err := review(items, strings.NewReader("ｃ\n\x1bc承認\nａ"), io.Discard, comment); err != nil {
		t.Fatal(err)
	}
	want := "変更する\n承認/コメント：コメント：\"承認\"\n\n変更しない\n承認/コメント：承認\n\n"
	if got := render(items); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNoImplicitApproval(t *testing.T) {
	items, err := parse(strings.NewReader("変更する\n承認/コメント：\n"))
	if err != nil {
		t.Fatal(err)
	}
	comment := func(*bufio.Reader) (string, error) { return "", nil }
	if err := review(items, strings.NewReader("\n"), io.Discard, comment); err != nil {
		t.Fatal(err)
	}
	if legacy, err := parse(strings.NewReader("何について：変更する\n承認/コメント：\n")); err != nil || legacy[0].topic != "変更する" {
		t.Fatalf("legacy proposal: %v, %v", legacy, err)
	}
	if items[0].answer != "" {
		t.Fatal("blank answer should remain pending")
	}
	if err := review(items, strings.NewReader(""), io.Discard, comment); err == nil {
		t.Fatal("interrupted review should fail")
	}
	if err := review(items, strings.NewReader("q"), io.Discard, comment); err == nil {
		t.Fatal("cancelled review should fail")
	}
	if _, err := parse(strings.NewReader("変更する\n承認/コメント：承認\n")); err == nil {
		t.Fatal("prefilled approval should be rejected")
	}
	for _, topic := range []string{"\xff", "文字化け�"} {
		if _, err := parse(strings.NewReader(topic + "\n承認/コメント：\n")); err == nil {
			t.Fatal("corrupted topic should be rejected")
		}
	}
	answer, err := readComment(bufio.NewReader(strings.NewReader("文字化け�\n正常\n")), io.Discard, nil)
	if err != nil || answer != "正常" {
		t.Fatalf("corrupted comment should be retried: %q, %v", answer, err)
	}
	answer, err = readComment(bufio.NewReader(strings.NewReader("\n\n")), io.Discard, func(r *bufio.Reader) (bool, error) {
		key, _, err := r.ReadRune()
		return key == '\n', err
	})
	if err != nil || answer != "" {
		t.Fatalf("empty comment confirmation: %q, %v", answer, err)
	}
	var output strings.Builder
	line, err := inputLine(bufio.NewReader(strings.NewReader("あい\bう\n")), &output, "コメント：")
	if err != nil || line != "あう" || !strings.Contains(output.String(), "\x1b[2D\x1b[0K") || strings.Contains(output.String(), "\r") {
		t.Fatalf("Japanese deletion: %q, %v, %q", line, err, output.String())
	}
	var menu strings.Builder
	if err := review([]item{{topic: "test"}}, strings.NewReader("xyzq"), &menu, comment); err == nil {
		t.Fatal("q should cancel")
	}
	if strings.Count(menu.String(), "[1/1]") != 1 {
		t.Fatalf("invalid keys should not scroll menu: %q", menu.String())
	}
}
