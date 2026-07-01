package middleware

import (
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

func TestRequestAuditRecordsBillableAPIKeyRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)

	key, rawKey, err := repo.CreateAPIKey(database.CreateAPIKeyParams{Name: "audit customer", DailyLimit: 5})
	require.NoError(t, err)

	router := gin.New()
	router.Use(RequestAudit(repo))
	router.GET("/api/v1/protected", APIKeyAuthWithRecharge(repo, ""), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/protected?q=明月", nil)
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	stats, err := repo.ListUsageEndpointStats(&key.ID, 30, 10)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	assert.Equal(t, "/api/v1/protected", stats[0].Endpoint)
	assert.Equal(t, 1, stats[0].TotalRequests)
	assert.Equal(t, 1, stats[0].BillableRequests)
}

func TestRequestAuditRecordsQuotaExceededWithAPIKeyID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)

	key, rawKey, err := repo.CreateAPIKey(database.CreateAPIKeyParams{Name: "quota customer", DailyLimit: 1})
	require.NoError(t, err)

	router := gin.New()
	router.Use(RequestAudit(repo))
	router.GET("/api/v1/protected", APIKeyAuthWithRecharge(repo, ""), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	firstReq := httptest.NewRequest(http.MethodGet, "/api/v1/protected?q=月", nil)
	firstReq.Header.Set("X-API-Key", rawKey)
	firstW := httptest.NewRecorder()
	router.ServeHTTP(firstW, firstReq)
	assert.Equal(t, http.StatusOK, firstW.Code)

	secondReq := httptest.NewRequest(http.MethodGet, "/api/v1/protected?q=月", nil)
	secondReq.Header.Set("X-API-Key", rawKey)
	secondW := httptest.NewRecorder()
	router.ServeHTTP(secondW, secondReq)
	assert.Equal(t, http.StatusTooManyRequests, secondW.Code)

	stats, err := repo.ListUsageDailyStats(&key.ID, 30)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	assert.Equal(t, 2, stats[0].TotalRequests)
	assert.Equal(t, 1, stats[0].ClientErrorRequests)
}
