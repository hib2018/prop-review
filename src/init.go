package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	bundled "prop-review/skills"
)

type skillCopy struct {
	path string
	data []byte
}

// initRepository installs repository-local skills without replacing user files.
func initRepository(root string) error {
	var copies []skillCopy
	for _, base := range []string{".pi/skills", ".agents/skills"} {
		for _, name := range []string{"prop-review", "prop-review-ingest"} {
			data, err := bundled.Files.ReadFile(name + "/SKILL.md")
			if err != nil {
				return err
			}
			copies = append(copies, skillCopy{filepath.Join(root, base, name, "SKILL.md"), data})
		}
	}
	// Preflight every destination before creating anything. Reject symlinked
	// parent directories too, so installation cannot escape into global skills.
	for _, c := range copies {
		if err := checkInitPath(root, c.path, c.data); err != nil {
			return err
		}
	}
	if err := checkInitPath(root, filepath.Join(root, "prop-review-tmp"), nil); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, "prop-review-tmp"), 0700); err != nil {
		return err
	}
	if err := ensureReviewExcluded(root); err != nil {
		return err
	}
	for _, c := range copies {
		if err := os.MkdirAll(filepath.Dir(c.path), 0755); err != nil {
			return err
		}
		// Recheck and use exclusive creation; a concurrently created file must not
		// be truncated, even if the preflight found the destination absent.
		if err := checkInitPath(root, c.path, c.data); err != nil {
			return err
		}
		if _, err := os.Lstat(c.path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		f, err := os.OpenFile(c.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return err
		}
		_, writeErr := f.Write(c.data)
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	fmt.Println("Initialized prop-review: prop-review-tmp, .pi/skills, .agents/skills")
	return nil
}

// A nil content denotes a directory; non-nil content denotes a regular file.
func checkInitPath(root, path string, content []byte) error {
	for current := path; current != root; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("init conflict: %s is a symbolic link", current)
		}
		if current != path || content == nil {
			if !info.IsDir() {
				return fmt.Errorf("init conflict: %s is not a directory", current)
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("init conflict: %s is not a regular file", current)
		}
		existing, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		if !bytes.Equal(existing, content) {
			return fmt.Errorf("init conflict: %s has different content", current)
		}
	}
	return nil
}
