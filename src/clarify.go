package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type reviewStep struct {
	issue       *githubIssue // Root snapshot, populated only after validating the linked history.
	path        string
	items       []item
	followup    bool
	restructure bool
}

func localFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("unsafe review file: %s", path)
	}
	return os.ReadFile(path)
}

func parentName(path string) (string, error) {
	link, err := localFile(filepath.Join(filepath.Dir(path), "parent.txt"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	name := strings.TrimSuffix(string(link), "\n")
	if !strings.HasPrefix(name, "review.") || name == "review." || filepath.Base(name) != name || string(link) != name+"\n" {
		return "", fmt.Errorf("invalid parent.txt beside %s", path)
	}
	return name, nil
}

func clarificationRoots(root string) ([]string, error) {
	paths, err := proposalPaths(root)
	if err != nil {
		return nil, err
	}
	var roots []string
	for _, path := range paths {
		parent, err := parentName(path)
		if err != nil {
			return nil, err
		}
		if parent == "" {
			roots = append(roots, path)
		}
	}
	return newestFirst(roots)
}

func selectClarificationRoot(paths []string, in io.Reader, out io.Writer) (string, error) {
	root, _ := repoRoot()
	for i, path := range paths {
		label, err := proposalDisplayName(path, "invalid: missing request.txt")
		if err != nil {
			return "", err
		}
		fmt.Fprintf(out, "%d) %s  %q\n", i+1, displayPath(root, path), label)
	}
	reader := bufio.NewReader(in)
	for {
		line, err := inputLine(reader, out, "Root number (q to cancel): ", 32)
		if err != nil {
			return "", err
		}
		if line == "q" {
			return "", errors.New("clarification cancelled")
		}
		index, err := strconv.Atoi(line)
		if err == nil && index > 0 && index <= len(paths) {
			return paths[index-1], nil
		}
		fmt.Fprintln(out, "Select a listed root number or q.")
	}
}

func clarificationHistory(root, selected string) ([]reviewStep, string, error) {
	return reviewHistory(root, selected, true)
}

func reviewHistory(root, selected string, requireRequest bool) ([]reviewStep, string, error) {
	paths, err := proposalPaths(root)
	if err != nil {
		return nil, "", err
	}
	base := filepath.Join(root, "prop-review-tmp")
	known := make(map[string]string, len(paths))
	children := make(map[string][]string)
	for _, path := range paths {
		name := filepath.Base(filepath.Dir(path))
		known[name] = path
		parent, err := parentName(path)
		if err != nil {
			if requireRequest {
				return nil, "", err
			}
			continue // A malformed, unlinked review cannot invalidate an unrelated spec chain.
		}
		if parent != "" {
			children[parent] = append(children[parent], name)
		}
	}
	name := filepath.Base(filepath.Dir(selected))
	if known[name] != selected {
		return nil, "", errors.New("root not found in repository")
	}
	parent, err := parentName(selected)
	if err != nil || parent != "" {
		return nil, "", errors.New("selected proposal is not a root")
	}
	visited := make(map[string]bool)
	var history []reviewStep
	for {
		if visited[name] {
			return nil, "", errors.New("cyclic review history")
		}
		visited[name] = true
		path := known[name]
		if path == "" {
			return nil, "", fmt.Errorf("missing review directory: %s", name)
		}
		for _, file := range []string{path, filepath.Join(filepath.Dir(path), "proposal.review.txt")} {
			if _, err := localFile(file); err != nil {
				return nil, "", fmt.Errorf("%s: %w", file, err)
			}
		}
		items, err := readReviewedProposal(path)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", path, err)
		}
		marker, err := localFile(filepath.Join(base, name, "followup.txt"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, "", err
		}
		if err == nil && string(marker) != "followup\n" {
			return nil, "", fmt.Errorf("invalid followup.txt beside %s", path)
		}
		step := reviewStep{path: path, items: items, followup: err == nil}
		restructure, err := reconstructionMarker(path)
		if err != nil {
			return nil, "", err
		}
		step.restructure = restructure
		if restructure && (step.followup || len(history) == 0) {
			return nil, "", errors.New("restructure requires a parent and cannot be a followup")
		}
		if len(history) == 0 && step.followup {
			return nil, "", errors.New("root cannot be a followup")
		}
		if len(history) > 0 && !step.followup && !step.restructure {
			prior := history[len(history)-1].items
			var pending []string
			for _, it := range prior {
				if it.answer == "" {
					pending = append(pending, it.topic)
				}
			}
			if len(items) != len(pending)+countComments(prior) {
				return nil, "", fmt.Errorf("revision item count mismatch: %s", path)
			}
			j := 0
			for _, it := range prior {
				if !it.comment && it.answer == "Approved" {
					continue
				}
				if it.answer == "" && items[j].topic != it.topic {
					return nil, "", fmt.Errorf("pending item changed: %s", path)
				}
				j++
			}
		}
		history = append(history, step)
		if len(children[name]) == 0 {
			break
		}
		if len(children[name]) != 1 {
			return nil, "", fmt.Errorf("ambiguous review branches from %s", path)
		}
		name = children[name][0]
	}
	request, issue, err := rootRequest(selected)
	if errors.Is(err, os.ErrNotExist) && !requireRequest {
		return history, "", nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", errors.New("invalid root: missing request.txt")
	}
	if err != nil {
		return nil, "", err
	}
	history[0].issue = issue
	return history, request, nil
}

func countComments(items []item) int {
	n := 0
	for _, it := range items {
		if it.comment {
			n++
		}
	}
	return n
}

func clarificationQuestions(root, selected, engine string) ([]item, string, error) {
	return clarificationQuestionsFor(root, selected, engine, "Checking additional questions", os.Stderr)
}

func clarificationQuestionsFor(root, selected, engine, activity string, progress io.Writer) ([]item, string, error) {
	history, request, err := clarificationHistory(root, selected)
	if err != nil {
		return nil, "", err
	}
	return clarificationQuestionsFromHistory(root, history, request, engine, activity, progress)
}

func clarificationQuestionsFromHistory(root string, history []reviewStep, request, engine, activity string, progress io.Writer) ([]item, string, error) {
	input, outstanding := reviewInput(history, request)
	accepted := make(map[string]bool)
	for _, topic := range input.Approved {
		accepted[topic] = true
	}
	if len(outstanding) > 10 {
		return nil, "", errors.New("clarification exceeds 10 questions")
	}
	if len(outstanding) == 10 {
		return outstanding, history[len(history)-1].path, nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, "", err
	}
	prompt := fmt.Sprintf(clarifyPrompt, data, 10-len(outstanding))
	items, err := proposalsWithActivity(root, prompt, engine, 100*time.Second, progress, true, activity)
	if err != nil {
		return nil, "", err
	}
	seen := make(map[string]bool)
	for _, it := range outstanding {
		seen[it.topic] = true
	}
	for _, it := range items {
		if seen[it.topic] || accepted[it.topic] {
			continue
		}
		outstanding = append(outstanding, it)
		seen[it.topic] = true
	}
	if len(outstanding) > 10 {
		return nil, "", errors.New("clarification exceeds 10 questions")
	}
	return outstanding, history[len(history)-1].path, nil
}

func clarify(engine string) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	roots, err := clarificationRoots(root)
	if err != nil {
		return err
	}
	if len(roots) == 0 {
		return errors.New("no root proposal in prop-review-tmp")
	}
	var selected string
	err = withRawTTY(func(tty *os.File) error {
		var selectErr error
		selected, selectErr = selectClarificationRoot(roots, tty, tty)
		return selectErr
	})
	if err != nil {
		return err
	}
	history, request, err := clarificationHistory(root, selected)
	if err != nil {
		return err
	}
	path, err := saveClarificationFromHistory(root, history, request, engine, os.Stderr)
	if err != nil {
		return err
	}
	if path == "" {
		fmt.Println("No additional questions; no proposal created.")
		return nil
	}
	fmt.Println(displayPath(root, path))
	return run([]string{path})
}

func saveClarification(root string, items []item, parent string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}
	return saveProposalWithMetadata(root, items, parent, "", true)
}

// Save only after successful generation and validation. The caller alone opens review.
func saveClarificationFromHistory(root string, history []reviewStep, request, engine string, progress io.Writer) (string, error) {
	if len(history) == 0 {
		return "", errors.New("empty review history")
	}
	input, _ := reviewInput(history, request)
	for _, it := range input.Outstanding {
		if it.Status == "commented" {
			prompt, err := reconstructionPromptFromHistory(history, request)
			if err != nil {
				return "", err
			}
			items, err := proposalsWithActivity(root, prompt, engine, 100*time.Second, progress, false, "Reconstructing proposals")
			if err != nil {
				return "", err
			}
			return saveProposalWithMode(root, items, history[len(history)-1].path, "", false, true)
		}
	}
	items, parent, err := clarificationQuestionsFromHistory(root, history, request, engine, "Checking additional questions", progress)
	if err != nil {
		return "", err
	}
	return saveClarification(root, items, parent)
}
