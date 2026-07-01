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

func setupFeedbackTestRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)
	_, rawKey, err := repo.CreateAPIKey(database.CreateAPIKeyParams{Name: "feedback customer", DailyLimit: 10})
	require.NoError(t, err)

	h := NewFeedbackHandler(repo)
	router := gin.New()
	router.POST("/feedback", middleware.APIKeyAuthNoUsage(repo), h.Create)
	admin := router.Group("/admin", middleware.AdminAuth("test-admin-token"))
	admin.GET("/feedback", h.List)
	admin.PATCH("/feedback/:id", h.Update)
	return router, rawKey
}

func TestFeedbackClientAndAdminFlow(t *testing.T) {
	router, rawKey := setupFeedbackTestRouter(t)

	createReq := httptest.NewRequest(http.MethodPost, "/feedback", strings.NewReader(`{
		"type":"data",
		"subject":"missing mid autumn poems",
		"message":"希望补更多中秋月亮诗句",
		"contact":"wechat"
	}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("X-API-Key", rawKey)
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)
	require.Equal(t, http.StatusCreated, createW.Code)

	var createResp map[string]any
	require.NoError(t, json.Unmarshal(createW.Body.Bytes(), &createResp))
	itemID := int64(createResp["data"].(map[string]any)["id"].(float64))

	listReq := httptest.NewRequest(http.MethodGet, "/admin/feedback?status=open", nil)
	listReq.Header.Set("X-Admin-Token", "test-admin-token")
	listW := httptest.NewRecorder()
	router.ServeHTTP(listW, listReq)
	require.Equal(t, http.StatusOK, listW.Code)
	var listResp map[string]any
	require.NoError(t, json.Unmarshal(listW.Body.Bytes(), &listResp))
	items := listResp["data"].(map[string]any)["items"].([]any)
	require.Len(t, items, 1)

	updateReq := httptest.NewRequest(http.MethodPatch, "/admin/feedback/"+formatTestID(itemID), strings.NewReader(`{
		"status":"resolved",
		"admin_notes":"已加入增强队列"
	}`))
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.Header.Set("X-Admin-Token", "test-admin-token")
	updateW := httptest.NewRecorder()
	router.ServeHTTP(updateW, updateReq)
	require.Equal(t, http.StatusOK, updateW.Code)
	var updateResp map[string]any
	require.NoError(t, json.Unmarshal(updateW.Body.Bytes(), &updateResp))
	data := updateResp["data"].(map[string]any)
	assert.Equal(t, "resolved", data["status"])
	assert.Equal(t, "已加入增强队列", data["admin_notes"])
}

func TestFeedbackRequiresAPIKey(t *testing.T) {
	router, _ := setupFeedbackTestRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/feedback", strings.NewReader(`{"message":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
