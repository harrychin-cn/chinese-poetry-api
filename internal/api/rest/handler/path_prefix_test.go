package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRenderProductContentPrefixesOnlyProductURLs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/console", nil)
	c.Request.Header.Set("X-Forwarded-Prefix", "/poetry-api/")

	content := `<a href="/"></a><a href="/console"></a><script>path.split("/");fetch("/api/v1/health");</script><img src="/console-placeholder-bg.png"><code>__POETRY_API_BASE_PATH__</code><code>__POETRY_DEPLOYMENT_BASE_PATH__</code>`
	rendered := string(renderProductContent(c, content))

	require.Contains(t, rendered, `href="/poetry-api/"`)
	require.Contains(t, rendered, `href="/poetry-api/console"`)
	require.Contains(t, rendered, `fetch("/poetry-api/api/v1/health")`)
	require.Contains(t, rendered, `src="/poetry-api/console-placeholder-bg.png"`)
	require.Contains(t, rendered, `path.split("/")`)
	require.Contains(t, rendered, `<code>/poetry-api/api/v1</code>`)
	require.Contains(t, rendered, `<code>/poetry-api</code>`)
	require.NotContains(t, rendered, `/poetry-api/poetry-api/`)
}

func TestRenderProductContentFallsBackForInvalidForwardedPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/console", nil)
	c.Request.Header.Set("X-Forwarded-Prefix", `/poetry-api\"><script>`)

	rendered := string(renderProductContent(c, `<a href="/console"></a><code>__POETRY_API_BASE_PATH__</code><code>__POETRY_DEPLOYMENT_BASE_PATH__</code>`))

	require.Contains(t, rendered, `href="/console"`)
	require.Contains(t, rendered, `<code>/api/v1</code>`)
	require.Contains(t, rendered, `<code>/</code>`)
	require.NotContains(t, rendered, `<script>`)
}

func TestRenderProductHTMLAddsBridgeOnlyForTrustedPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)

	validWriter := httptest.NewRecorder()
	validContext, _ := gin.CreateTestContext(validWriter)
	validContext.Request = httptest.NewRequest("GET", "/console", nil)
	validContext.Request.Header.Set("X-Forwarded-Prefix", "/poetry-api")
	valid := string(renderProductHTML(validContext, `<html><head></head><body></body></html>`))
	require.Contains(t, valid, `data-poetry-prefix-bridge`)
	require.Contains(t, valid, `const base="/poetry-api"`)

	rootWriter := httptest.NewRecorder()
	rootContext, _ := gin.CreateTestContext(rootWriter)
	rootContext.Request = httptest.NewRequest("GET", "/console", nil)
	root := string(renderProductHTML(rootContext, `<html><head></head><body></body></html>`))
	require.NotContains(t, root, `data-poetry-prefix-bridge`)
}
