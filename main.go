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
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"
)

type item struct {
	topic   string
	answer  string
	comment bool
}

var errCommentBack = errors.New("comment cancelled")
var errBadEncoding = errors.New("invalid UTF-8")

func parse(r io.Reader) ([]item, error) {
	var items []item
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		topic := strings.TrimSpace(strings.TrimPrefix(line, "何について："))
		if !utf8.ValidString(topic) || strings.ContainsRune(topic, utf8.RuneError) {
			return nil, errors.New("invalid UTF-8 in proposal")
		}
		if topic == "" {
			return nil, errors.New("empty topic")
		}
		if !s.Scan() || (strings.TrimSpace(s.Text()) != "承認/コメント：" && strings.TrimSpace(s.Text()) != "承認/コメント:") {
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

func review(items []item, in io.Reader, out io.Writer, comment func(*bufio.Reader, bool) (string, error)) error {
	r := bufio.NewReader(in)
	for i := 0; i < len(items); {
		fmt.Fprintf(out, "\n[%d/%d] %s\na 承認   c コメント   m 複数行   Enter 未確認   b 戻る   q 中断\n> ", i+1, len(items), items[i].topic)
		key, _, err := r.ReadRune()
		if err != nil {
			return fmt.Errorf("review interrupted: %w", err)
		}
		if key == utf8.RuneError {
			fmt.Fprint(out, "\n入力の文字コードを確認してください\n")
			continue
		}
		if key >= 'ａ' && key <= 'ｚ' {
			key -= 'ａ' - 'a'
		}
		switch key {
		case 'a':
			items[i].answer = "承認"
			items[i].comment = false
			i++
		case 'c', 'm':
			answer, err := comment(r, key == 'm')
			if errors.Is(err, errCommentBack) {
				continue
			}
			if err != nil {
				return fmt.Errorf("review interrupted: %w", err)
			}
			items[i].answer = answer
			items[i].comment = answer != ""
			i++
		case '\r', '\n':
			items[i].answer = ""
			items[i].comment = false
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

func inputLine(r *bufio.Reader, out io.Writer, prompt string) (string, error) {
	fmt.Fprint(out, prompt)
	var line string
	badEncoding := false
	for {
		key, _, err := r.ReadRune()
		if err != nil {
			return "", err
		}
		switch key {
		case '\r', '\n':
			fmt.Fprint(out, "\n")
			if badEncoding {
				return "", errBadEncoding
			}
			return line, nil
		case '\b', 127:
			if line != "" {
				_, size := utf8.DecodeLastRuneInString(line)
				line = line[:len(line)-size]
				fmt.Fprintf(out, "\r\x1b[2K%s%s", prompt, line)
			}
		case utf8.RuneError:
			badEncoding = true
		default:
			if key >= ' ' && !badEncoding {
				line += string(key)
				fmt.Fprint(out, string(key))
			}
		}
	}
}

func readComment(r *bufio.Reader, out io.Writer, multiline bool, confirm func(*bufio.Reader) (bool, error)) (string, error) {
	var lines []string
	for {
		prompt := "\nコメント："
		if multiline {
			prompt = "\nコメント（空行で終了）："
		}
		line, err := inputLine(r, out, prompt)
		if errors.Is(err, errBadEncoding) {
			fmt.Fprint(out, "入力の文字コードを確認して、もう一度入力してください\n")
			continue
		}
		if err != nil {
			return "", err
		}
		if line == "" && len(lines) == 0 {
			ok, err := confirm(r)
			if err != nil {
				return "", err
			}
			if !ok {
				return "", errCommentBack
			}
		}
		if !multiline || line == "" {
			return strings.Join(lines, "\n") + line, nil
		}
		lines = append(lines, line)
	}
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
		answer := item.answer
		if item.comment {
			answer = "コメント：" + strconv.Quote(answer)
		}
		fmt.Fprintf(&b, "%s\n承認/コメント：%s\n\n", item.topic, answer)
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
	comment := func(r *bufio.Reader, multiline bool) (string, error) {
		return readComment(r, tty, multiline, func(r *bufio.Reader) (bool, error) {
			fmt.Fprint(tty, "空コメントは未確認扱いになります。Enter=OK  Esc=戻る\n> ")
			for {
				key, _, err := r.ReadRune()
				if err != nil {
					return false, err
				}
				switch key {
				case '\r', '\n':
					return true, nil
				case '\x1b':
					return false, nil
				}
			}
		})
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
