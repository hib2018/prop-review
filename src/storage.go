package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var reviewFiles = []string{"request.txt", "proposal.txt", "proposal.review.txt", "issue.json", "parent.txt", "followup.txt", "revision.txt"}

// Keep ephemeral reviews out of Git without changing tracked project files.
func ensureReviewExcluded(root string, paths ...string) error {
	if len(paths) == 0 {
		for _, name := range reviewFiles {
			paths = append(paths, filepath.Join("prop-review-tmp", "review.init", name))
		}
	}
	if err := checkReviewExcluded(root, paths...); err == nil {
		return nil
	} else {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return err
		}
	}
	output, err := exec.Command("git", "-C", root, "rev-parse", "--git-path", "info/exclude").Output()
	if err != nil {
		return fmt.Errorf("locate exclusion for %s: %w; check Git settings", paths[0], err)
	}
	path := strings.TrimSpace(string(output))
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !strings.Contains("\n"+string(data)+"\n", "\n/prop-review-tmp/\n") {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, "\n/prop-review-tmp/\n")
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return checkReviewExcluded(root, paths...)
}

func checkReviewExcluded(root string, paths ...string) error {
	for _, path := range paths {
		output, err := exec.Command("git", "-C", root, "check-ignore", "-q", "--", path).CombinedOutput()
		if err != nil {
			reason := strings.TrimSpace(string(output))
			var exit *exec.ExitError
			if errors.As(err, &exit) && exit.ExitCode() == 1 {
				reason = "path is not ignored or is tracked"
			}
			return fmt.Errorf("Git exclusion not confirmed for %s: %s (%w); check .gitignore and tracking settings", path, reason, err)
		}
	}
	return nil
}

func saveReview(output string, items []item) error {
	path, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	base := filepath.Dir(dir)
	if filepath.Base(base) == "prop-review-tmp" && strings.HasPrefix(filepath.Base(dir), "review.") {
		if err := checkReviewExcluded(filepath.Dir(base), path); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = io.WriteString(f, render(items)); err != nil {
		f.Close()
		os.Remove(output)
		return err
	}
	if err = f.Close(); err != nil {
		os.Remove(output)
		return err
	}
	return nil
}
