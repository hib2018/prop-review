package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
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

func review(items []item, in io.Reader, out io.Writer, comment func(*bufio.Reader) (string, error)) error {
	r := bufio.NewReader(in)
	for i := 0; i < len(items); {
		fmt.Fprintf(out, "\n[%d/%d] %s\na 承認   c コメント   Enter 未確認   b 戻る   q 中断\n> ", i+1, len(items), items[i].topic)
		key, err := r.ReadByte()
		if err != nil {
			return fmt.Errorf("review interrupted: %w", err)
		}
		switch key {
		case 'a':
			items[i].answer = "承認"
			i++
		case 'c':
			answer, err := comment(r)
			if err != nil {
				return fmt.Errorf("review interrupted: %w", err)
			}
			items[i].answer = answer
			i++
		case '\r', '\n':
			items[i].answer = ""
			i++
		case 'b':
			if i > 0 {
				i--
			}
		case 'q':
			return errors.New("review cancelled; nothing saved")
		}
	}
	return nil
}

func stty(tty *os.File, args ...string) (string, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = tty
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("stty %v: %s: %w", args, strings.TrimSpace(string(output)), err)
	}
	return strings.TrimSpace(string(output)), nil
}

func render(items []item) string {
	var b strings.Builder
	for _, item := range items {
		fmt.Fprintf(&b, "%s\n承認/コメント：%s\n\n", item.topic, item.answer)
	}
	return b.String()
}

func run(args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return errors.New("usage: prop-review proposal.txt [result.txt]")
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
	if _, err := os.Stat(output); err == nil {
		return fmt.Errorf("result already exists: %s", output)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("human review requires a terminal: %w", err)
	}
	defer tty.Close()
	saved, err := stty(tty, "-g")
	if err != nil {
		return err
	}
	if _, err := stty(tty, "-echo", "-icanon", "min", "1", "time", "0"); err != nil {
		return err
	}
	defer stty(tty, saved)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		<-signals
		stty(tty, saved)
		os.Exit(130)
	}()
	comment := func(r *bufio.Reader) (string, error) {
		if _, err := stty(tty, saved); err != nil {
			return "", err
		}
		fmt.Fprint(tty, "\nコメント：")
		line, err := r.ReadString('\n')
		if err != nil {
			return "", err
		}
		if _, err := stty(tty, "-echo", "-icanon", "min", "1", "time", "0"); err != nil {
			return "", err
		}
		return strings.TrimSpace(line), nil
	}
	if err := review(items, tty, tty, comment); err != nil {
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
