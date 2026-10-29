package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func reconstructionMarker(path string) (bool, error) {
	data, err := localFile(filepath.Join(filepath.Dir(path), "revision.txt"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if string(data) != "restructure\n" {
		return false, fmt.Errorf("invalid revision.txt beside %s", path)
	}
	return true, nil
}

// Validate the entire linked discussion before sending review data to an engine.
func reconstructionPrompt(root, selected string) (string, error) {
	first := selected
	visited := make(map[string]bool)
	for {
		if visited[first] {
			return "", errors.New("cyclic review history")
		}
		visited[first] = true
		parent, err := parentName(first)
		if err != nil {
			return "", err
		}
		if parent == "" {
			break
		}
		first = filepath.Join(root, "prop-review-tmp", parent, "proposal.txt")
	}
	history, request, err := reviewHistory(root, first, false)
	if err != nil {
		return "", err
	}
	if history[len(history)-1].path != selected {
		return "", errors.New("revision must target the latest reviewed proposal in its chain")
	}
	type topic struct {
		Topic   string `json:"topic"`
		Comment string `json:"comment,omitempty"`
		Status  string `json:"status"`
	}
	type review struct {
		Items       []topic `json:"items"`
		Followup    bool    `json:"followup"`
		Restructure bool    `json:"restructure"`
	}
	input := struct {
		Request  string   `json:"request_or_issue_title"`
		Approved []string `json:"approved"`
		Reviews  []review `json:"reviews"`
	}{Request: request}
	accepted := make(map[string]bool)
	for _, step := range history {
		entry := review{Followup: step.followup, Restructure: step.restructure}
		for _, it := range step.items {
			value := topic{Topic: it.topic, Status: "pending"}
			if it.comment {
				value.Comment, value.Status = it.answer, "commented"
			}
			if !it.comment && it.answer == "Approved" {
				value.Status = "approved"
				if !accepted[it.topic] {
					input.Approved = append(input.Approved, it.topic)
				}
				accepted[it.topic] = true
			}
			entry.Items = append(entry.Items, value)
		}
		input.Reviews = append(input.Reviews, entry)
	}
	data, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(revisePrompt, data), nil
}
