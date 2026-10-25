package main

import (
	"path/filepath"
	"strings"
)

// displayPath only shortens paths within this repository; callers keep the original path for IO.
func displayPath(root, path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	} else if dir, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		abs = filepath.Join(dir, filepath.Base(abs))
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	if strings.HasPrefix(rel, "prop-review-tmp"+string(filepath.Separator)) {
		return strings.TrimPrefix(rel, "prop-review-tmp"+string(filepath.Separator))
	}
	return rel
}

func displayMessage(root, message string) string {
	if root == "" {
		return message
	}
	// Errors can contain several nested paths; shorten all project-owned paths on output only.
	message = strings.ReplaceAll(message, root+string(filepath.Separator)+"prop-review-tmp"+string(filepath.Separator), "")
	return strings.ReplaceAll(message, root+string(filepath.Separator), "")
}

func displayCurrentPath(path string) string {
	root, err := repoRoot()
	if err != nil {
		return path
	}
	return displayPath(root, path)
}
