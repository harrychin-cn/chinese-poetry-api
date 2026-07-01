package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

func TestRateLimiterByAPIKeyToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)

	key1, rawKey1, err := repo.CreateAPIKey(database.CreateAPIKeyParams{Name: "key1", DailyLimit: 10})
	require.NoError(t, err)
	_, rawKey2, err := repo.CreateAPIKey(database.CreateAPIKeyParams{Name: "key2", DailyLimit: 10})
	require.NoError(t, err)

	limiter := NewRateLimiter(1, 1)
	router := gin.New()
	router.GET("/protected", limiter.APIKeyTokenMiddleware(), APIKeyAuthWithRecharge(repo, ""), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req1 := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req1.Header.Set("X-API-Key", rawKey1)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	require.Equal(t, http.StatusOK, w1.Code)

	req2 := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req2.Header.Set("X-API-Key", rawKey1)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusTooManyRequests, w2.Code)
	assert.Contains(t, w2.Body.String(), `"scope":"api_key"`)

	req3 := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req3.Header.Set("X-API-Key", rawKey2)
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	require.Equal(t, http.StatusOK, w3.Code, "different API keys should not share the short-cycle limiter")

	usage, err := repo.GetAPIKeyUsageCount(key1.ID, time.Now().UTC().Format("2006-01-02"))
	require.NoError(t, err)
	assert.LessOrEqual(t, usage, 1, "rate-limited request should be blocked before daily quota is consumed")
}
