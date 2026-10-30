package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// issueJSON is the shared validation and size boundary for generation and storage.
func issueJSON(issue githubIssue) ([]byte, error) {
	validText := func(s string) bool {
		return utf8.ValidString(s) && !strings.ContainsRune(s, utf8.RuneError)
	}
	if issue.Number <= 0 {
		return nil, errors.New("invalid issue number")
	}
	if strings.TrimSpace(issue.Title) == "" || !validText(issue.Title) || strings.IndexFunc(issue.Title, unicode.IsControl) >= 0 {
		return nil, errors.New("invalid issue title")
	}
	if strings.TrimSpace(issue.URL) == "" || !validText(issue.URL) || strings.IndexFunc(issue.URL, unicode.IsControl) >= 0 {
		return nil, errors.New("invalid issue URL")
	}
	if !validText(issue.Body) {
		return nil, errors.New("invalid issue body")
	}
	data, err := json.Marshal(issue)
	if err != nil {
		return nil, err
	}
	if len(data) > maxRequestBytes {
		return nil, fmt.Errorf("issue #%d exceeds maximum input size (%d bytes)", issue.Number, maxRequestBytes)
	}
	return data, nil
}

func readIssueSnapshot(rootProposalPath string) (*githubIssue, error) {
	path := filepath.Join(filepath.Dir(rootProposalPath), "issue.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("unsafe issue snapshot: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxRequestBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxRequestBytes {
		return nil, errors.New("issue snapshot exceeds maximum input size")
	}
	if !utf8.Valid(data) {
		return nil, errBadEncoding
	}
	// Decode fields individually to reject nulls, duplicates and absent fields,
	// which ordinary struct unmarshalling would silently accept.
	dec := json.NewDecoder(bytes.NewReader(data))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("issue snapshot must be a JSON object")
	}
	var issue githubIssue
	fields := map[string]interface{}{
		"number": &issue.Number, "title": &issue.Title,
		"body": &issue.Body, "url": &issue.URL,
	}
	seen := make(map[string]bool)
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return nil, err
		}
		name, ok := token.(string)
		dest, known := fields[name]
		if !ok || !known || seen[name] {
			return nil, fmt.Errorf("invalid or duplicate issue field: %v", token)
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, fmt.Errorf("null issue field: %s", name)
		}
		if err := json.Unmarshal(value, dest); err != nil {
			return nil, err
		}
		seen[name] = true
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if len(seen) != len(fields) {
		return nil, errors.New("missing required issue field")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("unexpected data after issue snapshot")
	}
	if _, err := issueJSON(issue); err != nil {
		return nil, err
	}
	return &issue, nil
}

func saveIssueProposal(root string, items []item, issue githubIssue) (string, error) {
	data, err := issueJSON(issue)
	if err != nil {
		return "", err
	}
	return saveProposalWithSnapshot(root, items, "", issue.Title, false, false, data)
}
