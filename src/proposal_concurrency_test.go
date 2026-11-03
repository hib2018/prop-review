package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestConcurrentProposalGeneration(t *testing.T) {
	for _, mode := range []string{"revision", "followup", "mixed"} {
		t.Run(mode, func(t *testing.T) {
			root, original, _ := clarificationFixture(t)
			if mode != "revision" {
				writeReview(t, original, []item{{topic: "承認事項", answer: "Approved"}, {topic: "未確認"}, {topic: "要改訂"}})
			}
			history, request, err := clarificationHistory(root, original)
			if err != nil {
				t.Fatal(err)
			}
			prompt, err := reconstructionPromptFromHistory(history, request)
			if err != nil {
				t.Fatal(err)
			}
			before := initTestTree(t, filepath.Dir(original))
			t.Setenv("CLARIFY_OUTPUT", fxJSON(t, `["generated question"]`))
			type result struct {
				path string
				err  error
			}
			const workers = 6
			start := make(chan struct{})
			results := make(chan result, workers)
			for i := range workers {
				go func() {
					<-start
					var path string
					var err error
					if mode == "mixed" && i%2 == 0 {
						var items []item
						items, err = proposalsWithActivity(root, prompt, "fx", 100*time.Second, io.Discard, false, "test")
						if err == nil {
							path, err = saveProposalWithMode(root, items, original, "", false, true)
						}
					} else {
						path, err = saveClarificationFromHistory(root, history, request, "fx", io.Discard)
					}
					results <- result{path, err}
				}()
			}
			close(start)
			var winner string
			for range workers {
				got := <-results
				if got.err == nil {
					if got.path == "" || winner != "" {
						t.Fatalf("multiple or empty successes: %q, %q", winner, got.path)
					}
					winner = got.path
				} else if got.path != "" || (!strings.Contains(got.err.Error(), "proposal save locked") && !strings.Contains(got.err.Error(), "no longer the latest")) {
					t.Errorf("unexpected failure: %q, %v", got.path, got.err)
				}
			}
			paths, err := proposalPaths(root)
			if err != nil || len(paths) != 2 || winner == "" {
				t.Fatalf("proposals: %v, winner %q, error %v", paths, winner, err)
			}
			if !reflect.DeepEqual(before, initTestTree(t, filepath.Dir(original))) {
				t.Fatal("concurrent generation changed the parent")
			}
			childBefore := initTestTree(t, filepath.Dir(winner))
			if path, err := saveClarificationFromHistory(root, history, request, "fx", io.Discard); err == nil || path != "" || !strings.Contains(err.Error(), "no longer the latest") {
				t.Fatalf("stale generation: %q, %v", path, err)
			}
			if !reflect.DeepEqual(childBefore, initTestTree(t, filepath.Dir(winner))) {
				t.Fatal("stale generation changed the winning child")
			}
			entries, err := os.ReadDir(filepath.Join(root, "prop-review-tmp"))
			if err != nil || len(entries) != 2 {
				t.Fatalf("save left extra directories: %v, %v", entries, err)
			}
			writeReview(t, winner, clarifyRestructureItems(t, winner))
			after, _, err := clarificationHistory(root, original)
			if err != nil || len(after) != 2 || after[1].path != winner {
				t.Fatalf("history unusable after conflict: %v, %v", after, err)
			}
		})
	}
}

func TestProposalSaveProcessWorker(t *testing.T) {
	root := os.Getenv("PROP_REVIEW_SAVE_TEST_ROOT")
	if root == "" {
		t.Skip("subprocess helper")
	}
	_, err := saveProposalWithMode(root, []item{{topic: "child"}}, os.Getenv("PROP_REVIEW_SAVE_TEST_PARENT"), "", false, true)
	if err != nil {
		t.Fatal(err)
	}
}

func TestProposalSaveAcrossProcesses(t *testing.T) {
	root, original, _ := clarificationFixture(t)
	before := initTestTree(t, filepath.Dir(original))
	const workers = 6
	results := make(chan bool, workers)
	for range workers {
		go func() {
			cmd := exec.Command(os.Args[0], "-test.run=^TestProposalSaveProcessWorker$")
			cmd.Env = append(os.Environ(), "PROP_REVIEW_SAVE_TEST_ROOT="+root, "PROP_REVIEW_SAVE_TEST_PARENT="+original)
			output, err := cmd.CombinedOutput()
			if err != nil && !strings.Contains(string(output), "proposal save locked") && !strings.Contains(string(output), "no longer the latest") {
				t.Errorf("unexpected process failure: %v: %s", err, output)
			}
			results <- err == nil
		}()
	}
	successes := 0
	for range workers {
		if <-results {
			successes++
		}
	}
	paths, err := proposalPaths(root)
	if successes != 1 || err != nil || len(paths) != 2 {
		t.Fatalf("successes %d, proposals %v, error %v", successes, paths, err)
	}
	if !reflect.DeepEqual(before, initTestTree(t, filepath.Dir(original))) {
		t.Fatal("concurrent processes changed the parent")
	}
}

func TestProposalSaveLockAndInvalidParent(t *testing.T) {
	root, original, _ := clarificationFixture(t)
	base := filepath.Join(root, "prop-review-tmp")
	lock := filepath.Join(base, ".save.lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	before := initTestTree(t, base)
	if path, err := saveProposal(root, []item{{topic: "child"}}, original); err == nil || path != "" || !strings.Contains(err.Error(), "proposal save locked") {
		t.Fatalf("ignored existing lock: %q, %v", path, err)
	}
	if !reflect.DeepEqual(before, initTestTree(t, base)) {
		t.Fatal("locked save changed existing files or lock")
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	for _, parent := range []string{filepath.Join(base, "review.missing", "proposal.txt"), filepath.Join(base, "review.", "proposal.txt"), filepath.Join(root, "outside", "proposal.txt")} {
		if path, err := saveProposal(root, []item{{topic: "child"}}, parent); err == nil || path != "" {
			t.Fatalf("accepted invalid parent %s: %q, %v", parent, path, err)
		}
		if _, err := os.Stat(lock); !os.IsNotExist(err) {
			t.Fatalf("failed save retained lock: %v", err)
		}
	}
	if _, err := saveProposal(root, []item{{topic: "child"}}, original); err != nil {
		t.Fatalf("save after lock release: %v", err)
	}
}
