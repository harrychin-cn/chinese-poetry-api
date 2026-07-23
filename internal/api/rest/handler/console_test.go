package handler

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConsoleUsesPoetryKeyForModelSearchAndKeepsImageKeySeparate(t *testing.T) {
	require.Contains(t, consoleHTML, `"/api/v1/poems/search/ai?q="`)
	require.Contains(t, consoleHTML, `{"X-API-Key":key}`)
	require.Contains(t, consoleHTML, `"X-Image-API-Key":ik`)
	require.True(t, strings.Contains(consoleHTML, "image_api_key:ik"))
}

func TestConsoleDoesNotContainBrokenQanloKeyCopy(t *testing.T) {
	require.NotContains(t, consoleHTML, "\u7cbe\u7b80\u5145\u503c\u6d41\u7a0b?")
	require.Contains(t, consoleHTML, "Qanlo \u751f\u56fe Key\uff0c\u8bf7\u586b\u5199\u5230\u4e0b\u65b9")
}
