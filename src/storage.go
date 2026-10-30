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

// Keep ephemeral reviews out of Git without changing tracked project files.
func ensureReviewExcluded(root string) error {
	err := exec.Command("git", "-C", root, "check-ignore", "-q", "prop-review-tmp/probe").Run()
	if err == nil {
		return nil
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		return fmt.Errorf("check review exclusion: %w", err)
	}
	output, err := exec.Command("git", "-C", root, "rev-parse", "--git-path", "info/exclude").Output()
	if err != nil {
		return err
	}
	path := strings.TrimSpace(string(output))
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
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
	return closeErr
}
