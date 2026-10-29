package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func saveSpec(engine string) error {
	return withRawTTY(func(tty *os.File) error {
		return saveSpecWithConfirmation(engine, tty, tty)
	})
}

func saveSpecWithConfirmation(engine string, in io.Reader, out io.Writer) error {
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
	var warnings []string
	excluded := map[string]bool{}
	var roots []string
	for _, path := range paths {
		parent, err := parentName(path)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("Excluded %s: %v", path, err))
			excluded[path] = true
			continue
		}
		if parent != "" {
			if !known[parent] {
				warnings = append(warnings, fmt.Sprintf("Excluded %s: missing parent review %s", path, parent))
				excluded[path] = true
			}
			continue
		}
		_, err = localFile(filepath.Join(filepath.Dir(path), "proposal.review.txt"))
		if err == nil {
			roots = append(roots, path)
		} else if !errors.Is(err, os.ErrNotExist) {
			warnings = append(warnings, fmt.Sprintf("Excluded %s: %v", path, err))
			excluded[path] = true
		}
	}
	if len(roots) == 0 {
		return fmt.Errorf("no valid reviewed proposal in prop-review-tmp: %s", strings.Join(warnings, "; "))
	}
	// Keep output stable across runs, independent of filesystem enumeration order.
	roots, err = newestFirst(roots)
	if err != nil {
		return err
	}
	output := filepath.Join(root, "prop-review-tmp", "spec.md")
	var approved []string
	covered := map[string]bool{}
	stopChecking := func() {}
	defer func() { stopChecking() }()
	checkingShown := false
	for _, path := range roots {
		history, request, err := reviewHistory(root, path, false)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("Excluded %s: %v", path, err))
			excluded[path] = true
			continue
		}
		if request == "" {
			warnings = append(warnings, fmt.Sprintf("Cannot clarify %s: missing request.txt; run clarify after restoring the request.", path))
		} else {
			if !checkingShown {
				stopChecking = startSpinner(out, "Checking spec clarifications", engine)
				checkingShown = true
			}
			questions, _, err := clarificationQuestionsFromHistory(root, history, request, engine, "Checking spec clarifications", io.Discard)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("Could not check additional questions for %s: %v; retry with `prop-review clarify`.", path, err))
			} else if len(questions) > 0 {
				warnings = append(warnings, fmt.Sprintf("Additional questions for %s: run `prop-review clarify`.", path))
			}
		}
		seen := map[string]bool{}
		for _, step := range history {
			covered[step.path] = true
			for _, it := range step.items {
				if !it.comment && it.answer == "Approved" && !seen[it.topic] {
					approved = append(approved, it.topic)
					seen[it.topic] = true
				}
			}
		}
	}
	stopChecking()
	for _, path := range paths {
		if covered[path] || excluded[path] {
			continue
		}
		if _, err := localFile(filepath.Join(filepath.Dir(path), "proposal.review.txt")); err == nil {
			warnings = append(warnings, fmt.Sprintf("Excluded %s: review is not linked to a valid root", path))
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			warnings = append(warnings, fmt.Sprintf("Excluded %s: %v", path, err))
		}
	}
	if len(approved) == 0 {
		return fmt.Errorf("no approved items in valid review chains; %s", strings.Join(warnings, "; "))
	}
	data, err := json.Marshal(approved)
	if err != nil {
		return err
	}
	prompt := fmt.Sprintf(specPrompt, data)
	response, err := engineResponse(root, prompt, engine, 100*time.Second, io.Discard, "Summarizing approved items")
	if err != nil {
		return err
	}
	memo := strings.TrimSpace(string(response))
	if memo == "" || len(memo) > maxCommentBytes || !utf8.ValidString(memo) || strings.ContainsRune(memo, utf8.RuneError) || strings.IndexFunc(memo, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) >= 0 {
		return errors.New("invalid spec memo from engine")
	}
	body := "# 承認事項のメモ / Approved items memo\n\n参考用の要約です。承認内容は元のレビュー結果を確認してください。 / Reference only; check the original review results for approvals.\n\n" + memo + "\n"
	return confirmSpec(output, body, in, out, warnings...)
}

func confirmSpec(path, body string, in io.Reader, out io.Writer, warnings ...string) error {
	old, err := existingSpec(path)
	if err != nil {
		return err
	}
	action := "Save new file"
	if old != nil {
		action = "Overwrite existing file"
	}
	fmt.Fprintf(out, "\nGenerated memo for %s:\n\n%s\n", displayCurrentPath(path), body)
	if len(warnings) > 0 {
		fmt.Fprintln(out, "Warnings (excluded or unverified reviews):")
		root := filepath.Dir(filepath.Dir(path))
		for _, warning := range warnings {
			fmt.Fprintln(out, "- "+displayMessage(root, warning))
		}
	}
	if old != nil {
		fmt.Fprintf(out, "Existing file will be replaced (%d bytes).\n", len(old))
	}
	answer, err := inputLine(bufio.NewReader(in), out, action+"? [y/N]: ", 32)
	if err != nil || !strings.EqualFold(strings.TrimSpace(answer), "y") {
		return errors.New("spec not saved: confirmation cancelled")
	}
	// Recheck before writing so a change made while the human reads the preview is not lost.
	current, err := existingSpec(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(old, current) || (old == nil) != (current == nil) {
		return errors.New("spec changed during confirmation; nothing saved")
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
		f, err := os.CreateTemp(filepath.Dir(path), ".spec-")
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

func existingSpec(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("unsafe spec file: %s", path)
	}
	return os.ReadFile(path)
}
