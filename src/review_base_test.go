package main

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestUnsafeReviewBase(t *testing.T) {
	for _, kind := range []string{"external symlink", "internal symlink", "dangling symlink", "file"} {
		t.Run(kind, func(t *testing.T) {
			root, _ := initTestRepo(t)
			root, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			target := t.TempDir()
			if kind == "internal symlink" {
				target = filepath.Join(root, "history")
			}
			original := filepath.Join(target, "review.original", "proposal.txt")
			initTestWrite(t, original, render([]item{{topic: "External topic"}}))
			initTestWrite(t, filepath.Join(filepath.Dir(original), "proposal.review.txt"), render([]item{{topic: "External topic", answer: "Revise this", comment: true}}))
			initTestWrite(t, filepath.Join(filepath.Dir(original), "request.txt"), "PRIVATE EXTERNAL REQUEST")
			base := filepath.Join(root, "prop-review-tmp")
			if kind == "file" {
				initTestWrite(t, base, "not a directory")
			} else {
				link := target
				if kind == "dangling symlink" {
					link = filepath.Join(target, "missing")
				}
				if err := os.Symlink(link, base); err != nil {
					t.Fatal(err)
				}
			}
			selected := filepath.Join(base, "review.original", "proposal.txt")
			marker := filepath.Join(t.TempDir(), "engine-input")
			bin := t.TempDir()
			for _, name := range []string{"fx", "pi", "npm", "node", "gh"} {
				path := filepath.Join(bin, name)
				initTestWrite(t, path, "#!/bin/sh\nprintf 'started %s\\n' \"$*\" >> \"$ENGINE_INPUT\"\ncat >> \"$ENGINE_INPUT\"\nexit 1\n")
				if err := os.Chmod(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("ENGINE_INPUT", marker)
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			before, externalBefore := initTestTree(t, root), initTestTree(t, target)
			type baseCheck struct {
				name string
				run  func() error
			}
			checks := []baseCheck{
				{"list", func() error { _, err := proposalPaths(root); return err }},
				{"resolve local", func() error { _, err := resolveRevisionPath(root, selected); return err }},
				{"resolve external", func() error { _, err := resolveRevisionPath(root, original); return err }},
				{"history", func() error { _, _, err := clarificationHistory(root, selected); return err }},
				{"reconstruction", func() error { _, err := reconstructionPrompt(root, selected); return err }},
				{"save proposal", func() error {
					_, err := saveProposalWithMetadata(root, []item{{topic: "New"}}, "", "request", false)
					return err
				}},
				{"save review", func() error {
					return saveReview(filepath.Join(filepath.Dir(selected), "new.review.txt"), []item{{topic: "New"}})
				}},
				{"generate", func() error { return generate(false, "fx", "") }},
				{"revise latest", func() error { return generate(true, "fx", "") }},
				{"revise explicit", func() error { return generate(true, "fx", selected) }},
				{"issue", func() error { return generateIssue("fx") }},
				{"clarify", func() error { _, _, err := clarificationQuestions(root, selected, "fx"); return err }},
				{"spec", func() error { return saveSpecWithConfirmation("fx", strings.NewReader("y\n"), io.Discard) }},
				{"init", func() error { return initRepository(root) }},
				{"delete", func() error { _, err := deletionReferences(root); return err }},
			}
			for _, engine := range []string{"fx", "pi"} {
				checks = append(checks, baseCheck{"engine " + engine, func() error {
					_, err := engineResponse(root, "PRIVATE EXTERNAL REQUEST", engine, time.Second, io.Discard, "test")
					return err
				}}, baseCheck{"ingest " + engine, func() error { return ingest(root, engine) }})
			}
			for _, check := range checks {
				t.Run(check.name, func(t *testing.T) {
					if err := check.run(); err == nil || !strings.Contains(err.Error(), "unsafe review directory: "+base) {
						t.Fatalf("expected unsafe base rejection, got %v", err)
					}
				})
			}
			if data, err := os.ReadFile(marker); !os.IsNotExist(err) {
				t.Fatalf("engine launched or request forwarded: %q, %v", data, err)
			}
			if !reflect.DeepEqual(before, initTestTree(t, root)) || !reflect.DeepEqual(externalBefore, initTestTree(t, target)) {
				t.Fatal("rejected operation changed repository or external history")
			}
		})
	}
}

func TestMissingReviewBase(t *testing.T) {
	root, _ := initTestRepo(t)
	if paths, err := proposalPaths(root); err != nil || len(paths) != 0 {
		t.Fatalf("missing history = %v, %v", paths, err)
	}
	if refs, err := deletionReferences(root); err != nil || len(refs) != 0 {
		t.Fatalf("missing deletion references = %v, %v", refs, err)
	}
	path, err := saveProposal(root, []item{{topic: "New"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if paths, err := proposalPaths(root); err != nil || len(paths) != 1 || paths[0] != path {
		t.Fatalf("created history = %v, %v", paths, err)
	}
}
