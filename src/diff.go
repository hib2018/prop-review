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
	"time"
	"unicode/utf8"
)

type diffEvidence struct {
	Path     string `json:"path"`
	Location string `json:"location"`
}

type diffResult struct {
	Approval    string         `json:"approval"`
	Status      string         `json:"status"`
	Explanation string         `json:"explanation"`
	Shortage    string         `json:"shortage"`
	Evidence    []diffEvidence `json:"evidence"`
}

func showDiff(engine string, out, progress io.Writer) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	approvals, err := flowApprovals(root)
	if err != nil {
		return fmt.Errorf("validate comparison approvals: %w", err)
	}
	if len(approvals) == 0 {
		_, err := fmt.Fprintln(out, "No approved proposals; nothing to compare.")
		return err
	}
	data, err := json.Marshal(approvals)
	if err != nil {
		return err
	}
	response, err := engineResponse(root, fmt.Sprintf(diffPrompt, data), engine, 10*time.Minute, progress, "Comparing approved proposals")
	if err != nil {
		return fmt.Errorf("compare approved proposals: %w", err)
	}
	results, err := parseDiffResults(response, approvals)
	if err != nil {
		return err
	}
	for _, result := range results {
		for _, evidence := range result.Evidence {
			path, err := filepath.EvalSymlinks(filepath.Join(root, evidence.Path))
			if err != nil {
				return fmt.Errorf("invalid diff evidence %q: %w", evidence.Path, err)
			}
			relative, err := filepath.Rel(root, path)
			if err != nil || !filepath.IsLocal(relative) {
				return fmt.Errorf("diff evidence outside repository: %q", evidence.Path)
			}
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("diff evidence is not a regular repository file: %q", evidence.Path)
			}
		}
	}
	return writeDiffResults(out, results)
}

// Reject duplicate keys rather than silently accepting the last model verdict.
func decodeDiffObject(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return errors.New("expected diff result/evidence object")
	}
	seen := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		key = strings.ToLower(key) // Match encoding/json's case-insensitive field lookup.
		if !ok || seen[key] {
			return errors.New("duplicate diff response field")
		}
		seen[key] = true
		if err := decoder.Decode(new(json.RawMessage)); err != nil {
			return err
		}
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(destination)
}

func (result *diffResult) UnmarshalJSON(data []byte) error {
	type plain diffResult
	return decodeDiffObject(data, (*plain)(result))
}

func (evidence *diffEvidence) UnmarshalJSON(data []byte) error {
	type plain diffEvidence
	return decodeDiffObject(data, (*plain)(evidence))
}

func parseDiffResults(response []byte, approvals []string) ([]diffResult, error) {
	invalid := errors.New("invalid diff response; comparison is incomplete")
	if !utf8.Valid(response) {
		return nil, invalid
	}
	var results []diffResult
	decoder := json.NewDecoder(bytes.NewReader(response))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&results); err != nil || len(results) != len(approvals) {
		return nil, invalid
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, invalid
	}
	positions := make(map[string]int, len(approvals))
	for i, approval := range approvals {
		positions[approval] = i
	}
	ordered := make([]diffResult, len(approvals))
	seen := make(map[string]bool, len(approvals))
	for _, result := range results {
		index, ok := positions[result.Approval]
		if !ok || seen[result.Approval] || strings.TrimSpace(result.Explanation) == "" || result.Evidence == nil {
			return nil, invalid
		}
		switch result.Status {
		case "implemented":
			if len(result.Evidence) == 0 {
				return nil, invalid
			}
		case "partial", "missing":
			if strings.TrimSpace(result.Shortage) == "" {
				return nil, invalid
			}
		case "unknown":
		default:
			return nil, invalid
		}
		for _, evidence := range result.Evidence {
			if strings.TrimSpace(evidence.Path) == "" || !filepath.IsLocal(evidence.Path) || strings.TrimSpace(evidence.Location) == "" {
				return nil, invalid
			}
		}
		seen[result.Approval] = true
		ordered[index] = result
	}
	return ordered, nil
}

func writeDiffResults(out io.Writer, results []diffResult) error {
	counts := make(map[string]int)
	for _, result := range results {
		counts[result.Status]++
	}
	var report strings.Builder
	fmt.Fprintf(&report, "Total: %d; implemented: %d; partial: %d; missing: %d; unknown: %d\n", len(results), counts["implemented"], counts["partial"], counts["missing"], counts["unknown"])
	for _, group := range []struct {
		title    string
		statuses string
	}{{"Implementation gaps", "partial missing"}, {"Unable to determine", "unknown"}, {"Implemented", "implemented"}} {
		fmt.Fprintf(&report, "\n%s:\n", group.title)
		found := false
		for _, result := range results {
			if !strings.Contains(group.statuses, result.Status) {
				continue
			}
			found = true
			fmt.Fprintf(&report, "- [%s] %q\n  Explanation: %q\n", result.Status, result.Approval, result.Explanation)
			if result.Shortage != "" {
				fmt.Fprintf(&report, "  Shortage: %q\n", result.Shortage)
			}
			if len(result.Evidence) == 0 {
				fmt.Fprintln(&report, "  Evidence: none found.")
			}
			for _, evidence := range result.Evidence {
				fmt.Fprintf(&report, "  Evidence: %q (%q)\n", evidence.Path, evidence.Location)
			}
		}
		if !found {
			fmt.Fprintln(&report, "  None.")
		}
	}
	_, err := io.WriteString(out, report.String())
	return err
}
