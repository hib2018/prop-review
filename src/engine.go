package main

import (
	"bytes"
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
	"sync"
	"sync/atomic"
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
		return "pi", nil
	}
	if err != nil {
		return "", err
	}
	engine := strings.TrimSpace(string(data))
	if !validEngine(engine) {
		return "", fmt.Errorf("invalid engine setting: %s; select pi or fx with prop-review --engine NAME", path)
	}
	return engine, nil
}

func setEngine(engine string) error {
	if !validEngine(engine) {
		return errors.New("usage: prop-review --engine pi|fx")
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
	case "pi", "fx":
		return true
	}
	return false
}

//go:embed pi_sdk.mjs
var piSDK string

const maxEngineOutputBytes = 16 << 20
const maxEngineErrorBytes = 64 << 10
const maxNpmOutputBytes = 8 << 10

type limitedCapture struct {
	buf      bytes.Buffer
	limit    int
	exceeded atomic.Bool
	cancel   context.CancelFunc
}

func (c *limitedCapture) Write(p []byte) (int, error) {
	if c.exceeded.Load() {
		return len(p), nil
	}
	remaining := c.limit - c.buf.Len()
	if len(p) > remaining {
		c.buf.Write(p[:remaining])
		c.exceeded.Store(true)
		c.cancel()
		return len(p), nil
	}
	return c.buf.Write(p)
}

func (c *limitedCapture) String() string { return c.buf.String() }
func (c *limitedCapture) Bytes() []byte  { return c.buf.Bytes() }

// Engines return only a JSON array of proposal strings; never trust it as a proposal file.
func proposals(root, prompt, engine string) ([]item, error) {
	return proposalsWithTimeout(root, prompt, engine, 100*time.Second, os.Stderr)
}

func proposalsWithTimeout(root, prompt, engine string, timeout time.Duration, progress io.Writer) ([]item, error) {
	return proposalsWithLimit(root, prompt, engine, timeout, progress, false)
}

func proposalsWithLimit(root, prompt, engine string, timeout time.Duration, progress io.Writer, allowEmpty bool) ([]item, error) {
	return proposalsWithActivity(root, prompt, engine, timeout, progress, allowEmpty, "Generating proposals")
}

func proposalsWithActivity(root, prompt, engine string, timeout time.Duration, progress io.Writer, allowEmpty bool, activity string) ([]item, error) {
	output, err := engineResponse(root, prompt, engine, timeout, progress, activity)
	if err != nil {
		return nil, err
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
	if err := uniqueTopics(items); err != nil {
		return nil, err
	}
	return items, nil
}

func engineResponse(root, prompt, engine string, timeout time.Duration, progress io.Writer, activity string) ([]byte, error) {
	if !validEngine(engine) {
		return nil, fmt.Errorf("unknown engine: %s", engine)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	defer startSpinner(progress, activity, engine)()
	var cmd *exec.Cmd
	switch engine {
	case "fx":
		cmd = exec.CommandContext(ctx, "fx", "ask", "--no-save", "--json")
		cmd.Stdin = strings.NewReader(prompt)
	case "pi":
		npm := exec.CommandContext(ctx, "npm", "root", "-g")
		npm.WaitDelay = time.Second
		modules := &limitedCapture{limit: maxNpmOutputBytes, cancel: cancel}
		npmErrors := &limitedCapture{limit: maxEngineErrorBytes, cancel: cancel}
		npm.Stdout, npm.Stderr = modules, npmErrors
		err := npm.Run()
		if modules.exceeded.Load() {
			return nil, fmt.Errorf("npm root -g output exceeds %d bytes", maxNpmOutputBytes)
		}
		if npmErrors.exceeded.Load() {
			return nil, fmt.Errorf("npm root -g stderr exceeds %d bytes", maxEngineErrorBytes)
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("%s timed out after %s: %w", engine, timeout, ctx.Err())
			}
			return nil, fmt.Errorf("locate Pi SDK (npm root -g): %w: %s", err, strings.TrimSpace(npmErrors.String()))
		}
		cmd = exec.CommandContext(ctx, "node", "--input-type=module", "-e", piSDK, strings.TrimSpace(modules.String()))
		cmd.Stdin = strings.NewReader(prompt)
	default:
		return nil, fmt.Errorf("unknown engine: %s", engine)
	}
	cmd.Dir = root
	cmd.WaitDelay = time.Second
	stdout := &limitedCapture{limit: maxEngineOutputBytes, cancel: cancel}
	stderr := &limitedCapture{limit: maxEngineErrorBytes, cancel: cancel}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	if stdout.exceeded.Load() {
		return nil, fmt.Errorf("%s stdout exceeds %d bytes", engine, maxEngineOutputBytes)
	}
	if stderr.exceeded.Load() {
		return nil, fmt.Errorf("%s stderr exceeds %d bytes: %s", engine, maxEngineErrorBytes, strings.TrimSpace(stderr.String()))
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%s timed out after %s: %w", engine, timeout, ctx.Err())
	}
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w: %s", engine, err, strings.TrimSpace(stderr.String()))
	}
	output := stdout.Bytes()
	if engine == "fx" {
		return fxResponse(output)
	}
	return output, nil
}

func startSpinner(progress io.Writer, activity, engine string) func() {
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(150 * time.Millisecond)
		defer ticker.Stop()
		frames := []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
		frame := 0
		fmt.Fprintf(progress, "%c %s with %s...", frames[frame], activity, engine)
		for {
			select {
			case <-ticker.C:
				frame = (frame + 1) % len(frames)
				fmt.Fprintf(progress, "\r%c %s with %s...", frames[frame], activity, engine)
			case <-done:
				fmt.Fprintln(progress)
				return
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done); <-finished }) }
}

// fx's output includes intermediate commentary; only final_output is a task result.
func fxResponse(output []byte) ([]byte, error) {
	var response struct {
		FinalOutput *string `json:"final_output"`
		ExitCode    *int    `json:"exit_code"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("invalid fx response JSON: %w", err)
	}
	if response.ExitCode == nil {
		return nil, errors.New("fx response is missing exit_code")
	}
	if *response.ExitCode != 0 {
		return nil, fmt.Errorf("fx reported exit_code %d", *response.ExitCode)
	}
	if response.FinalOutput == nil || strings.TrimSpace(*response.FinalOutput) == "" {
		return nil, errors.New("fx response has no completed final_output")
	}
	return []byte(*response.FinalOutput), nil
}
