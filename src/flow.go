package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func saveFlowWithConfirmation(in io.Reader, out io.Writer) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	body, err := approvedFlow(root)
	if err != nil {
		return err
	}
	return confirmDocument(filepath.Join(root, "prop-review-tmp", "flow.txt"), body, "flow", in, out, func() error {
		current, err := approvedFlow(root)
		if err != nil {
			return err
		}
		if current != body {
			return errors.New("approved flow changed during confirmation; nothing saved")
		}
		return nil
	})
}

func approvedFlow(root string) (string, error) {
	paths, err := proposalPaths(root)
	if err != nil {
		return "", err
	}
	roots, err := clarificationRoots(root)
	if err != nil {
		return "", err
	}
	covered := map[string]bool{}
	var diagrams []string
	for _, path := range roots {
		if _, err := localFile(filepath.Join(filepath.Dir(path), "proposal.review.txt")); errors.Is(err, os.ErrNotExist) {
			continue // Unreviewed independent drafts have no approvals to contribute.
		}
		history, _, err := reviewHistory(root, path, false)
		if err != nil {
			return "", fmt.Errorf("invalid review history: %w", err)
		}
		for _, step := range history {
			covered[step.path] = true
		}
		input, _ := reviewInput(history, "")
		for _, topic := range input.Approved {
			diagrams = append(diagrams, flowTopic(topic))
		}
	}
	for _, path := range paths {
		if covered[path] {
			continue
		}
		parent, err := parentName(path)
		if err != nil {
			return "", err
		}
		_, reviewErr := localFile(filepath.Join(filepath.Dir(path), "proposal.review.txt"))
		if parent != "" || !errors.Is(reviewErr, os.ErrNotExist) {
			return "", fmt.Errorf("invalid review history: %s is not linked to a valid reviewed root", path)
		}
	}
	if len(diagrams) == 0 {
		return "", errors.New("no approved items in valid review histories; flow not saved")
	}
	return "承認済みフロー / Approved flow\n\n承認文中の → のみを遷移として表示。項目間の関係・分岐は推測しません。\nOnly explicit → transitions within approved topics are drawn; no inferred links or branches.\n\n" + strings.Join(diagrams, "\n\n") + "\n", nil
}

func flowTopic(topic string) string {
	// shortcut: only explicit → chains are split; richer notation needs an approved structured format.
	parts := strings.Split(topic, "→")
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			parts = []string{topic}
			break
		}
	}
	var boxes []string
	for _, part := range parts {
		text := strings.TrimSpace(part)
		width := 0
		for _, r := range text {
			width += displayWidth(r)
		}
		border := strings.Repeat("─", width+2)
		boxes = append(boxes, "┌"+border+"┐\n│ "+text+" │\n└"+border+"┘")
	}
	return strings.Join(boxes, "\n  │\n  ↓\n")
}
