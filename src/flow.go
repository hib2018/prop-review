package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Only explicit approvals cross the execution boundary, never requests or comments.
func flowApprovals(root string) ([]string, error) {
	if err := checkReviewBase(root); err != nil {
		return nil, err
	}
	base := filepath.Join(root, "prop-review-tmp")
	entries, err := os.ReadDir(base)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var reviewed, roots []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "review.") {
			continue
		}
		if !entry.IsDir() {
			return nil, fmt.Errorf("unsafe review directory: %s", entry.Name())
		}
		path := filepath.Join(base, entry.Name(), "proposal.txt")
		_, err := localFile(filepath.Join(filepath.Dir(path), "proposal.review.txt"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		parent, err := parentName(path)
		if err != nil {
			return nil, err
		}
		reviewed = append(reviewed, path)
		if parent == "" {
			roots = append(roots, path)
		}
	}
	seen := make(map[string]bool)
	accepted := make(map[string]bool)
	var approvals []string
	for _, rootPath := range roots {
		history, _, err := reviewHistoryPaths(root, rootPath, false, reviewed)
		if err != nil {
			return nil, err
		}
		for _, step := range history {
			seen[step.path] = true
			for _, it := range step.items {
				if !it.comment && it.answer == "Approved" && !accepted[it.topic] {
					approvals = append(approvals, it.topic)
					accepted[it.topic] = true
				}
			}
		}
	}
	if len(seen) != len(reviewed) {
		return nil, errors.New("broken or cyclic reviewed history: every reviewed proposal must have a reviewed root")
	}
	return approvals, nil
}

func showFlow(engine string, out, progress io.Writer) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	approvals, err := flowApprovals(root)
	if err != nil {
		return fmt.Errorf("validate execution approvals: %w", err)
	}
	if len(approvals) == 0 {
		_, err := fmt.Fprintln(out, "No approved proposals; nothing to execute.")
		return err
	}
	data, err := json.Marshal(approvals)
	if err != nil {
		return err
	}
	response, err := flowEngineResponse(root, fmt.Sprintf(flowPrompt, data), engine, progress)
	if err != nil {
		return fmt.Errorf("execute approved proposals (changes may already exist): %w", err)
	}
	var result struct {
		Status  string `json:"status"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(response, &result); err != nil || strings.TrimSpace(result.Summary) == "" {
		return errors.New("invalid flow execution result; inspect the working tree for partial changes")
	}
	switch result.Status {
	case "completed":
		_, err = fmt.Fprintf(out, "%q\n", result.Summary)
		return err
	case "blocked", "failed":
		return fmt.Errorf("flow %s (changes may already exist): %q", result.Status, result.Summary)
	default:
		return errors.New("invalid flow execution status; inspect the working tree for partial changes")
	}
}
