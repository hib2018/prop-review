package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSupportedEngines(t *testing.T) {
	config := t.TempDir()
	t.Setenv("HOME", config)
	t.Setenv("XDG_CONFIG_HOME", config)
	if got, err := selectedEngine(); err != nil || got != "pi" {
		t.Fatalf("default: %q, %v", got, err)
	}
	for _, engine := range []string{"pi", "fx"} {
		if err := setEngine(engine); err != nil {
			t.Fatal(err)
		}
		if got, err := selectedEngine(); err != nil || got != engine {
			t.Fatalf("engine: %q, %v", got, err)
		}
	}
	for _, engine := range []string{"claude", "codex", "agy", "opencode", "gemini", "", "PI"} {
		if validEngine(engine) {
			t.Fatalf("accepted engine %q", engine)
		}
		if err := setEngine(engine); err == nil {
			t.Fatalf("saved unsupported engine %q", engine)
		}
		if _, err := engineResponse(t.TempDir(), "prompt", engine, time.Second, io.Discard, "test"); err == nil {
			t.Fatalf("ran unsupported engine %q", engine)
		}
	}
	path, err := enginePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("codex\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := selectedEngine(); err == nil || !strings.Contains(err.Error(), "select pi or fx") {
		t.Fatalf("invalid saved engine: %v", err)
	}
	if err := setEngine("pi"); err != nil {
		t.Fatal(err)
	}
	if got, err := selectedEngine(); err != nil || got != "pi" {
		t.Fatalf("recovery: %q, %v", got, err)
	}
}

func TestFxResponse(t *testing.T) {
	for _, tc := range []struct{ name, response, want string }{
		{"final only", `{"output":"Checking files...\n[\"old\"]","final_output":"[\"完成案\"]","exit_code":0,"usage":{}}`, `["完成案"]`},
		{"markdown memo", `{"output":"Reading...","final_output":"## メモ\n\n承認事項のみ。\n","exit_code":0}`, "## メモ\n\n承認事項のみ。\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := fxResponse([]byte(tc.response))
			if err != nil || string(got) != tc.want {
				t.Fatalf("response: %q, %v", got, err)
			}
		})
	}
	for _, response := range []string{
		`not JSON`, `["legacy raw proposals"]`, `null`, `{"final_output":"[]"}`,
		`{"final_output":"[]","exit_code":null}`, `{"final_output":"[]","exit_code":1}`,
		`{"final_output":[],"exit_code":0}`, `{"final_output":"[]","exit_code":"0"}`,
		`{"output":"[\"draft\"]","exit_code":0}`, `{"final_output":null,"exit_code":0}`,
		`{"final_output":" \n\t","exit_code":0}`, `{"final_output":"[]","exit_code":0} trailing`,
	} {
		if _, err := fxResponse([]byte(response)); err == nil {
			t.Fatalf("accepted %s", response)
		}
	}
}

func TestFxFinalOutputStillValidated(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FX_RESPONSE", fxJSON(t, `["ok"]`))
	if err := os.WriteFile(filepath.Join(bin, "fx"), []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s' \"$FX_RESPONSE\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, final := range []string{`not-json`, `[]`, `["same","same"]`, `[" padded "]`, `["line\nbreak"]`} {
		t.Setenv("FX_RESPONSE", fxJSON(t, final))
		if _, err := proposals(t.TempDir(), "prompt", "fx"); err == nil {
			t.Fatalf("accepted final_output %q", final)
		}
	}
}
