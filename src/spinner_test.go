package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Observe frames without racing the spinner's writes.
type spinnerOutput struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	writes chan struct{}
}

func (out *spinnerOutput) Write(p []byte) (int, error) {
	out.mu.Lock()
	defer out.mu.Unlock()
	n, err := out.buf.Write(p)
	select {
	case out.writes <- struct{}{}:
	default:
	}
	return n, err
}

func (out *spinnerOutput) String() string {
	out.mu.Lock()
	defer out.mu.Unlock()
	return out.buf.String()
}

func TestSpinnerClearsAndStops(t *testing.T) {
	for _, animated := range []bool{false, true} {
		t.Run(fmt.Sprintf("animated=%t", animated), func(t *testing.T) {
			out := &spinnerOutput{writes: make(chan struct{}, 16)}
			stop := startSpinner(out, "Waiting", "fx")
			defer stop()
			if animated {
				deadline := time.NewTimer(3 * time.Second)
				defer deadline.Stop()
				for !strings.Contains(out.String(), "\r⠙ Waiting with fx...") {
					select {
					case <-out.writes:
					case <-deadline.C:
						t.Fatal("spinner did not advance")
					}
				}
			}
			var callers sync.WaitGroup
			for range 8 {
				callers.Go(stop)
			}
			callers.Wait()
			before := out.String()
			if !strings.HasPrefix(before, "⠋ Waiting with fx...") || !strings.HasSuffix(before, "\r\x1b[2K") || strings.Count(before, "\x1b[2K") != 1 || strings.Contains(before, "\n") {
				t.Fatalf("unexpected spinner output: %q", before)
			}
			fmt.Fprint(out, "Result or next action\n")
			time.Sleep(200 * time.Millisecond)
			stop()
			if got := out.String(); got != before+"Result or next action\n" {
				t.Fatalf("output changed after stop: %q", got)
			}
		})
	}
}

func TestEngineSpinnerClearsBeforeReturn(t *testing.T) {
	for _, engine := range []string{"fx", "pi"} {
		for _, mode := range []string{"immediate", "normal", "failure", "timeout"} {
			t.Run(engine+"/"+mode, func(t *testing.T) {
				bin := t.TempDir()
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				script := "#!/bin/sh\ncat >/dev/null\n"
				timeout := 3 * time.Second
				switch mode {
				case "normal":
					script += "sleep 0.2\n"
				case "failure":
					script += "echo engine-error >&2\nexit 1\n"
				case "timeout":
					script += "exec sleep 3\n"
					timeout = 100 * time.Millisecond
				}
				response := "result"
				if engine == "fx" {
					response = fxJSON(t, response)
				}
				t.Setenv("SPINNER_RESPONSE", response)
				script += "printf '%s' \"$SPINNER_RESPONSE\"\n"
				program := "fx"
				if engine == "pi" {
					program = "node"
					if err := os.WriteFile(filepath.Join(bin, "npm"), []byte("#!/bin/sh\necho /fixture/modules\n"), 0700); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(filepath.Join(bin, program), []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
				var out strings.Builder
				got, err := engineResponse(t.TempDir(), "prompt", engine, timeout, &out, "Working")
				switch mode {
				case "failure":
					if err == nil || !strings.Contains(err.Error(), "engine-error") {
						t.Fatalf("expected failure: %v", err)
					}
				case "timeout":
					if err == nil || !strings.Contains(err.Error(), "timed out") {
						t.Fatalf("expected timeout: %v", err)
					}
				default:
					if err != nil || string(got) != "result" {
						t.Fatalf("result: %q, %v", got, err)
					}
				}
				if !strings.HasSuffix(out.String(), "\r\x1b[2K") || strings.Count(out.String(), "\x1b[2K") != 1 {
					t.Fatalf("spinner not cleared before return: %q", out.String())
				}
			})
		}
	}
}
