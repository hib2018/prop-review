package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// The skill owns review selection, validation, and the reply; the CLI only starts the conversation.
func ingest(root, engine string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	var skill string
	for _, path := range []string{
		filepath.Join(home, ".agents", "skills", "prop-review-ingest", "SKILL.md"),
		filepath.Join(home, ".pi", "agent", "skills", "prop-review-ingest", "SKILL.md"),
	} {
		if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
			skill = path
			break
		} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
	}
	if skill == "" {
		return errors.New("prop-review-ingest skill not found; install it in ~/.agents/skills or ~/.pi/agent/skills")
	}
	prompt := fmt.Sprintf("Invoke prop-review-ingest now. Read and follow the skill at %s to report the latest completed review in this repository and respond to the human's feedback in chat. If no result is found, ask the human for its path. Do not modify review files, revise proposals, or implement anything.", skill)
	launch := func(args ...string) error {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = root
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s failed: %w", args[0], err)
		}
		return nil
	}
	switch engine {
	case "pi":
		return launch("pi", "--skill", skill, prompt)
	case "fx":
		// fx has no interactive initial-prompt flag. Save the first turn, then resume it.
		if err := launch("fx", "ask", "--prompt-permissions", "--", prompt); err != nil {
			return err
		}
		return launch("fx", "--continue")
	default:
		return fmt.Errorf("unknown engine: %s", engine)
	}
}
