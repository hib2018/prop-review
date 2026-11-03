//go:build !darwin && !linux

package main

import (
	"fmt"
	"os/exec"
	"runtime"
)

func configureEngineProcess(cmd *exec.Cmd) error {
	return fmt.Errorf("engine subprocess cancellation is unsupported on %s; use macOS or Linux", runtime.GOOS)
}
