package main

import (
	"io"
	"strings"
	"testing"
)

func TestReview(t *testing.T) {
	items, err := parse(strings.NewReader("何について：変更する\n承認/コメント：\n\n何について：変更しない\n承認/コメント：\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := review(items, strings.NewReader("承認\nここは修正して\n"), io.Discard); err != nil {
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
	if err := review(items, strings.NewReader("\n"), io.Discard); err != nil {
		t.Fatal(err)
	}
	if items[0].answer != "" {
		t.Fatal("blank answer should remain pending")
	}
	if err := review(items, strings.NewReader(""), io.Discard); err == nil {
		t.Fatal("interrupted review should fail")
	}
	if _, err := parse(strings.NewReader("何について：変更する\n承認/コメント：承認\n")); err == nil {
		t.Fatal("prefilled approval should be rejected")
	}
}
