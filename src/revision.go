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
	return reconstructionPromptFromHistory(history, request)
}

func reconstructionPromptFromHistory(history []reviewStep, request string) (string, error) {
	input, _ := reviewInput(history, request)
	data, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(revisePrompt, data), nil
}

// The same chronological interpretation feeds revision, clarification and spec checks.
// Historical approvals constrain generation; outstanding describes the current review state.
type reviewTopic struct {
	Topic   string `json:"topic"`
	Comment string `json:"comment,omitempty"`
	Status  string `json:"status"`
}

type reviewContext struct {
	Proposal    string        `json:"proposal"`
	Items       []reviewTopic `json:"items"`
	Followup    bool          `json:"followup"`
	Restructure bool          `json:"restructure"`
}

type reviewGenerationInput struct {
	Issue       *githubIssue    `json:"original_issue,omitempty"`
	Request     string          `json:"request_or_issue_title"`
	Approved    []string        `json:"approved"`
	Reviews     []reviewContext `json:"reviews"`
	Outstanding []reviewTopic   `json:"outstanding"`
}

func reviewInput(history []reviewStep, request string) (reviewGenerationInput, []item) {
	input := reviewGenerationInput{Request: request}
	if len(history) > 0 {
		input.Issue = history[0].issue
	}
	accepted := make(map[string]bool)
	open := make(map[string]reviewTopic)
	var order []string
	for i, step := range history {
		if step.restructure {
			clear(open)
			order = nil // Reused topics follow the reconstructed proposal's new order.
		} else if i > 0 && !step.followup {
			for _, prior := range history[i-1].items {
				if prior.comment {
					delete(open, prior.topic)
				}
			}
		}
		entry := reviewContext{Proposal: filepath.Base(filepath.Dir(step.path)), Followup: step.followup, Restructure: step.restructure}
		for _, it := range step.items {
			value := reviewTopic{Topic: it.topic, Status: "pending"}
			if it.comment {
				value.Comment, value.Status = it.answer, "commented"
			} else if it.answer == "Approved" {
				value.Status = "approved"
				if !accepted[it.topic] {
					input.Approved = append(input.Approved, it.topic)
				}
				accepted[it.topic] = true
			}
			entry.Items = append(entry.Items, value)
			if value.Status == "approved" {
				delete(open, it.topic)
			} else {
				if _, exists := open[it.topic]; !exists {
					order = append(order, it.topic)
				}
				open[it.topic] = value
			}
		}
		input.Reviews = append(input.Reviews, entry)
	}
	var outstanding []item
	carried := make(map[string]bool)
	for _, topic := range order {
		if value, exists := open[topic]; exists && !carried[topic] {
			input.Outstanding = append(input.Outstanding, value)
			outstanding = append(outstanding, item{topic: topic})
			carried[topic] = true
		}
	}
	return input, outstanding
}
