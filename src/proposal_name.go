package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// rootRequest prefers the validated snapshot over the redundant saved title.
// Legacy proposals still use their complete, original request text.
func rootRequest(path string) (string, *githubIssue, error) {
	issue, err := readIssueSnapshot(path)
	if err != nil {
		return "", nil, err
	}
	if issue != nil {
		return issue.Title, issue, nil
	}
	request, err := localFile(filepath.Join(filepath.Dir(path), "request.txt"))
	if err != nil {
		return "", nil, err
	}
	if len(request) == 0 || len(request) > maxRequestBytes {
		return "", nil, errors.New("invalid root request/title")
	}
	return string(request), nil, nil
}

// proposalRoot follows only local review-directory links, never symlinked dirs.
func proposalRoot(path string) (string, error) {
	base := filepath.Dir(filepath.Dir(path))
	visited := make(map[string]bool)
	for {
		if visited[path] {
			return "", errors.New("cyclic review history")
		}
		visited[path] = true
		info, err := os.Lstat(filepath.Dir(path))
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return "", fmt.Errorf("unsafe review directory: %s", filepath.Dir(path))
		}
		if _, err := localFile(path); err != nil {
			return "", err
		}
		parent, err := parentName(path)
		if err != nil {
			return "", err
		}
		if parent == "" {
			return path, nil
		}
		path = filepath.Join(base, parent, "proposal.txt")
	}
}

func proposalDisplayName(path, missingRequestLabel string) (string, error) {
	root, err := proposalRoot(path)
	if err != nil {
		return "", err
	}
	request, _, err := rootRequest(root)
	if errors.Is(err, os.ErrNotExist) {
		request = missingRequestLabel
	} else if err != nil {
		return "", err
	}
	label := []rune(strings.SplitN(request, "\n", 2)[0])
	if len(label) > 100 {
		label = append(label[:100], '…')
	}
	return string(label), nil
}
