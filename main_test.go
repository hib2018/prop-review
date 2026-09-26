package main

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

func TestReview(t *testing.T) {
	items, err := parse(strings.NewReader("変更する\n承認/コメント：\n\n変更しない\n承認/コメント：\n"))
	if err != nil {
		t.Fatal(err)
	}
	comment := func(r *bufio.Reader, multiline bool) (string, error) {
		return readComment(r, io.Discard, multiline, nil)
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
	comment := func(r *bufio.Reader, multiline bool) (string, error) {
		return readComment(r, io.Discard, multiline, func(r *bufio.Reader) (bool, error) {
			key, _, err := r.ReadRune()
			return key == '\n', err
		})
	}
	// Empty comment cancelled with Esc; then a multiline comment "承認".
	if err := review(items, strings.NewReader("ｃ\n\x1bm承認\n追記\n\nａ"), io.Discard, comment); err != nil {
		t.Fatal(err)
	}
	want := "変更する\n承認/コメント：コメント：\"承認\\n追記\"\n\n変更しない\n承認/コメント：承認\n\n"
	if got := render(items); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNoImplicitApproval(t *testing.T) {
	items, err := parse(strings.NewReader("変更する\n承認/コメント：\n"))
	if err != nil {
		t.Fatal(err)
	}
	comment := func(*bufio.Reader, bool) (string, error) { return "", nil }
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
	answer, err := readComment(bufio.NewReader(strings.NewReader("文字化け�\n正常\n")), io.Discard, false, nil)
	if err != nil || answer != "正常" {
		t.Fatalf("corrupted comment should be retried: %q, %v", answer, err)
	}
	answer, err = readComment(bufio.NewReader(strings.NewReader("\n\n")), io.Discard, false, func(r *bufio.Reader) (bool, error) {
		key, _, err := r.ReadRune()
		return key == '\n', err
	})
	if err != nil || answer != "" {
		t.Fatalf("empty comment confirmation: %q, %v", answer, err)
	}
	var output strings.Builder
	line, err := inputLine(bufio.NewReader(strings.NewReader("あい\bう\n")), &output, "コメント：")
	if err != nil || line != "あう" || !strings.Contains(output.String(), "\r\x1b[2Kコメント：あ") {
		t.Fatalf("Japanese deletion: %q, %v, %q", line, err, output.String())
	}
}
