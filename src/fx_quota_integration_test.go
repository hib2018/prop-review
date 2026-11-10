package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const quotaIntegrationResponse = `{"exit_code":1,"final_output":"[\"must not save\"]","session_id":"quota-session","recovery":{"state":"need_user","need":"quota","message":"Plan allowance exhausted"}}`
const quotaIntegrationError = `fx plan usage limit reached: "Plan allowance exhausted"`

func quotaIntegrationFX(t *testing.T, exit int) (string, string) {
	t.Helper()
	bin := t.TempDir()
	args, stdin := filepath.Join(bin, "args"), filepath.Join(bin, "stdin")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("QUOTA_ARGS", args)
	t.Setenv("QUOTA_STDIN", stdin)
	t.Setenv("QUOTA_RESPONSE", quotaIntegrationResponse)
	t.Setenv("QUOTA_STDERR", "unrelated diagnostic")
	t.Setenv("QUOTA_EXIT", fmt.Sprint(exit))
	limitScript(t, bin, "fx", `printf '%s\n' "$@" >> "$QUOTA_ARGS"
cat > "$QUOTA_STDIN"
printf '%s' "$QUOTA_RESPONSE"
printf '%s' "$QUOTA_STDERR" >&2
exit "$QUOTA_EXIT"`)
	return args, stdin
}

// Include directories and metadata files so an empty or partial revision also fails.
func quotaIntegrationStorage(t *testing.T, root string) map[string]string {
	t.Helper()
	base := filepath.Join(root, "prop-review-tmp")
	result := map[string]string{}
	err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			result[rel+"/"] = ""
			return nil
		}
		data, err := os.ReadFile(path)
		result[rel] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestFXQuotaIntegrationGeneration(t *testing.T) {
	for _, exit := range []int{0, 1} {
		for _, operation := range []string{"generate", "revise", "clarify-restructure", "clarify-followup", "issue"} {
			t.Run(fmt.Sprintf("%s/process-exit-%d", operation, exit), func(t *testing.T) {
				root, original, _ := clarificationFixture(t)
				inFixtureRepo(t, root)
				t.Setenv("HOME", t.TempDir())
				t.Setenv("XDG_CONFIG_HOME", t.TempDir())
				if err := setEngine("fx"); err != nil {
					t.Fatal(err)
				}
				args, stdin := quotaIntegrationFX(t, exit)
				if operation == "clarify-followup" {
					writeReview(t, original, []item{{topic: "承認事項", answer: "Approved"}, {topic: "未確認"}, {topic: "要改訂"}})
				}
				before := quotaIntegrationStorage(t, root)
				var err error
				switch operation {
				case "generate":
					// The CLI's request entry needs /dev/tty; exercise its generation boundary.
					_, err = proposals(root, fmt.Sprintf(generatePrompt, "quota request"), "fx")
				case "revise":
					err = run([]string{"revise", original})
				case "clarify-restructure", "clarify-followup":
					history, request := clarifyRestructureHistory(t, root, original)
					var path string
					path, err = saveClarificationFromHistory(root, history, request, "fx", io.Discard)
					if path != "" {
						t.Fatalf("quota returned saved clarification: %s", path)
					}
				case "issue":
					_, err = issueProposals(root, storageIssue(), "fx")
				}
				if err == nil || err.Error() != quotaIntegrationError {
					t.Errorf("quota error = %v; want %s", err, quotaIntegrationError)
				}
				if after := quotaIntegrationStorage(t, root); !reflect.DeepEqual(before, after) {
					t.Errorf("quota changed storage: before=%v after=%v", before, after)
				}
				data, err := os.ReadFile(args)
				if err != nil || string(data) != "ask\n--no-save\n--json\n" {
					t.Fatalf("generation arguments: %q, %v", data, err)
				}
				data, err = os.ReadFile(stdin)
				if err != nil || len(data) == 0 {
					t.Fatalf("prompt not delivered on stdin: %q, %v", data, err)
				}
			})
		}
	}
}

func TestFXQuotaIntegrationCLIWorker(t *testing.T) {
	if os.Getenv("PROP_REVIEW_QUOTA_CLI_WORKER") == "1" {
		os.Args = append([]string{"prop-review"}, strings.Split(os.Getenv("QUOTA_CLI_ARGS"), "\n")...)
		main()
	}
}

func TestFXQuotaIntegrationCLI(t *testing.T) {
	for _, exit := range []int{0, 1} {
		for _, operation := range []string{"revise", "ingest"} {
			t.Run(fmt.Sprintf("%s/process-exit-%d", operation, exit), func(t *testing.T) {
				var root, original string
				if operation == "ingest" {
					root, _ = ingestFixture(t)
					var err error
					original, err = saveProposalWithMetadata(root, []item{{topic: "Keep review"}}, "", "request", false)
					if err != nil {
						t.Fatal(err)
					}
					writeReview(t, original, []item{{topic: "Keep review", answer: "Approved"}})
				} else {
					root, original, _ = clarificationFixture(t)
					inFixtureRepo(t, root)
					t.Setenv("HOME", t.TempDir())
				}
				t.Setenv("XDG_CONFIG_HOME", t.TempDir())
				if err := setEngine("fx"); err != nil {
					t.Fatal(err)
				}
				args, stdin := quotaIntegrationFX(t, exit)
				before := quotaIntegrationStorage(t, root)
				cliArgs := "revise\n" + original
				if operation == "ingest" {
					cliArgs = "ingest\nfx"
				}
				cmd := exec.Command(os.Args[0], "-test.run=^TestFXQuotaIntegrationCLIWorker$")
				cmd.Dir = root
				cmd.Env = append(os.Environ(), "PROP_REVIEW_QUOTA_CLI_WORKER=1", "QUOTA_CLI_ARGS="+cliArgs)
				cmd.Stdin = strings.NewReader("permission input\n")
				var stdout, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &stdout, &stderr
				err := cmd.Run()
				var status *exec.ExitError
				if !errors.As(err, &status) || status.ExitCode() != 1 {
					t.Errorf("CLI exit: %v; stdout=%q stderr=%q", err, stdout.String(), stderr.String())
				}
				if !strings.Contains(stderr.String(), quotaIntegrationError+"\n") {
					t.Errorf("CLI stderr missing quota reason: %q", stderr.String())
				}
				if stdout.Len() != 0 {
					t.Errorf("failed CLI printed saved path or resumed: %q", stdout.String())
				}
				if after := quotaIntegrationStorage(t, root); !reflect.DeepEqual(before, after) {
					t.Errorf("CLI quota changed storage: before=%v after=%v", before, after)
				}
				if operation == "ingest" {
					data, err := os.ReadFile(args)
					lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
					if err != nil || len(lines) != 5 || !reflect.DeepEqual(lines[:4], []string{"ask", "--json", "--prompt-permissions", "--"}) || !strings.HasPrefix(lines[4], "Invoke prop-review-ingest") {
						t.Fatalf("ingest must not resume quota-session: %q, %v", data, err)
					}
					data, err = os.ReadFile(stdin)
					if err != nil || string(data) != "permission input\n" {
						t.Fatalf("ingest stdin changed: %q, %v", data, err)
					}
				}
			})
		}
	}
}

func TestFXQuotaIntegrationExplanation(t *testing.T) {
	for _, exit := range []int{0, 1} {
		t.Run(fmt.Sprintf("process-exit-%d", exit), func(t *testing.T) {
			root, _, _ := clarificationFixture(t)
			quotaIntegrationFX(t, exit)
			items := []item{{topic: "Approved decision", answer: "Approved"}, {topic: "Commented decision", answer: "Keep this comment", comment: true}}
			before := append([]item(nil), items...)
			storage := quotaIntegrationStorage(t, root)
			var out bytes.Buffer
			var indices []int
			explain := func(index int, question string) (string, error) {
				indices = append(indices, index)
				answer, err := explainProposal(root, items, index, question, "fx", io.Discard)
				if answer != "" || err == nil || err.Error() != quotaIntegrationError {
					t.Errorf("explanation = %q, %v", answer, err)
				}
				return answer, err
			}
			comment := func(r *bufio.Reader) (string, error) { return readComment(r, &out, nil) }
			if err := reviewWithExplanation(items, strings.NewReader("e\nwhy?\na\ne\nwhy again?\nq\n"), &out, comment, explain); err == nil || err.Error() != "review cancelled; nothing saved" {
				t.Fatalf("expected review cancellation after explanation: %v", err)
			}
			if !reflect.DeepEqual(indices, []int{0, 1}) || !reflect.DeepEqual(items, before) {
				t.Fatalf("explanation changed position/decisions: indices=%v items=%+v", indices, items)
			}
			for _, label := range []string{"[1/2] Approved decision", "[2/2] Commented decision", "Explanation failed:"} {
				if strings.Count(out.String(), label) != 2 {
					t.Errorf("quota must return to same item: missing repeated %q in %q", label, out.String())
				}
			}
			if after := quotaIntegrationStorage(t, root); !reflect.DeepEqual(storage, after) {
				t.Fatal("explanation quota changed stored reviews")
			}
		})
	}
}

func TestFXQuotaIntegrationRecoveryFallbacks(t *testing.T) {
	for _, exit := range []int{0, 1} {
		for _, diagnostic := range []string{"stderr quota reason", ""} {
			t.Run(fmt.Sprintf("exit-%d/stderr-%q", exit, diagnostic), func(t *testing.T) {
				quotaIntegrationFX(t, exit)
				t.Setenv("QUOTA_RESPONSE", strings.Replace(quotaIntegrationResponse, `,"message":"Plan allowance exhausted"`, "", 1))
				t.Setenv("QUOTA_STDERR", diagnostic)
				_, err := proposalsWithTimeout(t.TempDir(), "prompt", "fx", 5*time.Second, io.Discard)
				want := "fx plan usage limit reached"
				if diagnostic != "" {
					want += fmt.Sprintf(": %q", diagnostic)
				}
				if err == nil || err.Error() != want {
					t.Fatalf("fallback error = %v; want %s", err, want)
				}
			})
		}
	}
}

func TestFXQuotaIntegrationRequiresMachineSignal(t *testing.T) {
	for _, response := range []string{
		strings.Replace(quotaIntegrationResponse, `"exit_code":1`, `"exit_code":0`, 1),
		strings.Replace(quotaIntegrationResponse, `"state":"need_user"`, `"state":"other"`, 1),
		strings.Replace(quotaIntegrationResponse, `"need":"quota"`, `"need":"permission"`, 1),
		`{"exit_code":1,"final_output":"fx plan usage limit reached"}`,
	} {
		t.Run(response, func(t *testing.T) {
			quotaIntegrationFX(t, 0)
			t.Setenv("QUOTA_RESPONSE", response)
			t.Setenv("QUOTA_STDERR", "fx plan usage limit reached")
			items, err := proposalsWithTimeout(t.TempDir(), "prompt", "fx", 5*time.Second, io.Discard)
			if err != nil && strings.Contains(err.Error(), "fx plan usage limit reached") {
				t.Fatalf("classified non-quota machine response as quota: %v", err)
			}
			if strings.Contains(response, `"exit_code":0`) && (err != nil || len(items) != 1 || items[0].topic != "must not save") {
				t.Fatalf("successful response rejected: %v, %v", items, err)
			}
		})
	}
}
