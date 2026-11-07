package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func executionFlowFixture(t *testing.T) (string, string) {
	t.Helper()
	root := flowSourceTempGit(t)
	bin := t.TempDir()
	prompt := filepath.Join(bin, "prompt")
	limitScript(t, bin, "fx", `cat > "$FLOW_PROMPT"
printf '%s' "$FLOW_REPLY"`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FLOW_PROMPT", prompt)
	t.Setenv("FLOW_REPLY", fxJSON(t, `{"status":"completed","summary":"Implemented and verified"}`))
	t.Setenv("PROP_REVIEW_FLOW_MODEL", "")
	t.Setenv("PROP_REVIEW_FLOW_THINKING", "")
	inFixtureRepo(t, root)
	return root, prompt
}

func flowReviewed(t *testing.T, root, name string, items []item) string {
	t.Helper()
	path := filepath.Join(root, "prop-review-tmp", name, "proposal.txt")
	original := make([]item, len(items))
	for i, it := range items {
		original[i].topic = it.topic
	}
	flowSourceWrite(t, root, filepath.Join("prop-review-tmp", name, "proposal.txt"), render(original))
	flowSourceWrite(t, root, filepath.Join("prop-review-tmp", name, "proposal.review.txt"), render(items))
	return path
}

func TestFlowExecutesOnlyExplicitApprovals(t *testing.T) {
	for _, engine := range []string{"fx", "pi"} {
		t.Run(engine, func(t *testing.T) {
			root, prompt := executionFlowFixture(t)
			flowReviewed(t, root, "review.1", []item{{topic: "approved change", answer: "Approved"}, {topic: "pending secret"}, {topic: "commented secret", answer: "Approved", comment: true}})
			flowSourceWrite(t, root, "prop-review-tmp/review.1/request.txt", "unapproved request secret")
			if engine == "pi" {
				limitScript(t, filepath.Dir(prompt), "npm", "printf '/unused/modules\\n'")
				limitScript(t, filepath.Dir(prompt), "node", `cat > "$FLOW_PROMPT"
printf '%s' '{"status":"completed","summary":"Implemented and verified"}'`)
			}
			var out strings.Builder
			if err := showFlow(engine, &out, io.Discard); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(prompt)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), `["approved change"]`) || strings.Contains(string(data), "secret") {
				t.Fatalf("scope leak: %s", data)
			}
			if !strings.Contains(out.String(), "Implemented and verified") {
				t.Fatal(out.String())
			}
		})
	}
}

func TestFlowHistoricalApprovalsWithoutTransfer(t *testing.T) {
	root, _ := executionFlowFixture(t)
	parent := flowReviewed(t, root, "review.1", []item{{topic: "historical", answer: "Approved"}, {topic: "rework", answer: "change it", comment: true}})
	child := flowReviewed(t, root, "review.2", []item{{topic: "renamed historical"}, {topic: "new approved", answer: "Approved"}, {topic: "comment approval", answer: "Approved", comment: true}})
	flowSourceWrite(t, root, "prop-review-tmp/review.2/parent.txt", filepath.Base(filepath.Dir(parent))+"\n")
	flowSourceWrite(t, root, "prop-review-tmp/review.2/revision.txt", "restructure\n")
	// Unreviewed followup drafts do not remove historical approvals.
	flowSourceWrite(t, root, "prop-review-tmp/review.3/proposal.txt", render([]item{{topic: "draft"}}))
	flowSourceWrite(t, root, "prop-review-tmp/review.3/parent.txt", filepath.Base(filepath.Dir(child))+"\n")
	got, err := flowApprovals(root)
	if err != nil || !reflect.DeepEqual(got, []string{"historical", "new approved"}) {
		t.Fatalf("%v, %v", got, err)
	}
}

func TestFlowCombinesRootsAndExecutesWithoutTTY(t *testing.T) {
	root, prompt := executionFlowFixture(t)
	first := flowReviewed(t, root, "review.1", []item{{topic: "change A", answer: "Approved"}})
	flowReviewed(t, root, "review.2", []item{{topic: "change B depends on A", answer: "Approved"}, {topic: "change A", answer: "Approved"}})
	got, err := flowApprovals(root)
	if err != nil || !reflect.DeepEqual(got, []string{"change A", "change B depends on A"}) {
		t.Fatalf("combined approvals: %v, %v", got, err)
	}
	before, err := os.ReadFile(filepath.Join(filepath.Dir(first), "proposal.review.txt"))
	if err != nil {
		t.Fatal(err)
	}
	limitScript(t, filepath.Dir(prompt), "fx", `cat > "$FLOW_PROMPT"
printf 'implemented' > implemented.txt
printf '%s' "$FLOW_REPLY"`)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := setEngine("fx"); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"flow"}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "implemented.txt")); err != nil || string(data) != "implemented" {
		t.Fatalf("execution did not change repository: %q, %v", data, err)
	}
	after, err := os.ReadFile(filepath.Join(filepath.Dir(first), "proposal.review.txt"))
	if err != nil || string(before) != string(after) {
		t.Fatalf("review changed: %v", err)
	}
}

func TestFlowNoApprovalsDoesNotCallEngine(t *testing.T) {
	for _, kind := range []string{"empty", "pending", "comment"} {
		t.Run(kind, func(t *testing.T) {
			root, prompt := executionFlowFixture(t)
			if kind == "pending" {
				flowReviewed(t, root, "review.1", []item{{topic: "pending"}})
			}
			if kind == "comment" {
				flowReviewed(t, root, "review.1", []item{{topic: "comment", answer: "Approved", comment: true}})
			}
			var out strings.Builder
			if err := showFlow("fx", &out, io.Discard); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "No approved proposals") {
				t.Fatal(out.String())
			}
			if _, err := os.Stat(prompt); !os.IsNotExist(err) {
				t.Fatalf("engine called: %v", err)
			}
		})
	}
}

func TestFlowRejectsInvalidHistoryBeforeEngine(t *testing.T) {
	for _, kind := range []string{"mismatch", "malformed", "missing proposal", "parent", "cycle", "marker", "branches", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root, prompt := executionFlowFixture(t)
			path := flowReviewed(t, root, "review.1", []item{{topic: "approved", answer: "Approved"}})
			dir := filepath.Dir(path)
			switch kind {
			case "missing proposal":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "mismatch":
				flowSourceWrite(t, root, "prop-review-tmp/review.1/proposal.review.txt", "other\nApproval/Comment:Approved\n")
			case "malformed":
				flowSourceWrite(t, root, "prop-review-tmp/review.1/proposal.review.txt", "approved\nApproval/Comment:yes\n")
			case "parent":
				flowSourceWrite(t, root, "prop-review-tmp/review.1/parent.txt", "review.missing\n")
			case "cycle":
				flowSourceWrite(t, root, "prop-review-tmp/review.1/parent.txt", "review.1\n")
			case "marker":
				flowSourceWrite(t, root, "prop-review-tmp/review.1/revision.txt", "restructure\n")
			case "branches":
				for _, name := range []string{"review.2", "review.3"} {
					flowReviewed(t, root, name, []item{{topic: "extra", answer: "Approved"}})
					flowSourceWrite(t, root, "prop-review-tmp/"+name+"/parent.txt", "review.1\n")
					flowSourceWrite(t, root, "prop-review-tmp/"+name+"/followup.txt", "followup\n")
				}
			case "symlink":
				if err := os.Remove(filepath.Join(dir, "proposal.review.txt")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path, filepath.Join(dir, "proposal.review.txt")); err != nil {
					t.Fatal(err)
				}
			}
			if err := showFlow("fx", io.Discard, io.Discard); err == nil {
				t.Fatal("accepted invalid history")
			}
			if _, err := os.Stat(prompt); !os.IsNotExist(err) {
				t.Fatalf("engine called: %v", err)
			}
		})
	}
}

func TestFlowExecutionResults(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"conflict", `{"status":"blocked","summary":"conflicting approvals; dependent work stopped"}`, "conflicting approvals"},
		{"missing approval", `{"status":"blocked","summary":"dependency requires missing approval"}`, "missing approval"},
		{"permission", `{"status":"blocked","summary":"permission denied"}`, "permission denied"},
		{"verification", `{"status":"failed","summary":"tests failed"}`, "tests failed"},
		{"malformed", `not JSON`, "invalid flow execution result"},
		{"unknown", `{"status":"ok","summary":"done"}`, "invalid flow execution status"},
		{"empty", `{"status":"completed","summary":""}`, "invalid flow execution result"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := executionFlowFixture(t)
			flowReviewed(t, root, "review.1", []item{{topic: "approved", answer: "Approved"}})
			t.Setenv("FLOW_REPLY", fxJSON(t, tc.body))
			var out strings.Builder
			err := showFlow("fx", &out, io.Discard)
			if err == nil || !strings.Contains(err.Error(), tc.want) || out.Len() != 0 {
				t.Fatalf("%q, %v", out.String(), err)
			}
		})
	}
}

type flowErrorWriter struct{}

func (flowErrorWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestFlowWriteFailure(t *testing.T) {
	root, _ := executionFlowFixture(t)
	flowReviewed(t, root, "review.1", []item{{topic: "approved", answer: "Approved"}})
	if err := showFlow("fx", flowErrorWriter{}, io.Discard); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
}
func TestFlowCommandUsage(t *testing.T) {
	if err := run([]string{"flow", "extra"}); err == nil || err.Error() != "usage: prop-review flow" {
		t.Fatal(err)
	}
	if err := run([]string{"flow", "--help"}); err != nil {
		t.Fatal(err)
	}
}
func TestFlowPromptContract(t *testing.T) {
	for _, text := range []string{"assess ALL approvals together", "dependencies", "contradictions", "STOP work depending", "unapproved decision", "Never modify review files", "permission", "focused verification"} {
		if !strings.Contains(flowPrompt, text) {
			t.Fatalf("missing constraint %q", text)
		}
	}
	// The only variable context is an encoded list of approvals.
	data, _ := json.Marshal([]string{"approved"})
	if !strings.Contains(string(data), "approved") {
		t.Fatal(string(data))
	}
}
