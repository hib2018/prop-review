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

// visualPosition includes the prompt so edits remain correct across wrapped rows.
func visualPosition(text []rune, width int) (row, col int) {
	for _, ch := range text {
		w := displayWidth(ch)
		if col+w > width {
			row++
			col = 0
		}
		col += w
		if col == width {
			row++
			col = 0
		}
	}
	return
}

func moveCursor(out io.Writer, fromRow, toRow, toCol int) {
	if fromRow > toRow {
		fmt.Fprintf(out, "\x1b[%dA", fromRow-toRow)
	} else if fromRow < toRow {
		fmt.Fprintf(out, "\x1b[%dB", toRow-fromRow)
	}
	fmt.Fprint(out, "\r")
	if toCol > 0 {
		fmt.Fprintf(out, "\x1b[%dC", toCol)
	}
}

func drawRunes(out io.Writer, text []rune, width int, col int) {
	for _, ch := range text {
		w := displayWidth(ch)
		if col+w > width {
			fmt.Fprint(out, "\r\n")
			col = 0
		}
		fmt.Fprint(out, string(ch))
		col += w
		if col == width {
			fmt.Fprint(out, "\r\n")
			col = 0
		}
	}
}

func inputLine(r *bufio.Reader, out io.Writer, prompt string, maxBytes int) (string, error) {
	width := 1 << 30 // Non-terminal writers do not have a screen edge.
	if tty, ok := out.(*os.File); ok {
		if size, err := stty(tty, "size"); err == nil {
			var rows, columns int
			if _, err := fmt.Sscan(size, &rows, &columns); err == nil && columns > 0 {
				width = columns
			}
		}
	}
	if screen, ok := out.(interface{ terminalWidth() int }); ok && screen.terminalWidth() > 0 {
		width = screen.terminalWidth()
	}
	width = max(width, 2) // Wide characters cannot fit in a one-column terminal.
	prefix := []rune(prompt[strings.LastIndex(prompt, "\n")+1:])
	fmt.Fprint(out, prompt[:strings.LastIndex(prompt, "\n")+1])
	drawRunes(out, prefix, width, 0)
	var line []rune
	cursor, bytes := 0, 0
	endRow, endCol := visualPosition(prefix, width)
	position := func(index int) (int, int) {
		if index == len(line) {
			return endRow, endCol
		}
		return visualPosition(append(append([]rune(nil), prefix...), line[:index]...), width)
	}
	move := func(index int) {
		fromRow, _ := position(cursor)
		toRow, toCol := position(index)
		moveCursor(out, fromRow, toRow, toCol)
		cursor = index
	}
	redraw := func(oldRow int) {
		moveCursor(out, oldRow, 0, 0)
		fmt.Fprint(out, "\x1b[J")
		drawRunes(out, prefix, width, 0)
		_, prefixCol := visualPosition(prefix, width)
		drawRunes(out, line, width, prefixCol)
		endRow, _ := position(len(line))
		row, col := position(cursor)
		moveCursor(out, endRow, row, col)
	}
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
			move(len(line))
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
							move(cursor - 1)
						}
					case 'C':
						if cursor < len(line) {
							move(cursor + 1)
						}
					case 'H':
						move(0)
					case 'F':
						move(len(line))
					}
				}
			} else {
				hasQueued = true
			}
		case '\b', 127:
			if cursor > 0 {
				oldRow, _ := position(cursor)
				cursor--
				bytes -= len(string(line[cursor]))
				line = append(line[:cursor], line[cursor+1:]...)
				endRow, endCol = visualPosition(append(append([]rune(nil), prefix...), line...), width)
				redraw(oldRow)
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
				oldRow, oldCol := position(cursor)
				atEnd := cursor == len(line)
				bytes += len(string(key))
				line = append(line, 0)
				copy(line[cursor+1:], line[cursor:])
				line[cursor] = key
				cursor++
				if atEnd {
					drawRunes(out, []rune{key}, width, oldCol)
					if oldCol+displayWidth(key) > width {
						endRow++
						endCol = 0
					}
					endCol += displayWidth(key)
					if endCol == width {
						endRow++
						endCol = 0
					}
				} else {
					endRow, endCol = visualPosition(append(append([]rune(nil), prefix...), line...), width)
					redraw(oldRow)
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
