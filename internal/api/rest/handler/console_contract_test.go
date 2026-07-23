package handler

import (
	"strings"
	"testing"
)

func TestConsoleKeyStatusContractExplainsValidationFailures(t *testing.T) {
	for _, expected := range []string{
		"function keyStatusText(k,e)",
		"请填 cp_live API Key",
		"Key 未开通或已撤销",
		"Key 已被封禁",
		"校验服务异常",
		"error.status=r.status",
	} {
		if !strings.Contains(consoleHTML, expected) {
			t.Fatalf("console key-status contract missing %q", expected)
		}
	}
}
