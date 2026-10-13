package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func enginePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "prop-review", "engine"), nil
}

func selectedEngine() (string, error) {
	path, err := enginePath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "fx", nil
	}
	if err != nil {
		return "", err
	}
	engine := strings.TrimSpace(string(data))
	if !validEngine(engine) {
		return "", fmt.Errorf("invalid engine setting: %s", path)
	}
	return engine, nil
}

func setEngine(engine string) error {
	if !validEngine(engine) {
		return errors.New("usage: prop-review --engine fx|pi|claude|codex|agy|opencode")
	}
	path, err := enginePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".engine-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := io.WriteString(f, engine+"\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func validEngine(engine string) bool {
	switch engine {
	case "fx", "pi", "claude", "codex", "agy", "opencode":
		return true
	}
	return false
}

//go:embed pi_sdk.mjs
var piSDK string

// Engines return only a JSON array of proposal strings; never trust it as a proposal file.
func proposals(root, prompt, engine string) ([]item, error) {
	return proposalsWithTimeout(root, prompt, engine, 100*time.Second, os.Stderr)
}

func proposalsWithTimeout(root, prompt, engine string, timeout time.Duration, progress io.Writer) ([]item, error) {
	return proposalsWithLimit(root, prompt, engine, timeout, progress, false)
}

func proposalsWithLimit(root, prompt, engine string, timeout time.Duration, progress io.Writer, allowEmpty bool) ([]item, error) {
	if !validEngine(engine) {
		return nil, fmt.Errorf("unknown engine: %s", engine)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	start := time.Now()
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		shown := false
		for {
			select {
			case <-ticker.C:
				fmt.Fprintf(progress, "\r%ds", int(time.Since(start).Seconds()))
				shown = true
			case <-done:
				if shown {
					fmt.Fprintln(progress)
				}
				return
			}
		}
	}()
	defer func() { close(done); <-finished }()
	var cmd *exec.Cmd
	var resultFile string
	switch engine {
	case "fx":
		cmd = exec.CommandContext(ctx, "fx", "ask", "--no-save")
		cmd.Stdin = strings.NewReader(prompt)
	case "pi":
		npm := exec.CommandContext(ctx, "npm", "root", "-g")
		npm.WaitDelay = time.Second
		modules, err := npm.Output()
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("%s timed out after %s: %w", engine, timeout, ctx.Err())
			}
			return nil, fmt.Errorf("locate Pi SDK (npm root -g): %w", err)
		}
		cmd = exec.CommandContext(ctx, "node", "--input-type=module", "-e", piSDK, strings.TrimSpace(string(modules)))
		cmd.Stdin = strings.NewReader(prompt)
	case "claude":
		cmd = exec.CommandContext(ctx, "claude", "--print", "--restricted", "--strict-mcp-config", "--tools", "Read,Glob,Grep", "--permission-prompts", "none", "--no-session-persistence", "--output-format", "text")
		cmd.Stdin = strings.NewReader(prompt)
	case "codex":
		f, err := os.CreateTemp("", "prop-review-codex-*")
		if err != nil {
			return nil, err
		}
		resultFile = f.Name()
		if err := f.Close(); err != nil {
			os.Remove(resultFile)
			return nil, err
		}
		defer os.Remove(resultFile)
		cmd = exec.CommandContext(ctx, "codex", "exec", "--sandbox", "read-only", "--ephemeral", "-c", `approval_policy="never"`, "--output-last-message", resultFile, "-")
		cmd.Stdin = strings.NewReader(prompt)
	case "agy":
		cmd = exec.CommandContext(ctx, "agy", "--print", "--mode", "plan", "--sandbox", "--disable-slash-commands", "--output-format", "text")
		cmd.Stdin = strings.NewReader(prompt)
	case "opencode":
		// OpenCode accepts its prompt as an argument, unlike the other engines.
		if len(prompt) > 100000 {
			return nil, errors.New("opencode prompt exceeds command-line argument limit")
		}
		cmd = exec.CommandContext(ctx, "opencode", "run", "--standalone", "--agent", "prop-review", "--format", "json", prompt)
		cmd.Env = append(os.Environ(), `OPENCODE_CONFIG_CONTENT={"agent":{"prop-review":{"mode":"primary","permission":{"*":"deny","read":{"*":"allow","*.env":"deny","*.env.*":"deny"},"glob":"allow","grep":"allow"}}}}`)
	default:
		return nil, fmt.Errorf("unknown engine: %s", engine)
	}
	cmd.Dir = root
	cmd.WaitDelay = time.Second
	var stderr strings.Builder
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%s timed out after %s: %w", engine, timeout, ctx.Err())
	}
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w: %s", engine, err, strings.TrimSpace(stderr.String()))
	}
	switch engine {
	case "codex":
		output, err = os.ReadFile(resultFile)
		if err != nil {
			return nil, fmt.Errorf("read codex response: %w", err)
		}
	case "opencode":
		output, err = opencodeResponse(output)
		if err != nil {
			return nil, err
		}
	}
	var topics []string
	if err := json.Unmarshal(output, &topics); err != nil {
		return nil, fmt.Errorf("invalid %s JSON: %w", engine, err)
	}
	if len(topics) > 10 || len(topics) == 0 && (!allowEmpty || strings.TrimSpace(string(output)) != "[]") {
		return nil, fmt.Errorf("%s must return 1–10 proposals", engine)
	}
	items := make([]item, 0, len(topics))
	for _, topic := range topics {
		if topic != strings.TrimSpace(topic) || topic == "" || len(topic) > maxCommentBytes || strings.IndexFunc(topic, unicode.IsControl) >= 0 || !utf8.ValidString(topic) || strings.ContainsRune(topic, utf8.RuneError) {
			return nil, fmt.Errorf("invalid %s proposal topic", engine)
		}
		items = append(items, item{topic: topic})
	}
	return items, nil
}

func opencodeResponse(output []byte) ([]byte, error) {
	var text strings.Builder
	var messageID string
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	for decoder.More() {
		var event struct {
			Type string `json:"type"`
			Part struct {
				Type      string `json:"type"`
				Text      string `json:"text"`
				MessageID string `json:"messageID"`
			} `json:"part"`
		}
		if err := decoder.Decode(&event); err != nil {
			return nil, fmt.Errorf("invalid opencode events: %w", err)
		}
		if event.Type == "error" {
			return nil, errors.New("opencode reported an error")
		}
		if event.Type != "text" || event.Part.Type != "text" {
			continue
		}
		if event.Part.MessageID != "" && event.Part.MessageID != messageID {
			text.Reset()
			messageID = event.Part.MessageID
		}
		text.WriteString(event.Part.Text)
	}
	if text.Len() == 0 {
		return nil, errors.New("opencode returned no text response")
	}
	return []byte(text.String()), nil
}
