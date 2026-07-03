package rest

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/palemoky/chinese-poetry-api/internal/config"
	"github.com/palemoky/chinese-poetry-api/internal/database"
)

type routeContract struct {
	Method string
	Path   string
}

var commercialRouteContracts = []routeContract{
	{http.MethodPost, "/api/v1/keys"},
	{http.MethodGet, "/api/v1/keys/current"},
	{http.MethodPost, "/api/v1/billing/qanlo/provision"},
	{http.MethodPost, "/api/v1/billing/qanlo/recharge-session"},
	{http.MethodGet, "/api/v1/billing/qanlo/callback"},
	{http.MethodGet, "/api/v1/billing/status"},
	{http.MethodGet, "/api/v1/poems/query"},
	{http.MethodGet, "/api/v1/poems/search/fulltext"},
	{http.MethodGet, "/api/v1/knowledge/recall"},
	{http.MethodPost, "/api/v1/knowledge/batch"},
	{http.MethodPost, "/api/v1/images/generate"},
	{http.MethodGet, "/api/v1/usage/daily"},
	{http.MethodGet, "/api/v1/usage/endpoints"},
	{http.MethodGet, "/api/v1/usage/queries"},
	{http.MethodPost, "/api/v1/feedback"},
	{http.MethodGet, "/api/v1/public/works/:code"},
	{http.MethodPost, "/api/v1/works"},
	{http.MethodGet, "/api/v1/works"},
	{http.MethodGet, "/api/v1/works/:id"},
	{http.MethodPatch, "/api/v1/works/:id"},
	{http.MethodPost, "/api/v1/works/:id/publish"},
	{http.MethodGet, "/api/v1/works/:id/versions"},
	{http.MethodGet, "/api/v1/works/:id/license-acceptances"},
	{http.MethodGet, "/api/v1/works/:id/plagiarism-report"},
	{http.MethodGet, "/api/v1/works/:id/media-assets"},
	{http.MethodGet, "/api/v1/works/:id/image-jobs"},
	{http.MethodPost, "/api/v1/works/:id/images/generate"},
	{http.MethodPost, "/api/v1/admin/api-keys"},
	{http.MethodGet, "/api/v1/admin/api-keys"},
	{http.MethodPatch, "/api/v1/admin/api-keys/:id"},
	{http.MethodDelete, "/api/v1/admin/api-keys/:id"},
	{http.MethodGet, "/api/v1/admin/abuse/blocks"},
	{http.MethodPost, "/api/v1/admin/abuse/blocks"},
	{http.MethodPatch, "/api/v1/admin/abuse/blocks/:id"},
	{http.MethodPost, "/api/v1/admin/search/rebuild"},
	{http.MethodPost, "/api/v1/admin/tags"},
	{http.MethodPost, "/api/v1/admin/poems/:id/tags"},
	{http.MethodGet, "/api/v1/admin/usage/daily"},
	{http.MethodGet, "/api/v1/admin/usage/endpoints"},
	{http.MethodGet, "/api/v1/admin/usage/queries"},
	{http.MethodGet, "/api/v1/admin/feedback"},
	{http.MethodPatch, "/api/v1/admin/feedback/:id"},
	{http.MethodPost, "/api/v1/admin/enrichment/jobs"},
	{http.MethodGet, "/api/v1/admin/enrichment/jobs"},
	{http.MethodGet, "/api/v1/admin/enrichment/runs/:run_id/summary"},
	{http.MethodPost, "/api/v1/admin/enrichment/review-items"},
	{http.MethodGet, "/api/v1/admin/enrichment/review-items"},
	{http.MethodPatch, "/api/v1/admin/enrichment/review-items/:id"},
	{http.MethodPost, "/api/v1/admin/enrichment/review-items/:id/accept"},
	{http.MethodPost, "/api/v1/admin/enrichment/review-items/:id/reject"},
}

func TestRouterCommercialContractIsRegisteredAndDocumented(t *testing.T) {
	router := setupRouterContractTestRouter(t)

	registered := make(map[string]bool)
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = true
	}

	docs := readRepoFile(t, "docs", "commercial-api-keys.md")
	for _, contract := range commercialRouteContracts {
		routeKey := contract.Method + " " + contract.Path
		require.True(t, registered[routeKey], "router must register %s", routeKey)
		require.Contains(t, docs, routeKey, "commercial API docs must mention %s", routeKey)
	}
}

func TestBuiltInCommercialPagesRenderCoreEntrypoints(t *testing.T) {
	router := setupRouterContractTestRouter(t)

	cases := []struct {
		path     string
		contains []string
	}{
		{
			path: "/console",
			contains: []string{
				"/docs",
				"/pricing",
				"POST /api/v1/keys",
				"/api/v1/billing/status",
				"/api/v1/knowledge/recall",
				"/api/v1/images/generate",
				"/api/v1/feedback",
				"/api/v1/works",
				"/api/v1/works/:id/plagiarism-report",
				"console-placeholder-bg.png",
				"画中题诗",
				"不要像背景图上后贴图案",
			},
		},
		{
			path: "/docs",
			contains: []string{
				"/console",
				"/pricing",
				"/openapi.yaml",
				"POST /api/v1/keys",
				"POST /api/v1/billing/qanlo/recharge-session",
				"GET /api/v1/billing/status",
				"GET /api/v1/knowledge/recall",
				"POST /api/v1/images/generate",
				"GET /api/v1/admin/enrichment/runs/:run_id/summary",
				"GET /api/v1/admin/abuse/blocks",
			},
		},
		{
			path: "/pricing",
			contains: []string{
				"/console",
				"/docs",
				"/api/v1/poems/query",
				"/api/v1/knowledge/recall",
				"/api/v1/usage/daily",
				"/api/v1/feedback",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)
			body := w.Body.String()
			for _, expected := range tc.contains {
				require.Contains(t, body, expected)
			}
		})
	}
}

func TestConsolePlaceholderImageRouteRendersPaintingAsset(t *testing.T) {
	router := setupRouterContractTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/console-placeholder-bg.png", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "image/png")
	require.Greater(t, w.Body.Len(), 1024)
}

func TestOpenAPIYAMLIsRegisteredAndDocumentsCoreRoutes(t *testing.T) {
	router := setupRouterContractTestRouter(t)

	registered := make(map[string]bool)
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	require.True(t, registered[http.MethodGet+" /openapi.yaml"], "router must register GET /openapi.yaml")

	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "application/yaml")

	body := w.Body.String()
	require.Contains(t, body, "openapi: 3.0.3")
	require.Contains(t, body, "paths:")
	require.Contains(t, body, "X-API-Key")
	require.Contains(t, body, "X-Admin-Token")
	require.Contains(t, body, "/api/v1/poems/query:")
	require.Contains(t, body, "/api/v1/knowledge/recall:")
	require.Contains(t, body, "/api/v1/keys:")
	require.Contains(t, body, "/api/v1/billing/status:")
	require.Contains(t, body, "/api/v1/admin/enrichment/review-items/{id}/accept:")

	for _, contract := range commercialRouteContracts {
		require.Contains(t, body, openAPIPath(contract.Path), "OpenAPI YAML must mention %s", contract.Path)
	}
}

func setupRouterContractTestRouter(t *testing.T) *gin.Engine {
	t.Helper()

	gin.SetMode(gin.TestMode)
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)

	cfg := &config.Config{
		Server: config.ServerConfig{
			Mode: gin.TestMode,
			Port: 1279,
		},
		RateLimit: config.RateLimitConfig{
			Enabled:                 false,
			RequestsPerSecond:       10,
			Burst:                   20,
			APIKeyRequestsPerSecond: 2,
			APIKeyBurst:             5,
		},
		APIAuth: config.APIAuthConfig{
			Enabled:           true,
			AdminToken:        "test-admin-token",
			DefaultDailyLimit: 1000,
		},
		Qanlo: config.QanloConfig{
			AgentBaseURL:  "https://qanlo.test",
			OpenAIBaseURL: "https://qanlo.test/v1",
			RechargeURL:   "https://qanlo.test/recharge?compact=1",
			AgentName:     "chinese-poetry-api",
			AgentModel:    "gpt-test",
			ReturnURL:     "http://localhost:1279/api/v1/billing/qanlo/callback",
		},
	}

	return SetupRouter(cfg, db, repo)
}

func openAPIPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = "{" + strings.TrimPrefix(part, ":") + "}"
		}
	}
	return strings.Join(parts, "/") + ":"
}

func readRepoFile(t *testing.T, parts ...string) string {
	t.Helper()

	candidates := [][]string{
		append([]string{"..", "..", ".."}, parts...),
		append([]string{"..", "..", "..", ".."}, parts...),
		parts,
	}
	for _, candidate := range candidates {
		path := filepath.Join(candidate...)
		content, err := os.ReadFile(path)
		if err == nil {
			return string(content)
		}
	}

	require.FailNow(t, "failed to read repo file", strings.Join(parts, string(filepath.Separator)))
	return ""
}
