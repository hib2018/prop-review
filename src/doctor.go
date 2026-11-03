package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// doctor only inspects local state; never call initialization or generation here.
func doctor(root string, out io.Writer) error {
	failed := false
	report := func(label string, err error, advice string) {
		if err != nil {
			failed = true
			fmt.Fprintf(out, "WARN %s: %s; repair: %s\n", label, displayMessage(root, err.Error()), advice)
		} else {
			fmt.Fprintf(out, "OK %s\n", label)
		}
	}
	fmt.Fprintln(out, "Read-only diagnosis; no repairs or authentication/API checks are performed.")
	engine, err := selectedEngineForDoctor()
	report("engine "+engine, err, "select pi or fx with prop-review --engine NAME")
	commands := []string{"git", "gh"}
	switch engine {
	case "pi":
		commands = append(commands, "node", "npm", "pi")
	case "fx":
		commands = append(commands, "fx")
	}
	for _, name := range commands {
		_, err := exec.LookPath(name)
		report("command "+name, err, "install "+name+" or correct PATH (pi is used by ingest; gh by issue)")
	}
	if engine == "pi" {
		err := doctorPiSDK(root)
		report("Pi SDK placement", err, "install @earendil-works/pi-coding-agent globally for the npm/node on PATH")
	} else {
		fmt.Fprintln(out, "SKIP Pi SDK placement: Pi is not selected")
	}
	// Check prospective files as well as existing ones, without ensureReviewExcluded.
	for _, name := range reviewFiles {
		path := filepath.Join("prop-review-tmp", "review.doctor", name)
		report("Git exclusion "+path, checkReviewExcluded(root, path), "review Git ignore rules and tracked files manually; doctor changes neither")
	}
	doctorReviews(root, report)
	if failed {
		return errors.New("doctor found problems; no changes made")
	}
	return nil
}

func selectedEngineForDoctor() (string, error) {
	path, err := enginePath()
	if err != nil {
		return "", err
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return "", fmt.Errorf("unsafe engine setting: %s", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return selectedEngine()
}

func doctorPiSDK(root string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "npm", "root", "-g")
	cmd.Dir = root
	// npm must not create debug logs or run its update notifier during diagnosis.
	cmd.Env = append(os.Environ(), "npm_config_logs_max=0", "npm_config_update_notifier=false")
	cmd.WaitDelay = time.Second
	stdout := &limitedCapture{limit: maxNpmOutputBytes, cancel: cancel}
	stderr := &limitedCapture{limit: maxEngineErrorBytes, cancel: cancel}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("locate Pi SDK (npm root -g): %w", err)
	}
	if stdout.exceeded.Load() || stderr.exceeded.Load() {
		return errors.New("npm root -g output exceeds limit")
	}
	modules := strings.TrimSpace(stdout.String())
	if !filepath.IsAbs(modules) || strings.ContainsAny(modules, "\r\n") {
		return errors.New("npm root -g did not return an absolute module directory")
	}
	path := filepath.Join(modules, "@earendil-works", "pi-coding-agent", "dist", "index.js")
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("invalid Pi SDK entry point: %s", path)
	}
	return nil
}

func doctorReviews(root string, report func(string, error, string)) {
	advice := "inspect and restore review files/links from a trusted copy manually; do not delete or rewrite history automatically"
	base := filepath.Join(root, "prop-review-tmp")
	info, err := os.Lstat(base)
	if errors.Is(err, os.ErrNotExist) {
		report("history (no prop-review-tmp yet)", nil, "")
		return
	}
	if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		err = fmt.Errorf("unsafe review base: %s", base)
	}
	report("review base", err, advice)
	if err != nil {
		return
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		report("history", err, advice)
		return
	}
	var paths []string
	safe := true
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "review.") {
			continue
		}
		dir := filepath.Join(base, entry.Name())
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			report(entry.Name(), fmt.Errorf("unsafe review directory: %s", dir), advice)
			safe = false
			continue
		}
		paths = append(paths, filepath.Join(dir, "proposal.txt"))
		for _, file := range reviewFiles {
			path := filepath.Join(dir, file)
			report("Git exclusion "+entry.Name()+"/"+file, checkReviewExcluded(root, path), "review Git ignore rules and tracked files manually")
			info, err := os.Lstat(path)
			if errors.Is(err, os.ErrNotExist) && file != "proposal.txt" {
				continue
			}
			if err == nil && !info.Mode().IsRegular() {
				err = fmt.Errorf("unsafe review file: %s", path)
			}
			if err != nil {
				report(entry.Name()+"/"+file, err, advice)
				safe = false
			}
		}
	}
	// Existing helpers enumerate all reviews: do not let them traverse an unsafe entry.
	if !safe {
		return
	}
	parents := make(map[string]string)
	children := make(map[string][]string)
	for _, path := range paths {
		name := filepath.Base(filepath.Dir(path))
		data, err := localFile(path)
		if err == nil {
			_, err = parse(strings.NewReader(string(data)))
		}
		report(name+" proposal", err, advice)
		if _, err := os.Lstat(filepath.Join(filepath.Dir(path), "proposal.review.txt")); err == nil {
			_, err = readReviewedProposal(path)
			report(name+" review", err, advice)
		}
		parent, err := parentName(path)
		report(name+" parent link", err, advice)
		if err != nil {
			continue
		}
		parents[name] = parent
		if parent != "" {
			children[parent] = append(children[parent], name)
		}
		followup, followErr := localFile(filepath.Join(filepath.Dir(path), "followup.txt"))
		if followErr == nil && (string(followup) != "followup\n" || parent == "") {
			report(name+" followup", errors.New("invalid followup marker or missing parent"), advice)
		}
		restructure, err := reconstructionMarker(path)
		if err == nil && restructure && (parent == "" || followErr == nil) {
			err = errors.New("restructure requires a parent and cannot be a followup")
		}
		report(name+" revision marker", err, advice)
		if parent == "" {
			_, _, err := rootRequest(path)
			if errors.Is(err, os.ErrNotExist) {
				err = errors.New("missing request.txt (legacy root cannot be clarified)")
			}
			report(name+" root metadata", err, advice)
		} else {
			for _, file := range []string{"request.txt", "issue.json"} {
				if _, err := os.Lstat(filepath.Join(filepath.Dir(path), file)); err == nil {
					report(name+" metadata", fmt.Errorf("%s belongs to a root proposal", file), advice)
				}
			}
		}
	}
	for _, path := range paths {
		name := filepath.Base(filepath.Dir(path))
		parent, known := parents[name]
		if !known {
			continue
		}
		if parent != "" {
			if _, exists := parents[parent]; !exists {
				report(name+" history", fmt.Errorf("missing or invalid parent: %s", parent), advice)
			}
		}
		if len(children[name]) > 1 {
			report(name+" history", errors.New("ambiguous review branches"), advice)
		}
		seen := map[string]bool{}
		for next := name; next != ""; next = parents[next] {
			if seen[next] {
				report(name+" history", errors.New("cyclic review history"), advice)
				break
			}
			seen[next] = true
		}
		if parent == "" {
			_, _, err := reviewHistory(root, path, false)
			// A newly generated leaf awaiting review is normal, not corruption.
			var missing *os.PathError
			if errors.As(err, &missing) && errors.Is(err, os.ErrNotExist) && filepath.Base(missing.Path) == "proposal.review.txt" && len(children[filepath.Base(filepath.Dir(missing.Path))]) == 0 {
				err = nil
			}
			report(name+" history", err, advice)
		}
	}
	if len(paths) == 0 {
		report("history (no reviews)", nil, "")
	}
}
