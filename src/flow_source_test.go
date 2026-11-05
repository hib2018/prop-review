package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func flowSourceTempGit(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	flowSourceGit(t, root, "init", "-q")
	return root
}

func flowSourceGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func flowSourceWrite(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func flowSourceDecode(t *testing.T, root string) (flowSourceInput, []byte) {
	t.Helper()
	data, err := flowSourceContext(root)
	if err != nil {
		t.Fatal(err)
	}
	var input flowSourceInput
	if err := json.Unmarshal(data, &input); err != nil {
		t.Fatal(err)
	}
	if len(data) > flowSourceTotalLimit || len(input.Files) > flowSourceFileCount {
		t.Fatalf("limits exceeded: %d bytes, %d files", len(data), len(input.Files))
	}
	for _, f := range input.Files {
		if len(f.Content) > flowSourceFileLimit {
			t.Fatalf("file limit exceeded: %s", f.Path)
		}
	}
	return input, data
}

func TestFlowSourceWorkingTreeAndExclusions(t *testing.T) {
	root := flowSourceTempGit(t)
	flowSourceWrite(t, root, "main.go", "package main // indexed")
	flowSourceWrite(t, root, ".gitignore", "ignored/\n")
	flowSourceGit(t, root, "add", "main.go", ".gitignore")
	flowSourceWrite(t, root, "main.go", "package main // dirty working tree")
	flowSourceWrite(t, root, "src/cli.ts", "export const run = () => 1")
	flowSourceWrite(t, root, "README.md", "actual command execution")
	flowSourceWrite(t, root, "package.json", `{"scripts":{"start":"node main.js"}}`)
	for _, name := range []string{
		"ignored/main.go", "ignored/secret.go", ".env", ".hidden/main.go", "prop-review-tmp/main.go",
		"reviewdata/main.go", "fixtures/main.go", "testdata/main.go", "tests/main.go",
		"vendor/main.go", "node_modules/main.go", "config/main.go", "secrets.go",
		"credentials.json", "config.json", "other.json", "README.json", "main_test.go", "main.spec.ts",
		"config.go", "settings.py", "binary.go", "invalid.go",
	} {
		flowSourceWrite(t, root, name, "DO NOT SEND")
	}
	flowSourceWrite(t, root, "binary.go", "before\x00after")
	flowSourceWrite(t, root, "invalid.go", "before\xffafter")
	flowSourceGit(t, root, "add", "-f", ".env", "prop-review-tmp/main.go", "ignored/main.go")

	// Include index and config in the byte-for-byte no-mutation check.
	before := map[string][]byte{}
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			before[path] = b
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	input, data := flowSourceDecode(t, root)
	got := map[string]string{}
	for _, f := range input.Files {
		got[f.Path] = f.Content
	}
	want := map[string]string{
		"README.md":       "actual command execution",
		"package.json":    `{"scripts":{"start":"node main.js"}}`,
		"main.go":         "package main // dirty working tree",
		"src/cli.ts":      "export const run = () => 1",
		"ignored/main.go": "DO NOT SEND", // tracked files remain Git candidates even when ignored
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %#v, want %#v", got, want)
	}
	if input.Omitted == 0 || !bytes.Contains(data, []byte(`"omitted":`)) {
		t.Fatal("missing omission count")
	}
	for path, expected := range before {
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("file changed: %s (%v)", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".git", "index.lock")); !os.IsNotExist(err) {
		t.Fatalf("index.lock: %v", err)
	}
}

func TestFlowSourceSymlinksAndRootNormalization(t *testing.T) {
	root := flowSourceTempGit(t)
	flowSourceWrite(t, root, "README.md", "safe")
	flowSourceWrite(t, root, "src/nested/main.go", "original")
	flowSourceGit(t, root, "add", ".")
	outside := t.TempDir()
	flowSourceWrite(t, outside, "nested/main.go", "EXTERNAL SECRET")
	if err := os.RemoveAll(filepath.Join(root, "src")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "src")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "nested/main.go"), filepath.Join(root, "main.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("README.md", filepath.Join(root, "cli.go")); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "repo")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	input, data := flowSourceDecode(t, alias)
	if len(input.Files) != 1 || input.Files[0].Path != "README.md" || bytes.Contains(data, []byte("SECRET")) {
		t.Fatalf("unsafe contents: %s", data)
	}
	if input.Omitted != 4 {
		t.Fatalf("omitted = %d, want 4", input.Omitted)
	}
}

func TestFlowSourcePriorityAndFileCount(t *testing.T) {
	root := flowSourceTempGit(t)
	for i := 0; i < 50; i++ {
		flowSourceWrite(t, root, fmt.Sprintf("aaa/lib%02d.go", i), "package lib")
	}
	for _, name := range []string{"README.md", "package.json", "go.mod", "z/main.go", "z/cli.ts", "z/commands.py", "src/top.go"} {
		flowSourceWrite(t, root, name, name)
	}
	input, _ := flowSourceDecode(t, root)
	var first []string
	for _, f := range input.Files[:7] {
		first = append(first, f.Path)
	}
	want := []string{"README.md", "go.mod", "package.json", "z/cli.ts", "z/commands.py", "z/main.go", "src/top.go"}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("priority = %v, want %v", first, want)
	}
	if len(input.Files) != 32 || input.Omitted != 25 {
		t.Fatalf("counts: %+v", input)
	}
}

func TestFlowSourcePrioritizesTUIEntryAndInteractionCode(t *testing.T) {
	root := flowSourceTempGit(t)
	for i := 0; i < 50; i++ {
		flowSourceWrite(t, root, fmt.Sprintf("aaa/lib%02d.go", i), "package lib")
	}
	important := []string{"README.ja.md", "src/app.rs", "internal/tui/model.go", "internal/tui/update.go", "internal/tui/view.go", "pkg/events/dispatch.go", "ui/screens/list.tsx", "ui/keybindings.ts"}
	for _, name := range important {
		flowSourceWrite(t, root, name, "TUI interaction code")
	}
	input, _ := flowSourceDecode(t, root)
	seen := map[string]bool{}
	for _, file := range input.Files {
		seen[file.Path] = true
	}
	for _, name := range important {
		if !seen[name] {
			t.Errorf("missed priority TUI file: %s", name)
		}
	}
	if flowSourcePriority("src/main.go") >= flowSourcePriority("internal/tui/update.go") || flowSourcePriority("internal/tui/update.go") >= flowSourcePriority("aaa/lib00.go") {
		t.Fatal("expected entry points before TUI handlers, and handlers before generic library code")
	}
}

func TestFlowSourceByteLimits(t *testing.T) {
	root := flowSourceTempGit(t)
	flowSourceWrite(t, root, "README.md", strings.Repeat("r", flowSourceFileLimit))
	flowSourceWrite(t, root, "oversized.go", strings.Repeat("x", flowSourceFileLimit+1))
	for i := 0; i < 10; i++ {
		// HTML/JSON escaping must count against the serialized budget too.
		flowSourceWrite(t, root, fmt.Sprintf("src/file%02d.go", i), strings.Repeat("<", flowSourceFileLimit))
	}
	flowSourceWrite(t, root, "z.go", "small")
	input, data := flowSourceDecode(t, root)
	if input.Omitted == 0 {
		t.Fatal("expected omitted oversized and over-budget files")
	}
	if len(input.Files) != 2 || input.Files[0].Path != "README.md" || input.Files[1].Path != "z.go" {
		t.Fatalf("expected skipped oversized inputs and later small file; paths = %v; bytes = %d", input.Files, len(data))
	}
}

func TestFlowSourceEmptyAndGitFailure(t *testing.T) {
	for _, setup := range []string{"empty", "excluded", "binary", "oversized"} {
		t.Run(setup, func(t *testing.T) {
			root := flowSourceTempGit(t)
			switch setup {
			case "excluded":
				flowSourceWrite(t, root, ".env", "secret")
			case "binary":
				flowSourceWrite(t, root, "main.go", "\x00")
			case "oversized":
				flowSourceWrite(t, root, "main.go", strings.Repeat("x", flowSourceFileLimit+1))
			}
			if _, err := flowSourceContext(root); err == nil || !strings.Contains(err.Error(), "no readable source context") {
				t.Fatalf("error = %v", err)
			}
		})
	}
	if _, err := flowSourceContext(t.TempDir()); err == nil || !strings.Contains(err.Error(), "git ls-files") {
		t.Fatalf("git failure = %v", err)
	}
}

func TestFlowSourceRejectsUnsafeNames(t *testing.T) {
	for _, name := range []string{"../main.go", "/main.go", "a/../../main.go", "a//main.go", "a/./main.go", "a\\main.go", "bad\xff.go", "bad\n.go", ".git/main.go", "x/.env/main.go", "data.json", "README.json"} {
		if flowSourceAllowed(name) {
			t.Errorf("accepted %q", name)
		}
	}
}

func TestFlowSourceIgnoresAmbientGitRedirection(t *testing.T) {
	root := flowSourceTempGit(t)
	flowSourceWrite(t, root, "main.go", "local source")
	other := flowSourceTempGit(t)
	flowSourceWrite(t, other, "external.go", "external source")
	flowSourceGit(t, other, "add", ".")
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(other, ".git", "index"))
	input, _ := flowSourceDecode(t, root)
	if len(input.Files) != 1 || input.Files[0].Path != "main.go" || input.Files[0].Content != "local source" {
		t.Fatalf("read redirected repository: %+v", input)
	}
}

func TestFlowSourceListingTimeout(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, err := flowSourceContext(t.TempDir()); err == nil || !strings.Contains(err.Error(), "listing timeout") {
		t.Fatalf("timeout error = %v", err)
	}
}

func TestFlowSourceListingBound(t *testing.T) {
	bin := t.TempDir()
	// A fake Git exercises the output cap without allocating a huge repository.
	script := "#!/bin/sh\nexec /usr/bin/head -c 1048577 /dev/zero\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, err := flowSourceContext(t.TempDir()); err == nil || !strings.Contains(err.Error(), "listing exceeds") {
		t.Fatalf("listing limit error = %v", err)
	}
}
