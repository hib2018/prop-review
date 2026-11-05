package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	flowSourceListLimit  = 1 << 20
	flowSourceFileLimit  = 32 << 10
	flowSourceTotalLimit = 128 << 10
	flowSourceFileCount  = 32
)

type flowSourceFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type flowSourceInput struct {
	Files   []flowSourceFile `json:"files"`
	Omitted int              `json:"omitted"`
}

// flowSourceContext reads working-tree contents, never index blobs or review history.
func flowSourceContext(root string) ([]byte, error) {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("flow source root: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	tree, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("flow source root: %w", err)
	}
	defer tree.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-c", "core.fsmonitor=false", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--full-name", "--", ".")
	cmd.Dir = root
	// Ignore ambient Git redirections; listing must describe this tree only.
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_OPTIONAL_LOCKS=0")
	output := &flowSourceListing{cancel: cancel}
	cmd.Stdout, cmd.Stderr = output, io.Discard
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		if output.exceeded {
			return nil, fmt.Errorf("flow source git listing exceeds %d bytes", flowSourceListLimit)
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("flow source git listing timeout: %w", ctx.Err())
		}
		return nil, fmt.Errorf("flow source git ls-files: %w", err)
	}

	seen := map[string]bool{}
	var paths []string
	for _, name := range strings.Split(output.data.String(), "\x00") {
		if name != "" && !seen[name] {
			seen[name] = true
			paths = append(paths, name)
		}
	}
	sort.Slice(paths, func(i, j int) bool {
		a, b := flowSourcePriority(paths[i]), flowSourcePriority(paths[j])
		if a != b {
			return a < b
		}
		return paths[i] < paths[j]
	})
	input := flowSourceInput{Files: []flowSourceFile{}, Omitted: len(paths)}
	for _, name := range paths {
		if len(input.Files) == flowSourceFileCount {
			break
		}
		if !flowSourceAllowed(name) {
			continue
		}
		content, err := flowSourceRead(tree, name)
		if err != nil {
			continue
		}
		input.Files = append(input.Files, flowSourceFile{Path: name, Content: string(content)})
		input.Omitted--
		encoded, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		if len(encoded) > flowSourceTotalLimit {
			input.Files = input.Files[:len(input.Files)-1]
			input.Omitted++
		}
	}
	if len(input.Files) == 0 {
		return nil, errors.New("flow source: no readable source context in repository")
	}
	return json.Marshal(input)
}

type flowSourceListing struct {
	data     bytes.Buffer
	cancel   context.CancelFunc
	exceeded bool
}

func (b *flowSourceListing) Write(p []byte) (int, error) {
	if len(p) > flowSourceListLimit-b.data.Len() {
		b.exceeded = true
		b.cancel()
		return 0, errors.New("source listing too large")
	}
	return b.data.Write(p)
}

func flowSourceRead(tree *os.Root, name string) ([]byte, error) {
	parts := strings.Split(name, "/")
	var info os.FileInfo
	for i := range parts {
		var err error
		info, err = tree.Lstat(filepath.Join(parts[:i+1]...))
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !info.IsDir()) {
			return nil, errors.New("symlink or non-directory in source path")
		}
	}
	// Skip rather than truncate: even an unread tail could contain binary data.
	if !info.Mode().IsRegular() || info.Size() > flowSourceFileLimit {
		return nil, errors.New("source is not a bounded regular file")
	}
	// Root confines opens even if a checked directory is concurrently replaced.
	f, err := tree.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
		return nil, errors.New("source changed during open")
	}
	data, err := io.ReadAll(io.LimitReader(f, flowSourceFileLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > flowSourceFileLimit || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 || len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("source is oversized, binary, or empty")
	}
	return data, nil
}

func flowSourceAllowed(name string) bool {
	if !utf8.ValidString(name) || strings.ContainsAny(name, "\\\x00\r\n") || filepath.IsAbs(name) || filepath.ToSlash(filepath.Clean(name)) != name {
		return false
	}
	parts := strings.Split(strings.ToLower(name), "/")
	for _, part := range parts {
		if strings.HasPrefix(part, ".") {
			return false
		}
		for _, word := range []string{"secret", "credential", "password", "token", "private"} {
			if strings.Contains(part, word) {
				return false
			}
		}
		switch part {
		case "vendor", "node_modules", "prop-review-tmp", "reviewdata", "testdata", "test", "tests", "fixture", "fixtures", "config", "configs", "settings", "dist", "build", "coverage":
			return false
		}
	}
	base := parts[len(parts)-1]
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if stem == "config" || stem == "settings" || strings.HasPrefix(base, "config.") || strings.HasPrefix(base, "settings.") || strings.HasPrefix(base, "test_") || strings.HasSuffix(stem, "_test") || strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
		return false
	}
	if flowSourceManifest(base) {
		return true
	}
	if strings.HasPrefix(base, "readme") {
		switch filepath.Ext(base) {
		case "", ".md", ".txt", ".rst", ".markdown", ".adoc":
			return true
		}
		return false
	}
	switch filepath.Ext(base) {
	case ".go", ".rs", ".py", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".java", ".kt", ".kts", ".swift", ".c", ".h", ".cc", ".cpp", ".hpp", ".cs", ".fs", ".rb", ".php", ".sh", ".bash", ".zsh", ".ex", ".exs", ".erl", ".hs", ".lua", ".pl", ".r", ".scala", ".clj", ".cljs", ".dart", ".vue", ".svelte", ".zig":
		return true
	}
	return false
}

func flowSourceManifest(base string) bool {
	switch base {
	case "package.json", "composer.json", "go.mod", "cargo.toml", "pyproject.toml", "requirements.txt", "setup.py", "setup.cfg", "gemfile", "rakefile", "makefile", "cmakelists.txt", "build.gradle", "build.gradle.kts", "pom.xml", "mix.exs", "pubspec.yaml", "package.swift":
		return true
	}
	return false
}

func flowSourcePriority(name string) int {
	base := strings.ToLower(filepath.Base(name))
	if strings.HasPrefix(base, "readme") {
		return 0
	}
	if flowSourceManifest(base) {
		return 1
	}
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	switch stem {
	case "main", "cli", "commands", "app", "application", "tui", "ui", "index":
		return 2
	case "model", "update", "view", "render", "event", "events", "handler", "handlers", "key", "keys", "keymap", "keybindings", "screen", "screens", "reducer", "dispatch":
		return 3
	}
	for _, dir := range strings.Split(strings.ToLower(filepath.ToSlash(filepath.Dir(name))), "/") {
		switch dir {
		case "ui", "tui", "screens", "handlers", "events", "keymaps", "components":
			return 3
		}
	}
	if filepath.ToSlash(filepath.Dir(name)) == "src" {
		return 4
	}
	return 5
}
