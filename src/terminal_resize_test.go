package main

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

type resizeInput struct {
	screen *narrowScreen
	widths []int
	keys   []string
}

func (r *resizeInput) Read(p []byte) (int, error) {
	if len(r.keys) == 0 {
		return 0, io.EOF
	}
	r.screen.width = r.widths[0]
	n := copy(p, r.keys[0])
	r.keys[0] = r.keys[0][n:]
	if r.keys[0] == "" {
		r.keys, r.widths = r.keys[1:], r.widths[1:]
	}
	return n, nil
}

func TestInputLineResize(t *testing.T) {
	for _, tc := range []struct {
		name   string
		widths []int
		keys   []string
		want   string
		rows   []string
		col    int
	}{
		{"shrink and edit", []int{12, 8}, []string{"abcdefgh\x1b[D\x1b[D", "Z\x1b[F\x7f\n"}, "abcdefZg", []string{"P: abcde", "fZg"}, 3},
		{"grow and edit", []int{8, 20}, []string{"abcdefgh\x1b[D\x1b[D", "Z\x1b[F\x7f\n"}, "abcdefZg", []string{"P: abcdefZg"}, 11},
		{"Japanese repeated resize", []int{12, 8, 16}, []string{"あいうえ\x1b[D", "先\x1b[F", "\x7f末\n"}, "あいう先末", []string{"P: あ い う 先 末"}, 13},
		{"resize on Enter", []int{12, 8}, []string{"abcdefgh", "\n"}, "abcdefgh", []string{"P: abcde", "fgh"}, 3},
		{"unchanged width", []int{12, 12}, []string{"abc", "d\n"}, "abcd", []string{"P: abcd"}, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			screen := &narrowScreen{width: tc.widths[0]}
			reader := &resizeInput{screen, tc.widths, append([]string(nil), tc.keys...)}
			got, err := inputLine(bufio.NewReader(reader), screen, "P: ", 100)
			if err != nil || got != tc.want {
				t.Fatalf("input = %q, %v", got, err)
			}
			beforeEnter := &narrowScreen{width: screen.width}
			beforeEnter.WriteString(strings.TrimSuffix(screen.String(), "\n"))
			rows, row, col := beforeEnter.state()
			if row != len(tc.rows)-1 || col != tc.col {
				t.Fatalf("cursor = (%d,%d), want (%d,%d); screen %q", row, col, len(tc.rows)-1, tc.col, rows)
			}
			if strings.Join(rows, "\n") != strings.Join(tc.rows, "\n") {
				t.Fatalf("rows = %q, want %q", rows, tc.rows)
			}
			if tc.name == "unchanged width" && strings.Contains(screen.String(), "\x1b[J") {
				t.Fatal("unchanged width must not trigger a redraw")
			}
		})
	}
}
