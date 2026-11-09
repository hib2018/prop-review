package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func diffSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			files[name] = "symlink:" + target
			return err
		}
		data, err := os.ReadFile(path)
		files[name] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func diffUnchanged(t *testing.T, root string, before map[string]string) {
	t.Helper()
	if after := diffSnapshot(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("repository file list or contents changed:\nbefore: %v\nafter: %v", before, after)
	}
}

func diffFixture(t *testing.T, engine string) (string, string) {
	t.Helper()
	root, prompt := executionFlowFixture(t)
	bin := filepath.Dir(prompt)
	t.Setenv("DIFF_ENGINE", engine)
	body := `cat > "$FLOW_PROMPT"
if test -n "$DIFF_WORKTREE"; then
  test "$(cat source.txt)" = "$DIFF_WORKTREE" || exit 21
  test "$(cat nested/untracked.txt)" = 'untracked current contents' || exit 22
fi
if test "$DIFF_FAIL" = yes; then printf 'fixture failure' >&2; exit 7; fi
printf '%s' "$FLOW_REPLY"`
	limitScript(t, bin, "fx", `test "$DIFF_ENGINE" = fx || exit 30
test "$*" = 'ask --no-save --json --quiet' || exit 31
`+body)
	limitScript(t, bin, "npm", `test "$DIFF_ENGINE" = pi || exit 32
test "$*" = 'root -g' || exit 33
printf '/unused/modules\n'`)
	limitScript(t, bin, "node", `test "$DIFF_ENGINE" = pi || exit 34
test "$1" = '--input-type=module' && test "$2" = '-e' && test "$4" = '/unused/modules' || exit 35
`+body)
	t.Setenv("DIFF_WORKTREE", "")
	t.Setenv("DIFF_FAIL", "")
	return root, prompt
}

func diffReply(t *testing.T, engine, body string) {
	t.Helper()
	if engine == "fx" {
		body = fxJSON(t, body)
	}
	t.Setenv("FLOW_REPLY", body)
}

func TestDiffEnginesWorkingTreeAndApprovalScope(t *testing.T) {
	for _, engine := range []string{"pi", "fx"} {
		t.Run(engine, func(t *testing.T) {
			root, prompt := diffFixture(t, engine)
			flowSourceWrite(t, root, "source.txt", "committed contents")
			flowSourceGit(t, root, "add", "source.txt")
			flowSourceGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "fixture")
			flowSourceWrite(t, root, "source.txt", "uncommitted current contents")
			flowSourceWrite(t, root, "nested/untracked.txt", "untracked current contents")
			t.Setenv("DIFF_WORKTREE", "uncommitted current contents")
			flowReviewed(t, root, "review.1", []item{{topic: "historical", answer: "Approved"}, {topic: "partial approval", answer: "Approved"}, {topic: "pending secret"}, {topic: "comment secret", answer: "Approved", comment: true}})
			flowReviewed(t, root, "review.2", []item{{topic: "renamed historical secret"}, {topic: "partial approval", answer: "Approved"}, {topic: "renamed pending secret"}})
			flowSourceWrite(t, root, "prop-review-tmp/review.2/parent.txt", "review.1\n")
			flowSourceWrite(t, root, "prop-review-tmp/review.2/revision.txt", "restructure\n")
			flowReviewed(t, root, "review.3", []item{{topic: "missing approval", answer: "Approved"}, {topic: "unknown approval", answer: "Approved"}, {topic: "historical", answer: "Approved"}})
			flowSourceWrite(t, root, "prop-review-tmp/review.4/proposal.txt", render([]item{{topic: "unreviewed secret"}}))
			flowSourceWrite(t, root, "prop-review-tmp/review.4/parent.txt", "review.2\n")
			flowSourceWrite(t, root, "prop-review-tmp/review.1/request.txt", "request secret")
			flowSourceWrite(t, root, "prop-review-tmp/review.1/issue.json", `{"number":1,"title":"issue secret","body":"issue body secret","url":"https://github.com/example/repo/issues/1"}`)
			approvals, err := flowApprovals(root)
			want := []string{"historical", "partial approval", "missing approval", "unknown approval"}
			if err != nil || !reflect.DeepEqual(approvals, want) {
				t.Fatalf("approval fixture: %v, %v", approvals, err)
			}
			// Reverse engine order to exercise correspondence rather than positional matching.
			diffReply(t, engine, `[
{"approval":"unknown approval","status":"unknown","explanation":"permission prevents inspection","shortage":"","evidence":[]},
{"approval":"missing approval","status":"missing","explanation":"no implementation found","shortage":"add missing behavior","evidence":[]},
{"approval":"partial approval","status":"partial","explanation":"caller incomplete","shortage":"wire caller","evidence":[{"path":"source.txt","location":"line 1"}]},
{"approval":"historical","status":"implemented","explanation":"current contents verified","shortage":"","evidence":[{"path":"source.txt","location":"line 1"}]}]`)
			inFixtureRepo(t, filepath.Join(root, "nested"))
			before := diffSnapshot(t, root)
			var out strings.Builder
			if err := showDiff(engine, &out, io.Discard); err != nil {
				t.Fatal(err)
			}
			diffUnchanged(t, root, before)
			data, err := os.ReadFile(prompt)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(approvals)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != fmt.Sprintf(diffPrompt, encoded) || strings.Contains(string(data), "secret") {
				t.Fatalf("prompt differs from flow approval set: %s", data)
			}
			for _, text := range []string{"Total: 4; implemented: 1; partial: 1; missing: 1; unknown: 1", "Implementation gaps:", "Unable to determine:", "Implemented:", "wire caller", "add missing behavior", "permission prevents inspection", `Evidence: "source.txt" ("line 1")`, "Evidence: none found."} {
				if !strings.Contains(out.String(), text) {
					t.Fatalf("missing %q in %s", text, out.String())
				}
			}
			if strings.Index(out.String(), "partial approval") > strings.Index(out.String(), "missing approval") {
				t.Fatal("report did not preserve approval order within gaps")
			}
		})
	}
}

func TestDiffNoApprovalsNoEngine(t *testing.T) {
	for _, kind := range []string{"empty", "pending", "comment", "unreviewed"} {
		t.Run(kind, func(t *testing.T) {
			root, prompt := diffFixture(t, "fx")
			switch kind {
			case "pending":
				flowReviewed(t, root, "review.1", []item{{topic: "pending"}})
			case "comment":
				flowReviewed(t, root, "review.1", []item{{topic: "comment", answer: "Approved", comment: true}})
			case "unreviewed":
				flowSourceWrite(t, root, "prop-review-tmp/review.1/proposal.txt", render([]item{{topic: "draft"}}))
			}
			before := diffSnapshot(t, root)
			var out strings.Builder
			if err := showDiff("fx", &out, io.Discard); err != nil || out.String() != "No approved proposals; nothing to compare.\n" {
				t.Fatalf("%q, %v", out.String(), err)
			}
			if _, err := os.Stat(prompt); !os.IsNotExist(err) {
				t.Fatalf("engine called: %v", err)
			}
			diffUnchanged(t, root, before)
		})
	}
}

func TestDiffInvalidHistoryNoEngine(t *testing.T) {
	for _, kind := range []string{"mismatch", "malformed", "missing proposal", "parent", "cycle", "marker", "branches", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root, prompt := diffFixture(t, "fx")
			path := flowReviewed(t, root, "review.1", []item{{topic: "approved", answer: "Approved"}})
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
				review := filepath.Join(filepath.Dir(path), "proposal.review.txt")
				if err := os.Remove(review); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path, review); err != nil {
					t.Fatal(err)
				}
			}
			before := diffSnapshot(t, root)
			var out strings.Builder
			if err := showDiff("fx", &out, io.Discard); err == nil || out.Len() != 0 {
				t.Fatalf("accepted invalid history: %q, %v", out.String(), err)
			}
			if _, err := os.Stat(prompt); !os.IsNotExist(err) {
				t.Fatalf("engine called: %v", err)
			}
			diffUnchanged(t, root, before)
		})
	}
}

func TestDiffEngineFailuresLeaveNoReportOrChanges(t *testing.T) {
	for _, engine := range []string{"pi", "fx"} {
		for _, kind := range []string{"process", "malformed", "incomplete", "envelope"} {
			t.Run(engine+"/"+kind, func(t *testing.T) {
				root, prompt := diffFixture(t, engine)
				flowReviewed(t, root, "review.1", []item{{topic: "approved", answer: "Approved"}})
				diffReply(t, engine, "not JSON")
				switch kind {
				case "process":
					t.Setenv("DIFF_FAIL", "yes")
				case "incomplete":
					diffReply(t, engine, "[]")
				case "envelope":
					t.Setenv("FLOW_REPLY", `{}`)
				}
				before := diffSnapshot(t, root)
				var out strings.Builder
				err := showDiff(engine, &out, io.Discard)
				if err == nil || out.Len() != 0 {
					t.Fatalf("failure produced report: %q, %v", out.String(), err)
				}
				if kind == "process" && !strings.Contains(err.Error(), "fixture failure") {
					t.Fatal(err)
				}
				if _, err := os.Stat(prompt); err != nil {
					t.Fatalf("fixture engine did not run: %v", err)
				}
				diffUnchanged(t, root, before)
			})
		}
	}
}

func TestDiffRejectsInvalidEvidenceFiles(t *testing.T) {
	for _, kind := range []string{"missing", "directory", "external symlink"} {
		t.Run(kind, func(t *testing.T) {
			root, _ := diffFixture(t, "fx")
			flowReviewed(t, root, "review.1", []item{{topic: "approved", answer: "Approved"}})
			switch kind {
			case "directory":
				if err := os.Mkdir(filepath.Join(root, "evidence"), 0700); err != nil {
					t.Fatal(err)
				}
			case "external symlink":
				external := filepath.Join(t.TempDir(), "external.go")
				if err := os.WriteFile(external, []byte("outside repository"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, filepath.Join(root, "evidence")); err != nil {
					t.Fatal(err)
				}
			}
			diffReply(t, "fx", `[{"approval":"approved","status":"implemented","explanation":"claimed implementation","evidence":[{"path":"evidence","location":"line 1"}]}]`)
			before := diffSnapshot(t, root)
			var out strings.Builder
			if err := showDiff("fx", &out, io.Discard); err == nil || out.Len() != 0 {
				t.Fatalf("invalid evidence produced report: %q, %v", out.String(), err)
			}
			diffUnchanged(t, root, before)
		})
	}
}

func TestDiffStartupFailure(t *testing.T) {
	for _, engine := range []string{"pi", "fx"} {
		t.Run(engine, func(t *testing.T) {
			root, prompt := diffFixture(t, engine)
			flowReviewed(t, root, "review.1", []item{{topic: "approved", answer: "Approved"}})
			program := engine
			if engine == "pi" {
				program = "node"
			}
			if err := os.Chmod(filepath.Join(filepath.Dir(prompt), program), 0600); err != nil {
				t.Fatal(err)
			}
			// A non-executable fixture could otherwise fall through to a real engine on PATH.
			t.Setenv("PATH", filepath.Dir(prompt))
			before := diffSnapshot(t, root)
			// repoRoot still needs Git, but no other real executable is made available.
			// Use a fixture git that returns the known root without modifying anything.
			limitScript(t, filepath.Dir(prompt), "git", fmt.Sprintf("printf '%%s\\n' %q", root))
			var out strings.Builder
			if err := showDiff(engine, &out, io.Discard); err == nil || out.Len() != 0 {
				t.Fatalf("startup failure produced report: %q, %v", out.String(), err)
			}
			if _, err := os.Stat(prompt); !os.IsNotExist(err) {
				t.Fatalf("engine started: %v", err)
			}
			diffUnchanged(t, root, before)
		})
	}
}

func TestDiffValidation(t *testing.T) {
	valid := `{"approval":"approved","status":"implemented","explanation":"verified caller","shortage":"","evidence":[{"path":"src/main.go","location":"main:1"}]}`
	for _, tc := range []struct{ name, body string }{
		{"missing result", `[]`},
		{"null results", `null`},
		{"duplicate", "[" + valid + "," + valid + "]"},
		{"out of scope", "[" + strings.Replace(valid, `"approved"`, `"other"`, 1) + "]"},
		{"approval not exact", "[" + strings.Replace(valid, `"approved"`, `" approved"`, 1) + "]"},
		{"missing approval", "[" + strings.Replace(valid, `"approval":"approved",`, "", 1) + "]"},
		{"invalid status", "[" + strings.Replace(valid, "implemented", "done", 1) + "]"},
		{"missing status", "[" + strings.Replace(valid, `"status":"implemented",`, "", 1) + "]"},
		{"missing explanation", "[" + strings.Replace(valid, `"explanation":"verified caller",`, "", 1) + "]"},
		{"blank explanation", "[" + strings.Replace(valid, "verified caller", ` \n\t`, 1) + "]"},
		{"null explanation", "[" + strings.Replace(valid, `"verified caller"`, "null", 1) + "]"},
		{"missing evidence", "[" + strings.Replace(valid, `,"evidence":[{"path":"src/main.go","location":"main:1"}]`, "", 1) + "]"},
		{"null evidence", "[" + strings.Replace(valid, `[{"path":"src/main.go","location":"main:1"}]`, "null", 1) + "]"},
		{"empty implemented evidence", "[" + strings.Replace(valid, `[{"path":"src/main.go","location":"main:1"}]`, "[]", 1) + "]"},
		{"absolute evidence", "[" + strings.Replace(valid, "src/main.go", "/tmp/main.go", 1) + "]"},
		{"escaping evidence", "[" + strings.Replace(valid, "src/main.go", "../main.go", 1) + "]"},
		{"empty path", "[" + strings.Replace(valid, "src/main.go", "", 1) + "]"},
		{"blank path", "[" + strings.Replace(valid, "src/main.go", " ", 1) + "]"},
		{"missing location", "[" + strings.Replace(valid, `,"location":"main:1"`, "", 1) + "]"},
		{"blank location", "[" + strings.Replace(valid, "main:1", ` \t`, 1) + "]"},
		{"duplicate status field", "[" + strings.Replace(valid, `"status":"implemented"`, `"status":"unknown","status":"implemented"`, 1) + "]"},
		{"duplicate evidence field", "[" + strings.Replace(valid, `"path":"src/main.go"`, `"path":"other.go","path":"src/main.go"`, 1) + "]"},
		{"case variant duplicate field", "[" + strings.Replace(valid, `"status":"implemented"`, `"STATUS":"unknown","status":"implemented"`, 1) + "]"},
		{"unknown result field", "[" + strings.Replace(valid, `"shortage":""`, `"extra":true,"shortage":""`, 1) + "]"},
		{"unknown evidence field", "[" + strings.Replace(valid, `"location":"main:1"`, `"location":"main:1","extra":true`, 1) + "]"},
		{"trailing JSON", "[" + valid + "] []"},
		{"trailing garbage", "[" + valid + "] garbage"},
		{"wrong shape", valid},
		{"fenced JSON", "```json\n[" + valid + "]\n```"},
		{"invalid UTF8", "[" + strings.Replace(valid, "verified caller", string([]byte{0xff}), 1) + "]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			approvals := []string{"approved"}
			if tc.name == "duplicate" {
				approvals = append(approvals, "second")
			}
			if got, err := parseDiffResults([]byte(tc.body), approvals); err == nil || got != nil {
				t.Fatalf("accepted invalid response: %+v, %v", got, err)
			}
		})
	}
	for _, status := range []string{"implemented", "partial", "missing", "unknown"} {
		t.Run(status+" requirements", func(t *testing.T) {
			body := strings.Replace(valid, "implemented", status, 1)
			if status == "partial" || status == "missing" {
				for _, shortage := range []string{`""`, `" \t"`, "null", "missing"} {
					bad := strings.Replace(body, `"shortage":""`, `"shortage":`+shortage, 1)
					if shortage == "missing" {
						bad = strings.Replace(body, `"shortage":"",`, "", 1)
					}
					if _, err := parseDiffResults([]byte("["+bad+"]"), []string{"approved"}); err == nil {
						t.Fatalf("accepted %s shortage %s", status, shortage)
					}
				}
				body = strings.Replace(body, `"shortage":""`, `"shortage":"wire missing behavior"`, 1)
			}
			if status != "implemented" {
				body = strings.Replace(body, `[{"path":"src/main.go","location":"main:1"}]`, "[]", 1)
			}
			if _, err := parseDiffResults([]byte("["+body+"] \n\t"), []string{"approved"}); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{`"explanation":"verified caller",`, `,"evidence":[]`} {
				bad := strings.Replace(body, field, "", 1)
				if field == `,"evidence":[]` && status == "implemented" {
					continue
				}
				if _, err := parseDiffResults([]byte("["+bad+"]"), []string{"approved"}); err == nil {
					t.Fatalf("accepted %s without %s", status, field)
				}
			}
		})
	}
}

func TestDiffResultsOrderedByApprovals(t *testing.T) {
	body := `[{"approval":"second","status":"unknown","explanation":"blocked","evidence":[]},{"approval":"first","status":"missing","explanation":"not found","shortage":"add it","evidence":[]}]`
	got, err := parseDiffResults([]byte(body), []string{"first", "second"})
	if err != nil || len(got) != 2 || got[0].Approval != "first" || got[1].Approval != "second" {
		t.Fatalf("%+v, %v", got, err)
	}
}

func TestDiffWriterErrorsAndEscapes(t *testing.T) {
	unsafe := "unsafe\x1b[31m\n\r\t\x00"
	results := []diffResult{{Approval: unsafe, Status: "partial", Explanation: unsafe, Shortage: unsafe, Evidence: []diffEvidence{{Path: unsafe, Location: unsafe}}}}
	var out strings.Builder
	if err := writeDiffResults(&out, results); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out.String(), "\x1b\r\t\x00") || strings.Count(out.String(), fmt.Sprintf("%q", unsafe)) != 5 {
		t.Fatalf("unsafe report: %q", out.String())
	}
	if err := writeDiffResults(flowErrorWriter{}, results); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	out.Reset()
	if err := writeDiffResults(&out, nil); err != nil || strings.Count(out.String(), "  None.") != 3 {
		t.Fatalf("empty groups: %q, %v", out.String(), err)
	}
	root, _ := diffFixture(t, "fx")
	if err := showDiff("fx", flowErrorWriter{}, io.Discard); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	flowReviewed(t, root, "review.1", []item{{topic: "approved", answer: "Approved"}})
	diffReply(t, "fx", `[{"approval":"approved","status":"unknown","explanation":"blocked","evidence":[]}]`)
	before := diffSnapshot(t, root)
	if err := showDiff("fx", flowErrorWriter{}, io.Discard); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	diffUnchanged(t, root, before)
}

func diffRunOutput(t *testing.T, args []string) (string, error) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	previous := os.Stdout
	os.Stdout = file
	defer func() { os.Stdout = previous }()
	runErr := run(args)
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(data), runErr
}

func TestDiffCommandRoutingAndUsage(t *testing.T) {
	for _, engine := range []string{"pi", "fx"} {
		t.Run(engine, func(t *testing.T) {
			root, _ := diffFixture(t, engine)
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			if err := setEngine(engine); err != nil {
				t.Fatal(err)
			}
			flowReviewed(t, root, "review.1", []item{{topic: "approved", answer: "Approved"}})
			diffReply(t, engine, `[{"approval":"approved","status":"unknown","explanation":"blocked","evidence":[]}]`)
			before := diffSnapshot(t, root)
			out, err := diffRunOutput(t, []string{"diff"})
			if err != nil || !strings.Contains(out, "Total: 1; implemented: 0; partial: 0; missing: 0; unknown: 1") {
				t.Fatalf("%q, %v", out, err)
			}
			diffUnchanged(t, root, before)
		})
	}
	for _, args := range [][]string{{"diff", "extra"}, {"diff", "--help", "extra"}} {
		if err := run(args); err == nil || err.Error() != "usage: prop-review diff" {
			t.Fatalf("%v: %v", args, err)
		}
	}
	out, err := diffRunOutput(t, []string{"diff", "--help"})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"usage: prop-review diff", "prev diff", "uncommitted", "not a Git line diff", "10-minute", "Pi/fx", "zero approvals", "read-only", "not an enforced read-only sandbox"} {
		if !strings.Contains(out, text) {
			t.Fatalf("help missing %q: %s", text, out)
		}
	}
}

func TestDiffPromptContract(t *testing.T) {
	for _, text := range []string{"ALL", "uncommitted changes", "untrusted requirement data", "actual callers and wiring", "Do not change ANY files", "side effects", "Never treat pending items", "original requests, issue content", "Do not fetch external or private resources", "contradictions", "unapproved dependency work", "unknown or blockers", "exactly one object per input approval", "Use []", "implemented requires positive", "permission denial", "approvals' language", "read-tool-only", "not an enforced read-only sandbox", "Do not add persistent state"} {
		if !strings.Contains(diffPrompt, text) {
			t.Fatalf("missing diff prompt constraint %q", text)
		}
	}
}
