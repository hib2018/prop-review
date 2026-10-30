package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The skill owns review selection, validation, and the reply; the CLI only starts the conversation.
func ingest(root, engine string) error {
	skill, err := findIngestSkill(root, engine)
	if err != nil {
		return err
	}
	prompt := fmt.Sprintf("Invoke prop-review-ingest now. Read and follow the skill at %s to report the latest completed review in this repository and respond to the human's feedback in chat. Write both the outcome summary and your response in the same language as the selected proposal text, not the CLI, these instructions, or the repository documentation. Preserve quoted proposal and comment text as written. If no result is found, ask the human for its path. Do not modify review files, revise proposals, or implement anything.", skill)
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
		// Save a new first turn, then open exactly that session, never the latest one.
		id, err := fxIngestSession(root, prompt)
		if err != nil {
			return err
		}
		return launch("fx", "--resume", id)
	default:
		return fmt.Errorf("unknown engine: %s", engine)
	}
}

func findIngestSkill(root, engine string) (string, error) {
	local := []string{".agents/skills", ".pi/skills"}
	if engine == "pi" {
		local[0], local[1] = local[1], local[0]
	}
	var paths []string
	for _, base := range local {
		paths = append(paths, filepath.Join(root, base, "prop-review-ingest", "SKILL.md"))
	}
	// Preserve the existing user-wide fallback order.
	home, err := os.UserHomeDir()
	if err == nil {
		paths = append(paths,
			filepath.Join(home, ".agents", "skills", "prop-review-ingest", "SKILL.md"),
			filepath.Join(home, ".pi", "agent", "skills", "prop-review-ingest", "SKILL.md"))
	}
	for _, path := range paths {
		if info, statErr := os.Stat(path); statErr == nil && info.Mode().IsRegular() {
			return path, nil
		} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
	}
	if err != nil {
		return "", err
	}
	return "", errors.New("prop-review-ingest skill not found; run prop-review init or install it in ~/.agents/skills or ~/.pi/agent/skills")
}

func fxIngestSession(root, prompt string) (string, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "fx", "ask", "--json", "--prompt-permissions", "--", prompt)
	cmd.Dir = root
	cmd.Stdin, cmd.Stderr = os.Stdin, os.Stderr
	cmd.WaitDelay = time.Second
	output := &limitedCapture{limit: maxEngineOutputBytes, cancel: cancel}
	cmd.Stdout = output
	err := cmd.Run()
	if output.exceeded.Load() {
		return "", fmt.Errorf("fx ingest response exceeds %d bytes", maxEngineOutputBytes)
	}
	if err != nil {
		return "", fmt.Errorf("fx failed: %w", err)
	}
	// Validate successful completion before opening any session.
	if _, err := fxResponse(output.Bytes()); err != nil {
		return "", err
	}
	var response struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		return "", fmt.Errorf("invalid fx ingest response: %w", err)
	}
	id := response.SessionID
	// Reject aliases and option-like values: resume must identify one exact session.
	if id == "" || id == "last" || strings.HasPrefix(id, "-") || strings.IndexFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	}) >= 0 {
		return "", errors.New("fx ingest response has no valid exact session_id; refusing to resume another session")
	}
	return id, nil
}
