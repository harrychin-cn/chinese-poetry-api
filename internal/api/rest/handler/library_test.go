package handler

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

	"github.com/palemoky/chinese-poetry-api/internal/api/middleware"
	"github.com/palemoky/chinese-poetry-api/internal/database"
)

func setupLibraryTestRouter(t *testing.T) (*gin.Engine, *database.Repository, *database.APIKey, string, *database.APIKey, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)

	authorKey, authorRawKey, err := repo.CreateAPIKey(database.CreateAPIKeyParams{Name: "Library Author", DailyLimit: 10})
	require.NoError(t, err)
	readerKey, readerRawKey, err := repo.CreateAPIKey(database.CreateAPIKeyParams{Name: "Library Reader", DailyLimit: 10})
	require.NoError(t, err)

	handle := "library-author"
	display := "Library Author"
	bio := "public test author"
	_, err = repo.UpdateUserAccountForAPIKey(authorKey.ID, database.UpdateUserAccountParams{
		Handle:      &handle,
		DisplayName: &display,
		Bio:         &bio,
	})
	require.NoError(t, err)

	work, err := repo.CreateOriginalWork(database.CreateOriginalWorkParams{
		APIKeyID:           authorKey.ID,
		Title:              "Moon Library",
		WorkType:           "poem",
		Content:            "moon over lake\nwind under pine",
		Description:        "searchable moon work",
		OriginalCommitment: true,
		LicenseAccepted:    true,
		Publish:            true,
	})
	require.NoError(t, err)
	_, err = repo.CreateOriginalWork(database.CreateOriginalWorkParams{
		APIKeyID:           authorKey.ID,
		Title:              "River Library",
		WorkType:           "ci",
		Content:            "river and lamp",
		OriginalCommitment: true,
		LicenseAccepted:    true,
		Publish:            true,
	})
	require.NoError(t, err)
	_, _, err = repo.GrantCredits(database.GrantCreditsParams{
		APIKeyID:       readerKey.ID,
		Amount:         50,
		Reason:         "test top up",
		IdempotencyKey: "library-top-up",
	})
	require.NoError(t, err)
	_, _, _, err = repo.TipOriginalWork(database.TipOriginalWorkParams{
		FromAPIKeyID:   readerKey.ID,
		WorkID:         work.ID,
		Amount:         7,
		Message:        "great work",
		IdempotencyKey: "library-tip",
	})
	require.NoError(t, err)

	h := NewLibraryHandler(repo)
	router := gin.New()
	router.GET("/library", LibraryPage)
	router.GET("/public/works", h.ListPublicWorks)
	router.GET("/public/rankings/works", h.WorkRankings)
	router.GET("/public/rankings/authors", h.AuthorRankings)
	router.GET("/partners/works/export", middleware.APIKeyAuthNoUsage(repo), h.PartnerExport)
	return router, repo, authorKey, authorRawKey, readerKey, readerRawKey
}

func TestLibraryPublicSearchRankingsAndPartnerExport(t *testing.T) {
	router, _, _, _, _, readerRawKey := setupLibraryTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/public/works?q=moon&sort=tips&lang=en", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), "api_key_id")

	var listResp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listResp))
	listData := listResp["data"].(map[string]any)
	assert.Equal(t, "en", listData["locale"].(map[string]any)["lang"])
	items := listData["items"].([]any)
	require.Len(t, items, 1)
	first := items[0].(map[string]any)
	assert.Equal(t, "Moon Library", first["title"])
	assert.Equal(t, "library-author", first["author"].(map[string]any)["handle"])
	assert.EqualValues(t, 7, first["tip_summary"].(map[string]any)["total_amount"])
	assert.Equal(t, "Poem", first["localized"].(map[string]any)["work_type_label"])

	rankReq := httptest.NewRequest(http.MethodGet, "/public/rankings/works?metric=tips&limit=5", nil)
	rankW := httptest.NewRecorder()
	router.ServeHTTP(rankW, rankReq)
	require.Equal(t, http.StatusOK, rankW.Code, rankW.Body.String())
	assert.Contains(t, rankW.Body.String(), `"metric":"tips"`)
	assert.Contains(t, rankW.Body.String(), `"total_amount":7`)

	authorRankReq := httptest.NewRequest(http.MethodGet, "/public/rankings/authors?metric=tips&limit=5", nil)
	authorRankW := httptest.NewRecorder()
	router.ServeHTTP(authorRankW, authorRankReq)
	require.Equal(t, http.StatusOK, authorRankW.Code, authorRankW.Body.String())
	assert.Contains(t, authorRankW.Body.String(), `"handle":"library-author"`)
	assert.Contains(t, authorRankW.Body.String(), `"public_work_count":2`)

	exportReq := httptest.NewRequest(http.MethodGet, "/partners/works/export?limit=10&lang=en", nil)
	exportReq.Header.Set("X-API-Key", readerRawKey)
	exportW := httptest.NewRecorder()
	router.ServeHTTP(exportW, exportReq)
	require.Equal(t, http.StatusOK, exportW.Code, exportW.Body.String())
	assert.Contains(t, exportW.Body.String(), `"partner_api_version":"stage8-global-library-v1"`)
	assert.Contains(t, exportW.Body.String(), `"license_notice"`)
	assert.Contains(t, exportW.Body.String(), `"certificate_url"`)
}

func TestLibraryPageRendersShell(t *testing.T) {
	router, _, _, _, _, _ := setupLibraryTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/library", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "/api/v1/public/works")
	assert.Contains(t, w.Body.String(), "/api/v1/public/rankings/works")
}
