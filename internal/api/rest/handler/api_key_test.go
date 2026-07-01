package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/palemoky/chinese-poetry-api/internal/api/middleware"
	"github.com/palemoky/chinese-poetry-api/internal/config"
	"github.com/palemoky/chinese-poetry-api/internal/database"
)

func setupAPIKeyTestRouter(t *testing.T) (*gin.Engine, *database.Repository) {
	gin.SetMode(gin.TestMode)

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)

	h := NewAPIKeyHandler(repo, config.APIAuthConfig{DefaultDailyLimit: 100})
	router := gin.New()
	router.POST("/keys", h.CreateClientAPIKey)
	admin := router.Group("/admin", middleware.AdminAuth("test-admin-token"))
	admin.POST("/api-keys", h.CreateAPIKey)
	admin.GET("/api-keys", h.ListAPIKeys)
	admin.PATCH("/api-keys/:id", h.UpdateAPIKey)
	admin.DELETE("/api-keys/:id", h.RevokeAPIKey)
	return router, repo
}

func TestAPIKeyAdminUpdateFlow(t *testing.T) {
	router, _ := setupAPIKeyTestRouter(t)

	createReq := httptest.NewRequest(http.MethodPost, "/admin/api-keys", strings.NewReader(`{
		"name":"demo customer",
		"tier":"trial",
		"daily_limit":5,
		"notes":"first contact"
	}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("X-Admin-Token", "test-admin-token")
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)
	require.Equal(t, http.StatusCreated, createW.Code)

	var createResp map[string]any
	require.NoError(t, json.Unmarshal(createW.Body.Bytes(), &createResp))
	keyID := int64(createResp["data"].(map[string]any)["id"].(float64))

	updateReq := httptest.NewRequest(http.MethodPatch, "/admin/api-keys/"+formatTestID(keyID), strings.NewReader(`{
		"name":"paid customer",
		"tier":"pro",
		"daily_limit":30,
		"enabled":false,
		"notes":"upgraded manually"
	}`))
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.Header.Set("X-Admin-Token", "test-admin-token")
	updateW := httptest.NewRecorder()
	router.ServeHTTP(updateW, updateReq)

	assert.Equal(t, http.StatusOK, updateW.Code)
	var updateResp map[string]any
	require.NoError(t, json.Unmarshal(updateW.Body.Bytes(), &updateResp))
	data := updateResp["data"].(map[string]any)
	assert.Equal(t, "paid customer", data["name"])
	assert.Equal(t, "pro", data["tier"])
	assert.Equal(t, float64(30), data["daily_limit"])
	assert.Equal(t, false, data["enabled"])
	assert.Equal(t, "upgraded manually", data["notes"])
	assert.NotNil(t, data["revoked_at"])
}
