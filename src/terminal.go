package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

func review(items []item, in io.Reader, out io.Writer, comment func(*bufio.Reader) (string, error)) error {
	r := bufio.NewReader(in)
	showPrompt := true
	for i := 0; i < len(items); {
		if showPrompt {
			fmt.Fprintf(out, "\n[%d/%d] %s\na Approve   c Comment   Enter Skip   b Back   q Quit (press Enter to confirm)\n", i+1, len(items), items[i].topic)
			showPrompt = false
		}
		key, err := inputLine(r, out, "> ", 64)
		if errors.Is(err, errBadEncoding) {
			fmt.Fprint(out, "Invalid text encoding; please try again.\n")
			showPrompt = true
			continue
		}
		if err != nil {
			return fmt.Errorf("review interrupted: %w", err)
		}
		key = strings.TrimSpace(key)
		if len([]rune(key)) == 1 {
			key = strings.Map(func(r rune) rune {
				if r >= 'ａ' && r <= 'ｚ' {
					return r - ('ａ' - 'a')
				}
				return r
			}, key)
		}
		switch key {
		case "a":
			items[i].answer = "Approved"
			items[i].comment = false
			i++
			showPrompt = true
		case "c":
			answer, err := comment(r)
			if errors.Is(err, errCommentBack) {
				showPrompt = true
				continue
			}
			if err != nil {
				return fmt.Errorf("review interrupted: %w", err)
			}
			items[i].answer = answer
			items[i].comment = answer != ""
			i++
			showPrompt = true
		case "":
			items[i].answer = ""
			items[i].comment = false
			i++
			showPrompt = true
		case "b":
			if i > 0 {
				i--
				showPrompt = true
			}
		case "q":
			return errors.New("review cancelled; nothing saved")
		}
	}
	return nil
}

// ponytail: width covers common Japanese/fullwidth/emoji; use a terminal-width library if other scripts need exact cursor placement.
func displayWidth(r rune) int {
	if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) || r >= 0x3000 && r <= 0x303f || r >= 0xff01 && r <= 0xff60 || r >= 0x1f300 && r <= 0x1faff {
		return 2
	}
	return 1
}

func lineWidth(line []rune) int {
	width := 0
	for _, r := range line {
		width += displayWidth(r)
	}
	return width
}

func inputLine(r *bufio.Reader, out io.Writer, prompt string, maxBytes int) (string, error) {
	fmt.Fprint(out, prompt)
	var line []rune
	cursor, bytes := 0, 0
	badEncoding, tooLong := false, false
	type runeResult struct {
		key rune
		err error
	}
	var pending <-chan runeResult
	var queued runeResult
	hasQueued := false
	for {
		var current runeResult
		switch {
		case hasQueued:
			current, hasQueued = queued, false
		case pending != nil:
			current = <-pending
			pending = nil
		default:
			current.key, _, current.err = r.ReadRune()
		}
		if current.err != nil {
			return "", current.err
		}
		key := current.key
		switch key {
		case '\r', '\n':
			fmt.Fprint(out, "\n")
			if badEncoding {
				return "", errBadEncoding
			}
			if tooLong {
				return "", fmt.Errorf("%w (maximum %d bytes)", errInputTooLong, maxBytes)
			}
			return string(line), nil
		case '\x1b':
			// Keep the read pending after a lone Esc so the next character is not lost.
			result := make(chan runeResult, 1)
			go func() {
				key, _, err := r.ReadRune()
				result <- runeResult{key, err}
			}()
			select {
			case queued = <-result:
			case <-time.After(50 * time.Millisecond):
				pending = result
				continue
			}
			// Arrow keys and Home/End are escape sequences in raw terminal mode.
			if queued.err == nil && (queued.key == '[' || queued.key == 'O') {
				if direction, _, err := r.ReadRune(); err == nil {
					if direction >= '0' && direction <= '9' {
						if suffix, _, err := r.ReadRune(); err != nil || suffix != '~' {
							continue
						}
						switch direction {
						case '1', '7':
							direction = 'H'
						case '4', '8':
							direction = 'F'
						}
					}
					switch direction {
					case 'D':
						if cursor > 0 {
							cursor--
							fmt.Fprintf(out, "\x1b[%dD", displayWidth(line[cursor]))
						}
					case 'C':
						if cursor < len(line) {
							fmt.Fprintf(out, "\x1b[%dC", displayWidth(line[cursor]))
							cursor++
						}
					case 'H':
						if cursor > 0 {
							fmt.Fprintf(out, "\x1b[%dD", lineWidth(line[:cursor]))
							cursor = 0
						}
					case 'F':
						if cursor < len(line) {
							fmt.Fprintf(out, "\x1b[%dC", lineWidth(line[cursor:]))
							cursor = len(line)
						}
					}
				}
			} else {
				hasQueued = true
			}
		case '\b', 127:
			if cursor > 0 {
				cursor--
				deleted := displayWidth(line[cursor])
				bytes -= len(string(line[cursor]))
				line = append(line[:cursor], line[cursor+1:]...)
				tail := line[cursor:]
				fmt.Fprintf(out, "\x1b[%dD%s%s\x1b[%dD", deleted, string(tail), strings.Repeat(" ", deleted), lineWidth(tail)+deleted)
			}
		case utf8.RuneError:
			badEncoding = true
		default:
			if key >= ' ' && !badEncoding {
				if bytes+len(string(key)) > maxBytes {
					tooLong = true
					fmt.Fprint(out, "\a")
					continue
				}
				bytes += len(string(key))
				line = append(line, 0)
				copy(line[cursor+1:], line[cursor:])
				line[cursor] = key
				cursor++
				tail := line[cursor:]
				fmt.Fprint(out, string(key), string(tail))
				if width := lineWidth(tail); width > 0 {
					fmt.Fprintf(out, "\x1b[%dD", width)
				}
			}
		}
	}
}

func readComment(r *bufio.Reader, out io.Writer, confirm func(*bufio.Reader) (bool, error)) (string, error) {
	for {
		line, err := inputLine(r, out, "\nComment: ", maxCommentBytes)
		if errors.Is(err, errBadEncoding) || errors.Is(err, errInputTooLong) {
			fmt.Fprintf(out, "%v; please try again.\n", err)
			continue
		}
		if err != nil {
			return "", err
		}
		if line == "" {
			ok, err := confirm(r)
			if err != nil {
				return "", err
			}
			if !ok {
				return "", errCommentBack
			}
		}
		return line, nil
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

func withRawTTY(fn func(*os.File) error) error {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("terminal required: %w", err)
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
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-signals:
			stty(tty, saved)
			os.Exit(130)
		case <-done:
		}
	}()
	return fn(tty)
}
