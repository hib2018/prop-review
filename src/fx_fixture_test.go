package main

import (
	"encoding/json"
	"testing"
)

// fxJSON wraps a model response without requiring a JSON encoder in mock shells.
func fxJSON(t *testing.T, output string) string {
	t.Helper()
	data, err := json.Marshal(struct {
		FinalOutput string `json:"final_output"`
		ExitCode    int    `json:"exit_code"`
	}{FinalOutput: output})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
