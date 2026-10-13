package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAdditionalEngines(t *testing.T) {
	config := t.TempDir()
	t.Setenv("HOME", config)
	t.Setenv("XDG_CONFIG_HOME", config)
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	args := filepath.Join(bin, "args")
	input := filepath.Join(bin, "input")
	t.Setenv("ARGS", args)
	t.Setenv("INPUT", input)
	for name, script := range map[string]string{
		"claude":   "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ARGS\"\ncat > \"$INPUT\"\nprintf '%s\\n' '[\"Claude案\"]'\n",
		"codex":    "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ARGS\"\ncat > \"$INPUT\"\nprev=''\nfor arg do if [ \"$prev\" = '--output-last-message' ]; then printf '%s\\n' '[\"Codex案\"]' > \"$arg\"; fi; prev=$arg; done\n",
		"agy":      "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ARGS\"\ncat > \"$INPUT\"\nprintf '%s\\n' '[\"Antigravity案\"]'\n",
		"opencode": "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ARGS\"\nprintf '%s' \"$OPENCODE_CONFIG_CONTENT\" > \"$INPUT\"\nprintf '%s\\n' '{\"type\":\"step_start\"}' '{\"type\":\"text\",\"part\":{\"type\":\"text\",\"messageID\":\"last\",\"text\":\"[\\\"OpenCode案\\\"]\"}}'\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, topic string
		flags       []string
	}{
		{"claude", "Claude案", []string{"--print", "--restricted", "--tools", "Read,Glob,Grep", "--no-session-persistence"}},
		{"codex", "Codex案", []string{"exec", "--sandbox", "read-only", "--ephemeral", "--output-last-message"}},
		{"agy", "Antigravity案", []string{"--print", "--mode", "plan", "--sandbox", "--disable-slash-commands", "--output-format", "text"}},
		{"opencode", "OpenCode案", []string{"run", "--standalone", "--agent", "prop-review", "--format", "json", "prompt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := setEngine(tc.name); err != nil {
				t.Fatal(err)
			}
			if got, err := selectedEngine(); err != nil || got != tc.name {
				t.Fatalf("engine: %q, %v", got, err)
			}
			items, err := proposals(t.TempDir(), "prompt", tc.name)
			if err != nil || len(items) != 1 || items[0].topic != tc.topic {
				t.Fatalf("proposals: %v, %v", items, err)
			}
			data, err := os.ReadFile(args)
			if err != nil {
				t.Fatal(err)
			}
			for _, flag := range tc.flags {
				if !strings.Contains(string(data), flag+"\n") {
					t.Errorf("missing %q in %s", flag, data)
				}
			}
			inputData, err := os.ReadFile(input)
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "opencode" {
				for _, field := range []string{`"*":"deny"`, `"read":{"*":"allow","*.env":"deny","*.env.*":"deny"}`, `"glob":"allow"`, `"grep":"allow"`} {
					if !strings.Contains(string(inputData), field) {
						t.Errorf("missing policy %s: %s", field, inputData)
					}
				}
			} else if string(inputData) != "prompt" {
				t.Errorf("stdin: %q", inputData)
			}
		})
	}
	if err := setEngine("gemini"); err == nil {
		t.Fatal("obsolete gemini engine is still selectable")
	}
	if _, err := proposalsWithTimeout(t.TempDir(), strings.Repeat("x", 100001), "opencode", time.Second, os.Stderr); err == nil {
		t.Fatal("accepted oversized opencode argument")
	}
}

func TestOpenCodeEvents(t *testing.T) {
	for _, tc := range []struct{ events, want string }{
		{`{"type":"text","part":{"type":"text","messageID":"a","text":"draft"}}
{"type":"text","part":{"type":"text","messageID":"b","text":"[\"final\"]"}}`, `["final"]`},
		{`{"type":"text","part":{"type":"text","text":"["}}
{"type":"text","part":{"type":"text","text":"\"ok\"]"}}`, `["ok"]`},
	} {
		got, err := opencodeResponse([]byte(tc.events))
		if err != nil || string(got) != tc.want {
			t.Fatalf("events: %q, %v", got, err)
		}
	}
	for _, bad := range []string{"broken", `{"type":"step_start"}`, `{"type":"text","part":{"type":"text","text":"[\"old\"]"}}
{"type":"error"}`} {
		if _, err := opencodeResponse([]byte(bad)); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
