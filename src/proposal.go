package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type item struct {
	topic   string
	answer  string
	comment bool
}

var errCommentBack = errors.New("comment cancelled")
var errBadEncoding = errors.New("invalid UTF-8")
var errInputTooLong = errors.New("input too long")

const maxCommentBytes = 1 << 20
const maxRequestBytes = 256 << 10
const maxReviewLineBytes = 8 << 20

func parse(r io.Reader) ([]item, error) {
	var items []item
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 4096), maxReviewLineBytes)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		topic := strings.TrimSpace(strings.TrimPrefix(line, "何について：")) // Legacy proposal prefix.
		if !utf8.ValidString(topic) || strings.ContainsRune(topic, utf8.RuneError) || strings.IndexFunc(topic, unicode.IsControl) >= 0 {
			return nil, errors.New("invalid or control character in proposal")
		}
		if topic == "" {
			return nil, errors.New("empty topic")
		}
		if !s.Scan() || (strings.TrimSpace(s.Text()) != "Approval/Comment:" && strings.TrimSpace(s.Text()) != "承認/コメント：" && strings.TrimSpace(s.Text()) != "承認/コメント:") {
			return nil, fmt.Errorf("%q: expected empty Approval/Comment: line", topic)
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

func render(items []item) string {
	var b strings.Builder
	for _, item := range items {
		answer := item.answer
		if item.comment {
			answer = "Comment: " + strconv.Quote(answer)
		}
		fmt.Fprintf(&b, "%s\nApproval/Comment:%s\n\n", item.topic, answer)
	}
	return b.String()
}

func latestProposal(root string) (string, error) {
	paths, err := filepath.Glob(filepath.Join(root, "prop-review-tmp", "review.*", "proposal.txt"))
	if err != nil {
		return "", err
	}
	var latest string
	var latestTime int64
	for _, path := range paths {
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), "proposal.review.txt")); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			continue
		}
		if time := info.ModTime().UnixNano(); latest == "" || time > latestTime || time == latestTime && path > latest {
			latest, latestTime = path, time
		}
	}
	if latest == "" {
		return "", errors.New("no unreviewed proposal in prop-review-tmp")
	}
	return latest, nil
}

func saveProposal(root string, items []item) (string, error) {
	base := filepath.Join(root, "prop-review-tmp")
	if err := os.MkdirAll(base, 0700); err != nil {
		return "", err
	}
	// Keep ephemeral reviews out of Git without changing the project's tracked files.
	ignored := exec.Command("git", "-C", root, "check-ignore", "-q", "prop-review-tmp/probe")
	if err := ignored.Run(); err != nil {
		excludePath, err := exec.Command("git", "-C", root, "rev-parse", "--git-path", "info/exclude").Output()
		if err != nil {
			return "", err
		}
		path := strings.TrimSpace(string(excludePath))
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			return "", err
		}
		_, err = io.WriteString(f, "\n/prop-review-tmp/\n")
		closeErr := f.Close()
		if err != nil {
			return "", err
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	dir, err := os.MkdirTemp(base, "review.")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "proposal.txt")
	if err := os.WriteFile(path, []byte(render(items)), 0600); err != nil {
		os.Remove(dir)
		return "", err
	}
	return path, nil
}

func reviewedProposal(root string) (string, []item, error) {
	paths, err := filepath.Glob(filepath.Join(root, "prop-review-tmp", "review.*", "proposal.txt"))
	if err != nil {
		return "", nil, err
	}
	var latest string
	var latestTime int64
	for _, path := range paths {
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), "proposal.review.txt")); err != nil {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", nil, err
		}
		if latest == "" || info.ModTime().UnixNano() > latestTime || info.ModTime().UnixNano() == latestTime && path > latest {
			latest, latestTime = path, info.ModTime().UnixNano()
		}
	}
	if latest == "" {
		return "", nil, errors.New("no reviewed proposal in prop-review-tmp")
	}
	original, err := os.Open(latest)
	if err != nil {
		return "", nil, err
	}
	items, err := parse(original)
	original.Close()
	if err != nil {
		return "", nil, err
	}
	result, err := os.ReadFile(filepath.Join(filepath.Dir(latest), "proposal.review.txt"))
	if err != nil {
		return "", nil, err
	}
	// Match the whole result to its original before sending comments to the engine.
	var expected strings.Builder
	s := bufio.NewScanner(strings.NewReader(string(result)))
	s.Buffer(make([]byte, 4096), maxReviewLineBytes)
	for i := range items {
		if !s.Scan() || s.Text() != items[i].topic || !s.Scan() {
			return "", nil, errors.New("review result does not match proposal")
		}
		line := s.Text()
		prefix := "Approval/Comment:"
		if !strings.HasPrefix(line, prefix) {
			prefix = "承認/コメント：" // Legacy results.
			if !strings.HasPrefix(line, prefix) {
				return "", nil, errors.New("invalid review result")
			}
		}
		answer := strings.TrimPrefix(line, prefix)
		switch {
		case answer == "Approved", answer == "承認":
			items[i].answer = "Approved"
		case answer == "":
		case strings.HasPrefix(answer, "Comment: "), strings.HasPrefix(answer, "コメント："):
			commentPrefix := "Comment: "
			if strings.HasPrefix(answer, "コメント：") {
				commentPrefix = "コメント："
			}
			comment, err := strconv.Unquote(strings.TrimPrefix(answer, commentPrefix))
			if err != nil || comment == "" {
				return "", nil, errors.New("invalid review comment")
			}
			items[i].answer, items[i].comment = comment, true
		default:
			return "", nil, errors.New("invalid review answer")
		}
		fmt.Fprintf(&expected, "%s\n%s%s\n\n", items[i].topic, prefix, answer)
		if i < len(items)-1 && (!s.Scan() || s.Text() != "") {
			return "", nil, errors.New("invalid review separator")
		}
	}
	if s.Err() != nil || expected.String() != string(result) {
		return "", nil, errors.New("invalid review result")
	}
	return latest, items, nil
}
