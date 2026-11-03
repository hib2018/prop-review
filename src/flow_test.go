package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFlowTopicOnlyExplicitTransitions(t *testing.T) {
	want := "┌───────┐\n│ 入力a │\n└───────┘\n  │\n  ↓\n┌──────┐\n│ 判断 │\n└──────┘"
	if got := flowTopic("入力a → 判断"); got != want {
		t.Fatalf("diagram: %q, want %q", got, want)
	}
	for _, topic := range []string{"条件なら保存、それ以外は終了", "→ 未定", "入力 → → 出力"} {
		got := flowTopic(topic)
		if strings.Contains(got, "↓") || !strings.Contains(got, topic) || strings.Count(got, "┌") != 1 {
			t.Fatalf("inferred transition: %s", got)
		}
	}
}

func TestFlowLatestHistoryAndSourceUnchanged(t *testing.T) {
	root, original, prompt := clarificationFixture(t)
	child, err := saveProposalWithMode(root, []item{{topic: "入力 → 判断 → 保存"}, {topic: "新しい未確認"}, {topic: "コメント"}}, original, "", false, true)
	if err != nil {
		t.Fatal(err)
	}
	writeReview(t, child, []item{{topic: "入力 → 判断 → 保存", answer: "Approved"}, {topic: "新しい未確認"}, {topic: "コメント", answer: "Approved", comment: true}})
	before := map[string]string{}
	paths, _ := proposalPaths(root)
	for _, path := range paths {
		entries, _ := os.ReadDir(filepath.Dir(path))
		for _, entry := range entries {
			file := filepath.Join(filepath.Dir(path), entry.Name())
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			before[file] = string(data)
		}
	}
	inFixtureRepo(t, root)
	var preview strings.Builder
	if err := saveFlowWithConfirmation(strings.NewReader("y\n"), &preview); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "prop-review-tmp", "flow.txt"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "承認事項") || !strings.Contains(body, "│ 判断 │") || strings.Count(body, "↓") != 2 || strings.Count(body, "┌") != 4 {
		t.Fatalf("missing approvals or invented links: %s", body)
	}
	for _, forbidden := range []string{"未確認", "要改訂", "コメント", "変更して"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("unapproved content: %s", body)
		}
	}
	if !strings.Contains(preview.String(), body) || strings.Index(preview.String(), body) > strings.Index(preview.String(), "[y/N]") {
		t.Fatalf("missing preview before confirmation: %s", preview.String())
	}
	for file, old := range before {
		data, err := os.ReadFile(file)
		if err != nil || string(data) != old {
			t.Fatalf("source changed: %s, %v", file, err)
		}
	}
	after, err := proposalPaths(root)
	if err != nil || len(after) != len(paths) {
		t.Fatalf("proposals changed: %v, %v", after, err)
	}
	if _, err := os.Stat(prompt); !os.IsNotExist(err) {
		t.Fatalf("flow called an engine: %v", err)
	}
}

func TestFlowRejectsInvalidHistoryAndNoApprovals(t *testing.T) {
	for _, kind := range []string{"no approvals", "mismatch", "unreviewed tip", "branches", "orphan", "cycle", "invalid marker"} {
		t.Run(kind, func(t *testing.T) {
			root, original, _ := clarificationFixture(t)
			switch kind {
			case "no approvals":
				writeReview(t, original, []item{{topic: "承認事項"}, {topic: "未確認"}, {topic: "要改訂", answer: "Approved", comment: true}})
			case "mismatch":
				writeReview(t, original, []item{{topic: "偽の承認", answer: "Approved"}})
			case "unreviewed tip", "branches":
				for i := 0; i < 2; i++ {
					child, err := saveProposal(root, []item{{topic: "未確認"}, {topic: "改訂"}}, original)
					if err != nil {
						t.Fatal(err)
					}
					if kind == "unreviewed tip" {
						break
					}
					writeReview(t, child, []item{{topic: "未確認"}, {topic: "改訂", answer: "Approved"}})
				}
			case "orphan", "cycle":
				parent := "review.missing"
				if kind == "cycle" {
					parent = filepath.Base(filepath.Dir(original))
				}
				if err := os.WriteFile(filepath.Join(filepath.Dir(original), "parent.txt"), []byte(parent+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "invalid marker":
				if err := os.WriteFile(filepath.Join(filepath.Dir(original), "followup.txt"), []byte("bad\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			inFixtureRepo(t, root)
			var preview strings.Builder
			err := saveFlowWithConfirmation(strings.NewReader("y\n"), &preview)
			if err == nil || err.Error() == "" || strings.Contains(preview.String(), "[y/N]") {
				t.Fatalf("invalid history offered for saving: %v, %s", err, preview.String())
			}
			if _, err := os.Stat(filepath.Join(root, "prop-review-tmp", "flow.txt")); !os.IsNotExist(err) {
				t.Fatalf("saved invalid flow: %v", err)
			}
		})
	}
}

func TestFlowConfirmationAndUnsafeDestination(t *testing.T) {
	root, _, _ := clarificationFixture(t)
	inFixtureRepo(t, root)
	path := filepath.Join(root, "prop-review-tmp", "flow.txt")
	if err := saveFlowWithConfirmation(strings.NewReader("\n"), io.Discard); err == nil {
		t.Fatal("created without approval")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("cancelled create wrote file")
	}
	if err := os.WriteFile(path, []byte("old flow"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := saveFlowWithConfirmation(strings.NewReader("n\n"), io.Discard); err == nil {
		t.Fatal("overwrote without approval")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "old flow" {
		t.Fatal("cancelled overwrite changed file")
	}
	if err := saveFlowWithConfirmation(strings.NewReader("Y\n"), io.Discard); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.Rename(path, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := saveFlowWithConfirmation(strings.NewReader("y\n"), io.Discard); err == nil {
		t.Fatal("accepted symlink")
	}
}

type flowConfirmationReader struct {
	change func()
}

func (r flowConfirmationReader) Read(p []byte) (int, error) {
	r.change()
	return copy(p, "y\n"), io.EOF
}

func TestFlowRevalidatesAfterPreview(t *testing.T) {
	for _, change := range []string{"review", "destination", "overwrite", "deleted", "symlink", "directory", "invalid history"} {
		t.Run(change, func(t *testing.T) {
			root, original, _ := clarificationFixture(t)
			inFixtureRepo(t, root)
			path := filepath.Join(root, "prop-review-tmp", "flow.txt")
			if change == "overwrite" || change == "deleted" {
				if err := os.WriteFile(path, []byte("old flow"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			target := filepath.Join(root, "target")
			if err := os.WriteFile(target, []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
			reader := flowConfirmationReader{change: func() {
				var err error
				switch change {
				case "review":
					writeReview(t, original, []item{{topic: "承認事項"}, {topic: "未確認", answer: "Approved"}, {topic: "要改訂"}})
				case "invalid history":
					err = os.Remove(filepath.Join(filepath.Dir(original), "proposal.review.txt"))
				case "deleted":
					err = os.Remove(path)
				case "symlink":
					err = os.Symlink(target, path)
				case "directory":
					err = os.Mkdir(path, 0700)
				default:
					err = os.WriteFile(path, []byte("concurrent change"), 0600)
				}
				if err != nil {
					t.Fatal(err)
				}
			}}
			if err := saveFlowWithConfirmation(reader, io.Discard); err == nil {
				t.Fatal("saved stale preview")
			}
			data, err := os.ReadFile(path)
			switch change {
			case "review", "invalid history", "deleted":
				if !os.IsNotExist(err) {
					t.Fatalf("saved stale flow: %s, %v", data, err)
				}
			case "destination", "overwrite":
				if err != nil || string(data) != "concurrent change" {
					t.Fatalf("lost concurrent change: %s, %v", data, err)
				}
			case "symlink", "directory":
				info, err := os.Lstat(path)
				if err != nil || change == "symlink" && info.Mode()&os.ModeSymlink == 0 || change == "directory" && !info.IsDir() {
					t.Fatalf("unsafe destination replaced: %v, %v", info, err)
				}
			}
			if data, err := os.ReadFile(target); err != nil || string(data) != "untouched" {
				t.Fatalf("symlink target changed: %s, %v", data, err)
			}
		})
	}
}

func TestFlowIndependentChainsAndDrafts(t *testing.T) {
	root, _, _ := clarificationFixture(t)
	other, err := saveProposal(root, []item{{topic: "独立した承認"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	writeReview(t, other, []item{{topic: "独立した承認", answer: "Approved"}})
	if _, err := saveProposal(root, []item{{topic: "未レビュー草案"}}, ""); err != nil {
		t.Fatal(err)
	}
	body, err := approvedFlow(root)
	if err != nil || !strings.Contains(body, "承認事項") || !strings.Contains(body, "独立した承認") || strings.Contains(body, "未レビュー草案") || strings.Contains(body, "↓") {
		t.Fatalf("independent items linked or missing: %s, %v", body, err)
	}
	if _, err := approvedFlow(t.TempDir()); err == nil || !strings.Contains(err.Error(), "no approved items") {
		t.Fatalf("empty repository: %v", err)
	}
}

func TestFlowCommandUsage(t *testing.T) {
	if err := run([]string{"flow", "extra"}); err == nil || err.Error() != "usage: prop-review flow" {
		t.Fatalf("usage: %v", err)
	}
}
