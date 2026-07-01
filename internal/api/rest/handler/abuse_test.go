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
	"github.com/palemoky/chinese-poetry-api/internal/database"
)

func setupAbuseHandlerRouter(t *testing.T) (*gin.Engine, *database.Repository) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)

	h := NewAbuseHandler(repo)
	router := gin.New()
	admin := router.Group("/admin", middleware.AdminAuth("test-admin-token"))
	admin.GET("/abuse/blocks", h.ListBlocks)
	admin.POST("/abuse/blocks", h.CreateBlock)
	admin.PATCH("/abuse/blocks/:id", h.UpdateBlock)
	return router, repo
}

func TestAbuseHandlerCreateListUpdate(t *testing.T) {
	router, _ := setupAbuseHandlerRouter(t)

	createReq := httptest.NewRequest(http.MethodPost, "/admin/abuse/blocks", strings.NewReader(`{
		"target_type":"ip",
		"target_value":"203.0.113.66",
		"reason":"manual smoke",
		"ttl_minutes":30,
		"notes":"temporary"
	}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("X-Admin-Token", "test-admin-token")
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)
	require.Equal(t, http.StatusCreated, createW.Code)

	var createResp map[string]any
	require.NoError(t, json.Unmarshal(createW.Body.Bytes(), &createResp))
	data := createResp["data"].(map[string]any)
	blockID := int64(data["id"].(float64))
	assert.Equal(t, "ip", data["target_type"])
	assert.Equal(t, "203.0.113.66", data["target_value"])

	listReq := httptest.NewRequest(http.MethodGet, "/admin/abuse/blocks?active_only=true", nil)
	listReq.Header.Set("X-Admin-Token", "test-admin-token")
	listW := httptest.NewRecorder()
	router.ServeHTTP(listW, listReq)
	require.Equal(t, http.StatusOK, listW.Code)
	assert.Contains(t, listW.Body.String(), "203.0.113.66")

	updateReq := httptest.NewRequest(http.MethodPatch, "/admin/abuse/blocks/"+formatTestID(blockID), strings.NewReader(`{
		"enabled":false,
		"notes":"released"
	}`))
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.Header.Set("X-Admin-Token", "test-admin-token")
	updateW := httptest.NewRecorder()
	router.ServeHTTP(updateW, updateReq)
	require.Equal(t, http.StatusOK, updateW.Code)
	assert.Contains(t, updateW.Body.String(), `"enabled":false`)
	assert.Contains(t, updateW.Body.String(), "released")
}
