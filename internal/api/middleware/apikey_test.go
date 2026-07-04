package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

func TestAdminAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/admin", AdminAuth("secret-token"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	t.Run("reject missing token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("accept header token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req.Header.Set("X-Admin-Token", "secret-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("accept bearer token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req.Header.Set("Authorization", "Bearer secret-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestAPIKeyAuthNoUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)

	key, rawKey, err := repo.CreateAPIKey(database.CreateAPIKeyParams{
		Name:       "billing customer",
		DailyLimit: 1,
	})
	require.NoError(t, err)

	router := gin.New()
	router.GET("/billing/status", APIKeyAuthNoUsage(repo), func(c *gin.Context) {
		apiKeyID, _ := c.Get("api_key_id")
		c.JSON(http.StatusOK, gin.H{"api_key_id": apiKeyID})
	})

	req := httptest.NewRequest(http.MethodGet, "/billing/status", nil)
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "1", w.Header().Get("X-API-Key-ID"))

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, float64(key.ID), body["api_key_id"])

	_, usage, err := repo.AuthenticateAndRecordAPIKey(rawKey)
	require.NoError(t, err)
	assert.Equal(t, 1, usage)
}

func TestAPIKeyAuthRejectsQueryParameterKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)

	_, rawKey, err := repo.CreateAPIKey(database.CreateAPIKeyParams{
		Name:       "query customer",
		DailyLimit: 5,
	})
	require.NoError(t, err)

	router := gin.New()
	router.GET("/query", APIKeyAuth(repo), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/query?api_key="+rawKey, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "api key required")
}

func TestAPIKeyAuthWithRechargeQuotaExceeded(t *testing.T) {
	gin.SetMode(gin.TestMode)

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)

	_, rawKey, err := repo.CreateAPIKey(database.CreateAPIKeyParams{
		Name:       "quota customer",
		DailyLimit: 1,
	})
	require.NoError(t, err)

	router := gin.New()
	router.GET("/query", APIKeyAuthWithRecharge(repo, "https://qanlo.com/purchase?compact=1"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/query", nil)
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	req = httptest.NewRequest(http.MethodGet, "/query", nil)
	req.Header.Set("X-API-Key", rawKey)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "daily api quota exceeded", body["error"])
	assert.Equal(t, "https://qanlo.com/purchase?compact=1", body["recharge_url"])
	assert.Equal(t, "/api/v1/billing/qanlo/recharge-session", body["recharge_endpoint"])
}
