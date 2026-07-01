package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/palemoky/chinese-poetry-api/internal/api/middleware"
	"github.com/palemoky/chinese-poetry-api/internal/database"
)

func setupUsageTestRouter(t *testing.T) (*gin.Engine, *database.Repository, string, int64) {
	gin.SetMode(gin.TestMode)

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)
	key, rawKey, err := repo.CreateAPIKey(database.CreateAPIKeyParams{Name: "usage customer", DailyLimit: 10})
	require.NoError(t, err)
	require.NoError(t, repo.RecordAPIRequest(database.RecordAPIRequestParams{
		APIKeyID:       &key.ID,
		Method:         "GET",
		Path:           "/api/v1/poems/query",
		Endpoint:       "/api/v1/poems/query",
		StatusCode:     200,
		LatencyMs:      9,
		Billable:       true,
		QueryText:      "q=明月",
		QuerySignature: "/api/v1/poems/query?q=明月",
		CreatedAt:      time.Now().UTC(),
	}))

	h := NewUsageHandler(repo)
	router := gin.New()
	router.GET("/usage/daily", middleware.APIKeyAuthNoUsage(repo), h.ClientDaily)
	router.GET("/usage/endpoints", middleware.APIKeyAuthNoUsage(repo), h.ClientEndpoints)
	router.GET("/usage/queries", middleware.APIKeyAuthNoUsage(repo), h.ClientQueries)
	admin := router.Group("/admin", middleware.AdminAuth("test-admin-token"))
	admin.GET("/usage/daily", h.AdminDaily)
	admin.GET("/usage/endpoints", h.AdminEndpoints)
	admin.GET("/usage/queries", h.AdminQueries)

	return router, repo, rawKey, key.ID
}

func TestUsageClientEndpoints(t *testing.T) {
	router, repo, rawKey, keyID := setupUsageTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/usage/endpoints?days=30", nil)
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]any)
	items := data["items"].([]any)
	require.Len(t, items, 1)
	item := items[0].(map[string]any)
	assert.Equal(t, "/api/v1/poems/query", item["endpoint"])

	usage, err := repo.GetAPIKeyUsageCount(keyID, time.Now().UTC().Format("2006-01-02"))
	require.NoError(t, err)
	assert.Equal(t, 0, usage, "usage inspection endpoint should not consume quota")
}

func TestUsageAdminDaily(t *testing.T) {
	router, _, _, _ := setupUsageTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/daily", nil)
	req.Header.Set("X-Admin-Token", "test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]any)
	items := data["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, float64(1), items[0].(map[string]any)["total_requests"])
}
