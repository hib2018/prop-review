package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func saveSpec(engine string) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	paths, err := proposalPaths(root)
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(paths))
	for _, path := range paths {
		known[filepath.Base(filepath.Dir(path))] = true
	}
	var roots []string
	for _, path := range paths {
		parent, err := parentName(path)
		if err != nil {
			return err
		}
		if parent != "" {
			if !known[parent] {
				return fmt.Errorf("missing parent review: %s", parent)
			}
			continue
		}
		_, err = localFile(filepath.Join(filepath.Dir(path), "proposal.review.txt"))
		if err == nil {
			roots = append(roots, path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if len(roots) == 0 {
		return errors.New("no reviewed proposal in prop-review-tmp")
	}
	// Keep output stable across runs, independent of filesystem enumeration order.
	roots, err = newestFirst(roots)
	if err != nil {
		return err
	}
	output := filepath.Join(root, "prop-review-tmp", "spec.md")
	if _, err := os.Lstat(output); err == nil {
		return fmt.Errorf("spec already exists: %s", output)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var body strings.Builder
	body.WriteString("# Approved specification\n")
	count := 0
	covered := map[string]bool{}
	for _, path := range roots {
		history, request, err := reviewHistory(root, path, false)
		if err != nil {
			return err
		}
		if request == "" {
			fmt.Fprintf(os.Stderr, "Cannot clarify %s: missing request.txt; run clarify after restoring the request.\n", path)
		} else {
			questions, _, err := clarificationQuestions(root, path, engine)
			if err != nil {
				return err
			}
			if len(questions) > 0 {
				fmt.Fprintf(os.Stderr, "Additional questions for %s: run `prop-review clarify`.\n", path)
			}
		}
		var section strings.Builder
		seen := map[string]bool{}
		for _, step := range history {
			covered[step.path] = true
			for _, it := range step.items {
				if it.answer == "Approved" && !seen[it.topic] {
					fmt.Fprintf(&section, "- %s\n", it.topic)
					seen[it.topic] = true
					count++
				}
			}
		}
		if section.Len() > 0 {
			fmt.Fprintf(&body, "\n## %s\n\n%s", path, section.String())
		}
	}
	for _, path := range paths {
		if _, err := localFile(filepath.Join(filepath.Dir(path), "proposal.review.txt")); err == nil && !covered[path] {
			return fmt.Errorf("review is not linked to a valid root: %s", path)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if count == 0 {
		return errors.New("no approved items to save")
	}
	f, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = io.WriteString(f, body.String()); err != nil {
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
