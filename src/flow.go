package main

import (
	"bufio"
	"bytes"
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
	return confirmFlow(root, body, in, out)
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

func confirmFlow(root, body string, in io.Reader, out io.Writer) error {
	path := filepath.Join(root, "prop-review-tmp", "flow.txt")
	old, err := existingFlow(path)
	if err != nil {
		return err
	}
	action := "Save new file"
	if old != nil {
		action = "Overwrite existing file"
	}
	fmt.Fprintf(out, "\nGenerated flow for %s:\n\n%s\n", displayCurrentPath(path), body)
	if old != nil {
		fmt.Fprintf(out, "Existing file will be replaced (%d bytes).\n", len(old))
	}
	answer, err := inputLine(bufio.NewReader(in), out, action+"? [y/N]: ", 32)
	if err != nil || !strings.EqualFold(strings.TrimSpace(answer), "y") {
		return fmt.Errorf("%s not saved: confirmation cancelled", filepath.Base(path))
	}
	currentBody, err := approvedFlow(root)
	if err != nil {
		return err
	}
	if currentBody != body {
		return errors.New("approved flow changed during confirmation; nothing saved")
	}
	// Recheck before writing so a change made while the human reads the preview is not lost.
	current, err := existingFlow(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(old, current) || (old == nil) != (current == nil) {
		return fmt.Errorf("%s changed during confirmation; nothing saved", filepath.Base(path))
	}
	if old == nil {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		if _, err = io.WriteString(f, body); err != nil {
			f.Close()
			os.Remove(path)
			return err
		}
		if err = f.Close(); err != nil {
			os.Remove(path)
			return err
		}
	} else {
		f, err := os.CreateTemp(filepath.Dir(path), ".flow-")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if err := f.Chmod(0600); err != nil {
			f.Close()
			return err
		}
		if _, err = io.WriteString(f, body); err != nil {
			f.Close()
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
		if err := os.Rename(f.Name(), path); err != nil {
			return err
		}
	}
	return nil
}

func existingFlow(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("unsafe output file: %s", path)
	}
	return os.ReadFile(path)
}
