package main

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

func TestReview(t *testing.T) {
	items, err := parse(strings.NewReader("何について：変更する\n承認/コメント：\n\n何について：変更しない\n承認/コメント：\n"))
	if err != nil {
		t.Fatal(err)
	}
	comment := func(r *bufio.Reader) (string, error) {
		line, err := r.ReadString('\n')
		return strings.TrimSpace(line), err
	}
	// Approve, go back, reapprove, then comment on the next item.
	if err := review(items, strings.NewReader("abacここは修正して\n"), io.Discard, comment); err != nil {
		t.Fatal(err)
	}
	want := "何について：変更する\n承認/コメント：承認\n\n何について：変更しない\n承認/コメント：ここは修正して\n\n"
	if got := render(items); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNoImplicitApproval(t *testing.T) {
	items, err := parse(strings.NewReader("何について：変更する\n承認/コメント：\n"))
	if err != nil {
		t.Fatal(err)
	}
	comment := func(*bufio.Reader) (string, error) { return "", nil }
	if err := review(items, strings.NewReader("\n"), io.Discard, comment); err != nil {
		t.Fatal(err)
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
	if _, err := parse(strings.NewReader("何について：変更する\n承認/コメント：承認\n")); err == nil {
		t.Fatal("prefilled approval should be rejected")
	}
}
