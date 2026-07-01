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

func setupAbuseMiddlewareRepo(t *testing.T) *database.Repository {
	t.Helper()

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	return database.NewRepository(db)
}

func TestAbuseBlocklistBlocksIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := setupAbuseMiddlewareRepo(t)

	_, err := repo.UpsertAbuseBlock(database.AbuseBlockParams{
		TargetType:  database.AbuseTargetIP,
		TargetValue: "203.0.113.44",
		Reason:      "manual test",
	})
	require.NoError(t, err)

	router := gin.New()
	router.Use(AbuseBlocklist(repo))
	router.GET("/ok", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "203.0.113.44:1234"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "request blocked")
}

func TestAbuseDetectorAutoBlocksRepeatedFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := setupAbuseMiddlewareRepo(t)

	detector := NewAbuseDetector(repo, 2, time.Minute, time.Hour)
	router := gin.New()
	router.Use(detector.Middleware())
	router.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.RemoteAddr = "203.0.113.45:1234"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code)
	}

	block, err := repo.FindActiveAbuseBlock(database.AbuseTargetIP, "203.0.113.45")
	require.NoError(t, err)
	require.NotNil(t, block)
	assert.Equal(t, "auto", block.CreatedBy)
}
