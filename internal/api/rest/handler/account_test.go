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

func setupAccountTestRouter(t *testing.T) (*gin.Engine, *database.Repository, *database.APIKey, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)
	key, rawKey, err := repo.CreateAPIKey(database.CreateAPIKeyParams{Name: "works author", DailyLimit: 10})
	require.NoError(t, err)

	h := NewAccountHandler(repo)
	router := gin.New()
	router.GET("/account", middleware.APIKeyAuthNoUsage(repo), h.Current)
	router.PATCH("/account", middleware.APIKeyAuthNoUsage(repo), h.Update)
	router.GET("/public/users/:handle", h.PublicProfile)
	router.GET("/public/users/:handle/works", h.PublicWorks)
	return router, repo, key, rawKey
}

func TestAccountHandlerCurrentUpdateAndPublicProfile(t *testing.T) {
	router, repo, key, rawKey := setupAccountTestRouter(t)

	updateReq := httptest.NewRequest(http.MethodPatch, "/account", strings.NewReader(`{
		"handle":"demo-author",
		"display_name":"Demo Author",
		"bio":"writes test poems"
	}`))
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.Header.Set("X-API-Key", rawKey)
	updateW := httptest.NewRecorder()
	router.ServeHTTP(updateW, updateReq)
	require.Equal(t, http.StatusOK, updateW.Code, updateW.Body.String())
	assert.Contains(t, updateW.Body.String(), `"handle":"demo-author"`)

	_, err := repo.CreateOriginalWork(database.CreateOriginalWorkParams{
		APIKeyID:           key.ID,
		Title:              "Demo Work",
		Content:            "line one\nline two",
		OriginalCommitment: true,
		LicenseAccepted:    true,
		Publish:            true,
	})
	require.NoError(t, err)

	profileReq := httptest.NewRequest(http.MethodGet, "/public/users/demo-author", nil)
	profileW := httptest.NewRecorder()
	router.ServeHTTP(profileW, profileReq)
	require.Equal(t, http.StatusOK, profileW.Code, profileW.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(profileW.Body.Bytes(), &resp))
	data := resp["data"].(map[string]any)
	assert.Equal(t, "demo-author", data["handle"])
	assert.EqualValues(t, 1, data["public_work_count"])
	require.Len(t, data["works"].([]any), 1)
}

func TestAccountHandlerRejectsDuplicateHandle(t *testing.T) {
	router, repo, _, rawKey1 := setupAccountTestRouter(t)
	_, rawKey2, err := repo.CreateAPIKey(database.CreateAPIKeyParams{Name: "second author", DailyLimit: 10})
	require.NoError(t, err)

	body := `{"handle":"same-author","display_name":"Same Author"}`
	req := httptest.NewRequest(http.MethodPatch, "/account", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", rawKey1)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	req = httptest.NewRequest(http.MethodPatch, "/account", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", rawKey2)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)
}
