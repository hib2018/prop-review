package main

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

//go:embed pi_flow.mjs
var piFlowSDK string

// Both engines execute approved work with their normal permission boundary.
func flowEngineResponse(root, prompt, engine string, progress io.Writer) ([]byte, error) {
	return flowEngineResponseWithTimeout(root, prompt, engine, progress, 30*time.Minute)
}

func flowEngineResponseWithTimeout(root, prompt, engine string, progress io.Writer, timeout time.Duration) ([]byte, error) {
	if !validEngine(engine) {
		return nil, fmt.Errorf("unknown engine: %q", engine)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if progress == nil {
		progress = io.Discard
	}
	defer startSpinner(progress, "Executing approved proposals", engine)()

	if engine == "fx" {
		args := []string{"ask", "--no-save", "--json"}
		var help []byte
		for _, option := range []struct{ env, flag string }{
			{"PROP_REVIEW_FLOW_MODEL", "--model"},
			{"PROP_REVIEW_FLOW_THINKING", "--effort"},
		} {
			value := strings.TrimSpace(os.Getenv(option.env))
			if value == "" {
				continue
			}
			if help == nil {
				cmd := exec.CommandContext(ctx, "fx", "ask", "--help")
				cmd.Dir = root
				var err error
				help, err = flowCommandOutput(ctx, cancel, cmd, "fx ask --help", timeout, maxEngineErrorBytes)
				if err != nil {
					return nil, fmt.Errorf("cannot apply %s: %w", option.env, err)
				}
			}
			if !strings.Contains(string(help), option.flag+" <") {
				return nil, fmt.Errorf("%s is unsupported: installed fx ask help does not advertise %s", option.env, option.flag)
			}
			args = append(args, option.flag, value)
		}
		cmd := exec.CommandContext(ctx, "fx", args...)
		cmd.Dir = root
		cmd.Stdin = strings.NewReader(prompt)
		output, err := flowCommandOutput(ctx, cancel, cmd, engine, timeout, maxEngineOutputBytes)
		if err != nil {
			return nil, err
		}
		return fxResponse(output)
	}

	npm := exec.CommandContext(ctx, "npm", "root", "-g")
	npm.Dir = root
	modules, err := flowCommandOutput(ctx, cancel, npm, "npm root -g", timeout, maxNpmOutputBytes)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "node", "--input-type=module", "-e", piFlowSDK, strings.TrimSpace(string(modules)))
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(prompt)
	return flowCommandOutput(ctx, cancel, cmd, engine, timeout, maxEngineOutputBytes)
}

func flowCommandOutput(ctx context.Context, cancel context.CancelFunc, cmd *exec.Cmd, name string, timeout time.Duration, limit int) ([]byte, error) {
	if err := configureEngineProcess(cmd); err != nil {
		return nil, err
	}
	// Give the SDK time to abort detached tool processes before killing its group.
	kill := cmd.Cancel
	cmd.Cancel = func() error {
		if err := cmd.Process.Signal(os.Interrupt); err == nil {
			time.Sleep(time.Second)
		}
		return kill()
	}
	cmd.WaitDelay = 3 * time.Second
	stdout := &limitedCapture{limit: limit, cancel: cancel}
	stderr := &limitedCapture{limit: maxEngineErrorBytes, cancel: cancel}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	if stdout.exceeded.Load() {
		return nil, fmt.Errorf("%s stdout exceeds %d bytes", name, limit)
	}
	if stderr.exceeded.Load() {
		return nil, fmt.Errorf("%s stderr exceeds %d bytes: %q", name, maxEngineErrorBytes, strings.TrimSpace(stderr.String()))
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%s timed out after %s: %w", name, timeout, ctx.Err())
	}
	if name == "fx" {
		if quotaErr := fxUsageLimitError(stdout.Bytes(), stderr.String()); quotaErr != nil {
			return nil, quotaErr
		}
	}
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w: %q", name, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
