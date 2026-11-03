package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEngineDiagnosticsEscapeTerminalControls(t *testing.T) {
	for _, mode := range []string{"npm failure", "fx failure", "pi failure", "fx overflow", "pi overflow"} {
		t.Run(mode, func(t *testing.T) {
			bin := t.TempDir()
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			diagnostic := "診断\x1b[2J\x1b]0;forged title\a\rhidden"
			if strings.HasSuffix(mode, "overflow") {
				diagnostic += strings.Repeat("x", maxEngineErrorBytes)
			}
			t.Setenv("UNSAFE_DIAGNOSTIC", diagnostic)
			failure := "#!/bin/sh\nprintf '%s' \"$UNSAFE_DIAGNOSTIC\" >&2\nexit 1\n"
			npm := "#!/bin/sh\nprintf '/fixture/modules\\n'\n"
			if mode == "npm failure" {
				npm = failure
			}
			for name, script := range map[string]string{"fx": failure, "node": failure, "npm": npm} {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
			}
			engine := "pi"
			if strings.HasPrefix(mode, "fx") {
				engine = "fx"
			}
			_, err := engineResponse(t.TempDir(), "prompt", engine, 3*time.Second, io.Discard, "Working")
			if err == nil {
				t.Fatal("expected engine failure")
			}
			captured := diagnostic
			if strings.HasSuffix(mode, "overflow") {
				captured = captured[:maxEngineErrorBytes]
				if !strings.Contains(err.Error(), "stderr exceeds") {
					t.Fatalf("expected stderr limit: %v", err)
				}
			}
			if strings.ContainsAny(err.Error(), "\x1b\a\r") || !strings.Contains(err.Error(), strconv.Quote(captured)) {
				t.Fatalf("unsafe or missing diagnostic: %.120q", err.Error())
			}
		})
	}
}

func TestProposalLabelEscapesTerminalControlsWithoutChangingFiles(t *testing.T) {
	root, proposal, _ := clarificationFixture(t)
	inFixtureRepo(t, root)
	request := "依頼\x1b[2J\x1b]0;forged title\a\rhidden\nOriginal context"
	requestPath := filepath.Join(filepath.Dir(proposal), "request.txt")
	if err := os.WriteFile(requestPath, []byte(request), 0600); err != nil {
		t.Fatal(err)
	}
	files := []string{requestPath, proposal, filepath.Join(filepath.Dir(proposal), "proposal.review.txt")}
	before := make(map[string]string)
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = string(data)
	}
	for i, selectFn := range []func([]string, io.Reader, io.Writer) (string, error){selectProposal, selectClarificationRoot} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			var out strings.Builder
			selected, err := selectFn([]string{proposal}, strings.NewReader("1\n"), &out)
			if err != nil || selected != proposal {
				t.Fatalf("selection: %q, %v", selected, err)
			}
			label := strings.SplitN(request, "\n", 2)[0]
			line := strings.SplitN(out.String(), "\n", 2)[0]
			if strings.ContainsAny(line, "\x1b\a\r") || !strings.Contains(line, strconv.Quote(label)) {
				t.Fatalf("unsafe or missing label: %q", line)
			}
		})
	}
	for path, want := range before {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("saved file changed: %s: %q, %v", path, data, err)
		}
	}
}
