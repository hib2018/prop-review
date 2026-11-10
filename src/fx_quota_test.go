package main

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFxQuotaFixture(t *testing.T) {
	output, err := os.ReadFile("testdata/fx-quota/stdout.json")
	if err != nil {
		t.Fatal(err)
	}
	if got := fxUsageLimitError(output, ""); got == nil || got.Error() != `fx plan usage limit reached: "Out of credits"` {
		t.Fatalf("quota: %v", got)
	}
	if got, err := fxResponse(output); got != nil || err == nil || !strings.Contains(err.Error(), "Out of credits") {
		t.Fatalf("response: %q, %v", got, err)
	}
	stderr, err := os.ReadFile("testdata/fx-quota/stderr.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, exit := range []int{0, 1} {
		quotaIntegrationFX(t, exit)
		t.Setenv("QUOTA_RESPONSE", string(output))
		t.Setenv("QUOTA_STDERR", string(stderr))
		got, err := engineResponse(t.TempDir(), "prompt", "fx", 5*time.Second, io.Discard, "test")
		if got != nil || err == nil || err.Error() != `fx plan usage limit reached: "Out of credits"` {
			t.Fatalf("fixture process exit %d: %q, %v", exit, got, err)
		}
	}
}

func TestFxQuotaRejectsUnconfirmedSignals(t *testing.T) {
	for _, output := range []string{
		`not JSON`, `null`, `{}`, `{"exit_code":null,"recovery":{"state":"need_user","need":"quota"}}`,
		`{"exit_code":1,"recovery":null}`, `{"exit_code":1,"recovery":{"state":"failed","cause":"rate_limited"}}`,
		`{"exit_code":1,"recovery":{"state":"failed","cause":"request_limit_reached"}}`,
		`{"exit_code":1,"recovery":{"state":"need_user","need":"sign_in"}}`,
		`{"exit_code":1,"recovery":{"state":"need_user","need":"quota","message":42}}`,
		quotaIntegrationResponse + " trailing",
	} {
		if err := fxUsageLimitError([]byte(output), "Out of credits"); err != nil {
			t.Fatalf("misclassified %s: %v", output, err)
		}
	}
}

func TestFxQuotaReasonEscaped(t *testing.T) {
	output := []byte(strings.Replace(quotaIntegrationResponse, "Plan allowance exhausted", `bad\u001b[2J\nreason`, 1))
	err := fxUsageLimitError(output, "")
	if err == nil || strings.ContainsAny(err.Error(), "\x1b\n") || !strings.Contains(err.Error(), `\x1b[2J\nreason`) {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestFxQuotaFlowAndPiIsolation(t *testing.T) {
	for _, exit := range []int{0, 1} {
		quotaIntegrationFX(t, exit)
		got, err := flowEngineResponse(t.TempDir(), "prompt", "fx", io.Discard)
		if got != nil || err == nil || err.Error() != quotaIntegrationError {
			t.Fatalf("flow exit %d: %q, %v", exit, got, err)
		}
	}
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	limitScript(t, bin, "npm", "printf '/fake/modules\\n'")
	limitScript(t, bin, "node", `cat >/dev/null
printf '%s' "$QUOTA_RESPONSE"
printf 'Out of credits' >&2
exit 1`)
	_, err := engineResponse(t.TempDir(), "prompt", "pi", 5*time.Second, io.Discard, "test")
	if err == nil || !strings.HasPrefix(err.Error(), "pi failed:") || strings.Contains(err.Error(), "fx plan usage limit") {
		t.Fatalf("Pi misclassified: %v", err)
	}
}
