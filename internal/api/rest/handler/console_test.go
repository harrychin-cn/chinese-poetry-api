package handler

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConsoleUsesPoetryKeyForEnhancedSearchAndKeepsImageKeySeparate(t *testing.T) {
	require.Contains(t, consoleHTML, `"/api/v1/knowledge/recall?q="`)
	require.Contains(t, consoleHTML, `{"X-API-Key":key}`)
	require.Contains(t, consoleHTML, `"X-Image-API-Key":ik`)
	require.Contains(t, consoleHTML, `已使用诗词服务 Key`)
	require.True(t, strings.Contains(consoleHTML, "image_api_key:ik"))
}
