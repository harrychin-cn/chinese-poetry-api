package handler

import (
	"strings"
	"testing"
)

func TestConsoleKeyStatusContractExplainsValidationFailures(t *testing.T) {
	for _, expected := range []string{
		"function keyStatusText(k,e)",
		"qk_|sk_",
		"e.status===401",
		"e.status===403",
		"e.status>=500",
		"function createKey()",
		"/api/v1/keys",
		"function rechargeQanlo()",
		"/api/v1/billing/qanlo/recharge-session",
		"error.status=r.status",
	} {
		if !strings.Contains(consoleHTML, expected) {
			t.Fatalf("console key-status contract missing %q", expected)
		}
	}
}

func TestConsoleUsesCreateAndRechargeFlowWithoutSeparateBindingStep(t *testing.T) {
	for _, unexpected := range []string{
		`id="issueKey"`,
		`id="bindQanlo"`,
		"function issueTrialKey()",
	} {
		if strings.Contains(consoleHTML, unexpected) {
			t.Fatalf("console retains retired flow control %q", unexpected)
		}
	}
}
