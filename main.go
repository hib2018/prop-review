package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type item struct {
	topic  string
	answer string
}

func parse(r io.Reader) ([]item, error) {
	var items []item
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "何について：") {
			return nil, fmt.Errorf("expected 何について：, got %q", line)
		}
		topic := strings.TrimSpace(strings.TrimPrefix(line, "何について："))
		if topic == "" {
			return nil, errors.New("empty topic")
		}
		if !s.Scan() || strings.TrimSpace(s.Text()) != "承認/コメント：" {
			return nil, fmt.Errorf("%q: expected empty 承認/コメント： line", topic)
		}
		items = append(items, item{topic: topic})
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("no items")
	}
	return items, nil
}

func review(items []item, in io.Reader, out io.Writer) error {
	r := bufio.NewReader(in)
	for i := range items {
		fmt.Fprintf(out, "何について：%s\n承認 / コメント（空欄は未確認）：", items[i].topic)
		line, err := r.ReadString('\n')
		if err != nil {
			return fmt.Errorf("review interrupted: %w", err)
		}
		items[i].answer = strings.TrimSpace(line)
	}
	return nil
}

func render(items []item) string {
	var b strings.Builder
	for _, item := range items {
		fmt.Fprintf(&b, "何について：%s\n承認/コメント：%s\n\n", item.topic, item.answer)
	}
	return b.String()
}

func run(args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return errors.New("usage: zcomment proposal.txt [result.txt]")
	}
	input, err := os.Open(args[0])
	if err != nil {
		return err
	}
	defer input.Close()
	items, err := parse(input)
	if err != nil {
		return err
	}
	output := strings.TrimSuffix(args[0], filepath.Ext(args[0])) + ".review.txt"
	if len(args) == 2 {
		output = args[1]
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("human review requires a terminal: %w", err)
	}
	defer tty.Close()
	if err := review(items, tty, tty); err != nil {
		return err
	}
	f, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = io.WriteString(f, render(items)); err != nil {
		f.Close()
		os.Remove(output)
		return err
	}
	if err = f.Close(); err != nil {
		os.Remove(output)
		return err
	}
	fmt.Println(output)
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
