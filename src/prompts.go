package main

import _ "embed"

// Prompt templates retain fmt.Sprintf placeholders; keep their argument contracts
// in sync with the call sites and prompts_test.go.

//go:embed prompts/revise.txt
var revisePrompt string

//go:embed prompts/generate.txt
var generatePrompt string

//go:embed prompts/issue.txt
var issuePrompt string

//go:embed prompts/clarify.txt
var clarifyPrompt string

//go:embed prompts/spec.txt
var specPrompt string
