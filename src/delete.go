package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Inspect every review directory before offering anything for deletion. Links must not
// cause a scan or a later removal to reach outside this repository.
func deletionReferences(root string) (map[string]bool, error) {
	base := filepath.Join(root, "prop-review-tmp")
	info, err := os.Lstat(base)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("unsafe review directory: %s", base)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	referenced := make(map[string]bool)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "review.") {
			continue
		}
		dir := filepath.Join(base, entry.Name())
		if !entry.IsDir() {
			return nil, fmt.Errorf("unsafe review directory: %s", dir)
		}
		for _, name := range []string{"proposal.txt", "proposal.review.txt", "parent.txt"} {
			path := filepath.Join(dir, name)
			info, err := os.Lstat(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("unsafe review file: %s", path)
			}
			if name == "parent.txt" {
				link, err := os.ReadFile(path)
				if err != nil {
					return nil, err
				}
				parent := strings.TrimSuffix(string(link), "\n")
				if !strings.HasPrefix(parent, "review.") || parent == "review." || filepath.Base(parent) != parent || string(link) != parent+"\n" {
					return nil, fmt.Errorf("invalid parent.txt: %s", path)
				}
				referenced[parent] = true
			}
		}
	}
	return referenced, nil
}

func deletableProposals(root string) ([]string, error) {
	referenced, err := deletionReferences(root)
	if err != nil {
		return nil, err
	}
	paths, err := proposalPaths(root)
	if err != nil {
		return nil, err
	}
	var candidates []string
	for _, path := range paths {
		if referenced[filepath.Base(filepath.Dir(path))] {
			continue
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), "proposal.review.txt")); errors.Is(err, os.ErrNotExist) {
			candidates = append(candidates, path)
			continue
		} else if err != nil {
			return nil, err
		}
		items, err := readReviewedProposal(path)
		if err != nil { // An invalid review is not eligible for automatic deletion.
			continue
		}
		for _, it := range items {
			if it.comment {
				candidates = append(candidates, path)
				break
			}
		}
	}
	return newestFirst(candidates)
}

func deletionContents(path string) (string, string, error) {
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", "", err
	}
	for _, entry := range entries {
		if entry.IsDir() || !slices.Contains([]string{"proposal.txt", "proposal.review.txt", "parent.txt"}, entry.Name()) {
			return "", "", fmt.Errorf("unexpected file in review directory: %s", filepath.Join(dir, entry.Name()))
		}
	}
	proposal, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	result, err := os.ReadFile(filepath.Join(dir, "proposal.review.txt"))
	if errors.Is(err, os.ErrNotExist) {
		return string(proposal), "", nil
	}
	if err != nil {
		return "", "", err
	}
	return string(proposal), string(result), nil
}

func deleteProposal(root string, in io.Reader, out io.Writer) error {
	paths, err := deletableProposals(root)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return errors.New("no unreviewed or awaiting-revision proposals to delete")
	}
	reader := bufio.NewReader(in)
	path, err := selectProposal(paths, reader, out)
	if err != nil {
		return err
	}
	proposal, result, err := deletionContents(path)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nDelete: %s\nProposal:\n%s", path, proposal)
	if result != "" {
		fmt.Fprintf(out, "Review (including any approvals):\n%s", result)
	}
	confirmation, err := inputLine(reader, out, "Type delete to confirm (anything else cancels): ", 32)
	if err != nil {
		return fmt.Errorf("deletion cancelled: %w", err)
	}
	if confirmation != "delete" {
		return errors.New("deletion cancelled; nothing removed")
	}
	// Recheck both eligibility and contents after the human has seen them.
	paths, err = deletableProposals(root)
	if err != nil {
		return err
	}
	if !slices.Contains(paths, path) {
		return errors.New("proposal is no longer eligible for deletion")
	}
	current, currentResult, err := deletionContents(path)
	if err != nil {
		return err
	}
	if current != proposal || currentResult != result {
		return errors.New("proposal or review changed during confirmation")
	}
	dir := filepath.Dir(path)
	for _, name := range []string{"proposal.review.txt", "parent.txt", "proposal.txt"} {
		err := os.Remove(filepath.Join(dir, name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Remove(dir); err != nil {
		return err
	}
	fmt.Fprintf(out, "Deleted: %s\n", dir)
	return nil
}
