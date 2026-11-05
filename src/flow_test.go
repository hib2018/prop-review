package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func commandFlowFixture(t *testing.T) (string, string) {
	t.Helper()
	root := flowSourceTempGit(t)
	flowSourceWrite(t, root, "README.ja.md", "generate: 依頼から提案を作成。revise: 履歴から改訂。")
	flowSourceWrite(t, root, "src/main.go", "package main\nfunc main() {} // command entry point")
	flowSourceGit(t, root, "add", "README.ja.md", "src/main.go")
	bin := t.TempDir()
	prompt := filepath.Join(bin, "prompt")
	script := "#!/bin/sh\ncat > \"$FLOW_PROMPT\"\nprintf '%s' \"$FLOW_REPLY\"\n"
	if err := os.WriteFile(filepath.Join(bin, "fx"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FLOW_PROMPT", prompt)
	t.Setenv("FLOW_REPLY", fxJSON(t, commandFlowExample))
	t.Setenv("PROP_REVIEW_FLOW_MODEL", "")
	t.Setenv("PROP_REVIEW_FLOW_THINKING", "")
	inFixtureRepo(t, root)
	return root, prompt
}

func TestFlowCommandsFromCodeWithoutReviewHistory(t *testing.T) {
	for _, engine := range []string{"fx", "pi"} {
		t.Run(engine, func(t *testing.T) {
			root, prompt := commandFlowFixture(t)
			flowSourceWrite(t, root, "prop-review-tmp/review.1/proposal.txt", "do not send review text")
			flowSourceWrite(t, root, ".env", "DO_NOT_SEND_SECRET")
			before, err := flowSourceContext(root)
			if err != nil {
				t.Fatal(err)
			}
			if engine == "pi" {
				bin := filepath.Dir(prompt)
				for name, script := range map[string]string{
					"npm":  "#!/bin/sh\nprintf '/unused/modules\\n'\n",
					"node": "#!/bin/sh\ncat > \"$FLOW_PROMPT\"\nprintf '%s' \"$FLOW_JSON\"\n",
				} {
					if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
						t.Fatal(err)
					}
				}
				t.Setenv("FLOW_JSON", commandFlowExample)
			}
			var out, progress strings.Builder
			if err := showFlow(engine, &out, &progress); err != nil {
				t.Fatal(err)
			}
			for _, label := range []string{"generate", "依頼を入力", "AIが提案を生成", "revise", "レビューは別途開始", "┌", "↓"} {
				if !strings.Contains(out.String(), label) {
					t.Errorf("missing %q: %s", label, out.String())
				}
			}
			for _, forbidden := range []string{"sources:", "Discussion", "history=", "request_bytes=", "[y/N]"} {
				if strings.Contains(out.String()+progress.String(), forbidden) {
					t.Errorf("old output remains: %s", forbidden)
				}
			}
			data, err := os.ReadFile(prompt)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "command entry point") || !strings.Contains(string(data), "3..6 short") || strings.Contains(string(data), "DO_NOT_SEND_SECRET") || strings.Contains(string(data), "do not send review text") {
				t.Fatalf("unexpected prompt context: %s", data)
			}
			after, err := flowSourceContext(root)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("source changed: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, "prop-review-tmp", "flow.txt")); !os.IsNotExist(err) {
				t.Fatalf("flow wrote a file: %v", err)
			}
		})
	}
}

func TestFlowTUIBranchesFromSource(t *testing.T) {
	root, prompt := commandFlowFixture(t)
	flowSourceWrite(t, root, "README.ja.md", "TUI: 一覧で項目を選択しEnterで開く。成功なら詳細、失敗ならエラーを表示。")
	flowSourceWrite(t, root, "src/main.go", "package main // start TUI event loop")
	flowSourceWrite(t, root, "internal/tui/model.go", "package tui\nfunc Update(key string) { if key == \"KeyEnter\" { /* fetch detail, show detail on success, error on failure */ } }")
	t.Setenv("FLOW_REPLY", fxJSON(t, `[{"command":"項目を開く","steps":["一覧で選択","Enter入力","詳細を取得","取得成功？"],"branches":[{"label":"成功","steps":["詳細を表示"]},{"label":"失敗","steps":["エラーを表示"]}]}]`))
	var out strings.Builder
	if err := showFlow("fx", &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"操作・処理フロー", "項目を開く", "取得成功？", "成功", "失敗", "詳細を表示", "エラーを表示", "┴", "↓"} {
		if !strings.Contains(out.String(), text) {
			t.Errorf("missing TUI branch %q: %s", text, out.String())
		}
	}
	data, err := os.ReadFile(prompt)
	if err != nil || !strings.Contains(string(data), "internal/tui/model.go") || !strings.Contains(string(data), "KeyEnter") || !strings.Contains(string(data), "Do not assume a CLI") || !strings.Contains(string(data), "2..3 branches") {
		t.Fatalf("TUI prompt: %s, %v", data, err)
	}
	t.Log("\n" + out.String())
}

func TestFlowStopsWithoutCodeBeforeEngine(t *testing.T) {
	root, prompt := commandFlowFixture(t)
	if err := os.Remove(filepath.Join(root, "README.ja.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "src/main.go")); err != nil {
		t.Fatal(err)
	}
	if err := showFlow("fx", io.Discard, io.Discard); err == nil {
		t.Fatal("accepted empty source context")
	}
	if _, err := os.Stat(prompt); !os.IsNotExist(err) {
		t.Fatalf("engine ran without source context: %v", err)
	}
}

func TestFlowRejectsUnsafeCommandResponse(t *testing.T) {
	for _, body := range []string{"", "[]", `[{"command":"x","steps":["\u001b[2J"]}]`, `[{"command":"x","steps":["\u202e"]}]`, strings.Repeat("x", maxFlowResponse+1)} {
		name := body
		if len(name) > 80 {
			name = "oversized"
		}
		t.Run(name, func(t *testing.T) {
			_, prompt := commandFlowFixture(t)
			file := filepath.Join(t.TempDir(), "response")
			if err := os.WriteFile(file, []byte(fxJSON(t, body)), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("FLOW_RESPONSE", file)
			if err := os.WriteFile(filepath.Join(filepath.Dir(prompt), "fx"), []byte("#!/bin/sh\ncat >/dev/null\ncat \"$FLOW_RESPONSE\"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			var out strings.Builder
			if err := showFlow("fx", &out, io.Discard); err == nil || out.Len() != 0 {
				t.Fatalf("invalid response displayed: %v, %q", err, out.String())
			}
		})
	}
}

func TestFlowLeavesExistingDiagramsAlone(t *testing.T) {
	for _, kind := range []string{"file", "symlink", "directory"} {
		t.Run(kind, func(t *testing.T) {
			root, _ := commandFlowFixture(t)
			base := filepath.Join(root, "prop-review-tmp")
			if err := os.Mkdir(base, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(base, "flow.txt")
			target := filepath.Join(t.TempDir(), "target")
			if err := os.WriteFile(target, []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "file":
				err = os.WriteFile(path, []byte("untouched"), 0600)
			case "symlink":
				err = os.Symlink(target, path)
			case "directory":
				err = os.Mkdir(path, 0700)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := showFlow("fx", io.Discard, io.Discard); err != nil {
				t.Fatal(err)
			}
			if kind != "directory" {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "untouched" {
					t.Fatalf("old diagram changed: %q, %v", data, err)
				}
			} else if info, err := os.Stat(path); err != nil || !info.IsDir() {
				t.Fatalf("directory replaced: %v, %v", info, err)
			}
		})
	}
}

func TestFlowRejectsSourceChangesDuringGeneration(t *testing.T) {
	root, prompt := commandFlowFixture(t)
	t.Setenv("FLOW_SOURCE", filepath.Join(root, "src/main.go"))
	script := "#!/bin/sh\ncat >/dev/null\nprintf 'package main // changed' > \"$FLOW_SOURCE\"\nprintf '%s' \"$FLOW_REPLY\"\n"
	if err := os.WriteFile(filepath.Join(filepath.Dir(prompt), "fx"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := showFlow("fx", &out, io.Discard); err == nil || out.Len() != 0 {
		t.Fatalf("stale source diagram: %v, %q", err, out.String())
	}
}

func TestFlowEscapesEngineFailure(t *testing.T) {
	_, prompt := commandFlowFixture(t)
	if err := os.WriteFile(filepath.Join(filepath.Dir(prompt), "fx"), []byte("#!/bin/sh\ncat >/dev/null\nprintf '\\033[2Jfailed' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	err := showFlow("fx", io.Discard, io.Discard)
	if err == nil || strings.ContainsRune(err.Error(), '\x1b') {
		t.Fatalf("unsafe engine error: %v", err)
	}
}

type flowErrorWriter struct{}

func (flowErrorWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestFlowWriteFailure(t *testing.T) {
	commandFlowFixture(t)
	if err := showFlow("fx", flowErrorWriter{}, io.Discard); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write error lost: %v", err)
	}
}

func TestFlowCommandWithoutTTY(t *testing.T) {
	commandFlowFixture(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := setEngine("fx"); err != nil {
		t.Fatal(err)
	}
	out, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	old := os.Stdout
	os.Stdout = out
	defer func() { os.Stdout = old }()
	if err := run([]string{"flow"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out.Name())
	if err != nil || !strings.Contains(string(data), "generate\n┌") || !strings.Contains(string(data), "↓") {
		t.Fatalf("CLI output: %q, %v", data, err)
	}
}

func TestFlowCommandUsage(t *testing.T) {
	if err := run([]string{"flow", "extra"}); err == nil || err.Error() != "usage: prop-review flow" {
		t.Fatalf("usage: %v", err)
	}
}

func TestFlowPromptContract(t *testing.T) {
	data, err := json.Marshal(flowSourceInput{Files: []flowSourceFile{{Path: "main.go", Content: "package main"}}})
	if err != nil || len(data) == 0 || !strings.Contains(flowPrompt, "Go will draw every step in a box") {
		t.Fatalf("prompt contract: %v", err)
	}
}
