//go:build darwin || linux

package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestEngineStopsDescendants(t *testing.T) {
	for _, command := range []string{"fx", "node", "npm"} {
		for _, mode := range []string{"timeout", "stdout", "stderr"} {
			t.Run(command+"/"+mode, func(t *testing.T) {
				bin, root := t.TempDir(), t.TempDir()
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				t.Setenv("CHILD_PID", filepath.Join(root, "child.pid"))
				t.Setenv("GRANDCHILD_PID", filepath.Join(root, "grandchild.pid"))
				limitScript(t, bin, "npm", "printf '/fake/modules\\n'")
				script := `trap '' TERM
sh -c 'trap "" TERM; sleep 60 & echo $! > "$GRANDCHILD_PID"; wait' &
echo $! > "$CHILD_PID"
while [ ! -s "$GRANDCHILD_PID" ]; do sleep 0.01; done
`
				want, timeout := "timed out", time.Second
				if mode != "timeout" {
					limit := maxEngineOutputBytes
					redirect := ""
					want, timeout = "stdout exceeds", 10*time.Second
					if command == "npm" {
						limit, want = maxNpmOutputBytes, "output exceeds"
					}
					if mode == "stderr" {
						limit, redirect, want = maxEngineErrorBytes, " >&2", "stderr exceeds"
					}
					script += "head -c " + strconv.Itoa(limit+1) + " /dev/zero" + redirect + "\n"
				}
				limitScript(t, bin, command, script+"wait")
				// Also clean up when this regression test is run against the old implementation.
				t.Cleanup(func() {
					for _, name := range []string{"child.pid", "grandchild.pid"} {
						data, _ := os.ReadFile(filepath.Join(root, name))
						if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
							_ = syscall.Kill(pid, syscall.SIGKILL)
						}
					}
				})
				engine := "pi"
				if command == "fx" {
					engine = "fx"
				}
				start := time.Now()
				_, err := engineResponse(root, "prompt", engine, timeout, io.Discard, "test")
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("expected %s, got %v", want, err)
				}
				if time.Since(start) > 5*time.Second {
					t.Fatal("engine cancellation was not prompt")
				}
				for _, name := range []string{"child.pid", "grandchild.pid"} {
					data, err := os.ReadFile(filepath.Join(root, name))
					if err != nil {
						t.Fatal(err)
					}
					pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
					if err != nil || pid <= 0 {
						t.Fatalf("invalid %s: %q", name, data)
					}
					deadline := time.Now().Add(3 * time.Second)
					for {
						if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
							break
						}
						state, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
						// An orphan zombie has stopped; reaping it belongs to the OS/container init.
						if err == nil && strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
							break
						}
						if time.Now().After(deadline) {
							t.Fatalf("%s process %d still running (state %q, ps error %v)", name, pid, state, err)
						}
						time.Sleep(10 * time.Millisecond)
					}
				}
			})
		}
	}
}
