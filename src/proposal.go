package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
		topic := line
		if !utf8.ValidString(topic) || strings.ContainsRune(topic, utf8.RuneError) || strings.IndexFunc(topic, unicode.IsControl) >= 0 {
			return nil, errors.New("invalid or control character in proposal")
		}
		if topic == "" {
			return nil, errors.New("empty topic")
		}
		if !s.Scan() || strings.TrimSpace(s.Text()) != "Approval/Comment:" {
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

func proposalPaths(root string) ([]string, error) {
	base := filepath.Join(root, "prop-review-tmp")
	entries, err := os.ReadDir(base)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "review.") {
			continue
		}
		path := filepath.Join(base, entry.Name(), "proposal.txt")
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func unreviewedProposals(root string) ([]string, error) {
	paths, err := proposalPaths(root)
	if err != nil {
		return nil, err
	}
	var candidates []string
	for _, path := range paths {
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), "proposal.review.txt")); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		candidates = append(candidates, path)
	}
	return newestFirst(candidates)
}

func newestFirst(paths []string) ([]string, error) {
	type candidate struct {
		path string
		time int64
	}
	var entries []candidate
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		entries = append(entries, candidate{path, info.ModTime().UnixNano()})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].time == entries[j].time {
			return entries[i].path > entries[j].path
		}
		return entries[i].time > entries[j].time
	})
	ordered := make([]string, len(entries))
	for i, entry := range entries {
		ordered[i] = entry.path
	}
	return ordered, nil
}

func latestProposal(root string) (string, error) {
	paths, err := unreviewedProposals(root)
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", errors.New("no unreviewed proposal in prop-review-tmp")
	}
	return paths[0], nil
}

func saveProposal(root string, items []item, parent string) (string, error) {
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
	if parent != "" {
		// The parent is another review directory under the same repository.
		name := filepath.Base(filepath.Dir(parent))
		if filepath.Dir(filepath.Dir(parent)) != base || filepath.Base(parent) != "proposal.txt" || !strings.HasPrefix(name, "review.") {
			os.Remove(dir)
			return "", errors.New("invalid parent proposal path")
		}
		if err := os.WriteFile(filepath.Join(dir, "parent.txt"), []byte(name+"\n"), 0600); err != nil {
			os.RemoveAll(dir)
			return "", err
		}
	}
	path := filepath.Join(dir, "proposal.txt")
	if err := os.WriteFile(path, []byte(render(items)), 0600); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return path, nil
}

func revisableProposals(root string) ([]string, error) {
	paths, err := proposalPaths(root)
	if err != nil {
		return nil, err
	}
	children := make(map[string]bool)
	for _, path := range paths {
		link, err := os.ReadFile(filepath.Join(filepath.Dir(path), "parent.txt"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(string(link), "\n")
		if !strings.HasPrefix(name, "review.") || name == "review." || string(link) != name+"\n" || filepath.Base(name) != name {
			return nil, fmt.Errorf("invalid parent.txt beside %s", path)
		}
		children[name] = true
	}
	var candidates []string
	for _, path := range paths {
		if children[filepath.Base(filepath.Dir(path))] {
			continue
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), "proposal.review.txt")); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		items, err := readReviewedProposal(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		for _, it := range items {
			if it.comment {
				candidates = append(candidates, path)
				break
			}
		}
	}
	return newestFirst(candidates)
}

func resolveRevisionPath(root, input string) (string, error) {
	abs, err := filepath.Abs(input)
	if err != nil {
		return "", err
	}
	base, err := filepath.EvalSymlinks(filepath.Join(root, "prop-review-tmp"))
	if err != nil {
		return "", fmt.Errorf("no prop-review-tmp in repository: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("revision proposal not found: %w", err)
	}
	name := filepath.Base(filepath.Dir(resolved))
	if filepath.Base(resolved) != "proposal.txt" || !strings.HasPrefix(name, "review.") || name == "review." || filepath.Dir(filepath.Dir(resolved)) != base {
		return "", errors.New("revision path must be this repository's prop-review-tmp/review.*/proposal.txt")
	}
	return filepath.Join(root, "prop-review-tmp", name, "proposal.txt"), nil
}

func reviewedProposal(root string) (string, []item, error) {
	paths, err := proposalPaths(root)
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
	items, err := readReviewedProposal(latest)
	return latest, items, err
}

func readReviewedProposal(path string) ([]item, error) {
	original, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	items, err := parse(original)
	original.Close()
	if err != nil {
		return nil, err
	}
	result, err := os.ReadFile(filepath.Join(filepath.Dir(path), "proposal.review.txt"))
	if err != nil {
		return nil, err
	}
	// Match the whole result to its original before sending comments to the engine.
	var expected strings.Builder
	s := bufio.NewScanner(strings.NewReader(string(result)))
	s.Buffer(make([]byte, 4096), maxReviewLineBytes)
	for i := range items {
		if !s.Scan() || s.Text() != items[i].topic || !s.Scan() {
			return nil, errors.New("review result does not match proposal")
		}
		line := s.Text()
		prefix := "Approval/Comment:"
		if !strings.HasPrefix(line, prefix) {
			return nil, errors.New("invalid review result")
		}
		answer := strings.TrimPrefix(line, prefix)
		switch {
		case answer == "Approved":
			items[i].answer = "Approved"
		case answer == "":
		case strings.HasPrefix(answer, "Comment: "):
			comment, err := strconv.Unquote(strings.TrimPrefix(answer, "Comment: "))
			if err != nil || comment == "" {
				return nil, errors.New("invalid review comment")
			}
			items[i].answer, items[i].comment = comment, true
		default:
			return nil, errors.New("invalid review answer")
		}
		fmt.Fprintf(&expected, "%s\n%s%s\n\n", items[i].topic, prefix, answer)
		if i < len(items)-1 && (!s.Scan() || s.Text() != "") {
			return nil, errors.New("invalid review separator")
		}
	}
	if s.Err() != nil || expected.String() != string(result) {
		return nil, errors.New("invalid review result")
	}
	return items, nil
}
