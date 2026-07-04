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

func setupReverseCreationTestRouter(t *testing.T) (*gin.Engine, *database.Repository, string, int64) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)
	key, rawKey, err := repo.CreateAPIKey(database.CreateAPIKeyParams{Name: "reverse creation customer", DailyLimit: 10})
	require.NoError(t, err)

	h := NewReverseCreationHandler(repo)
	router := gin.New()
	router.POST("/works/reverse-create", middleware.APIKeyAuthNoUsage(repo), h.Create)
	router.GET("/works/reverse-jobs", middleware.APIKeyAuthNoUsage(repo), h.ListJobs)
	return router, repo, rawKey, key.ID
}

func TestReverseCreationDryRunPreviewsDraftAndJob(t *testing.T) {
	router, repo, rawKey, apiKeyID := setupReverseCreationTestRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/works/reverse-create", strings.NewReader(`{
		"source_type":"story",
		"source_text":"\u5c11\u5e74\u591c\u6cca\u6c5f\u8fb9\uff0c\u770b\u6708\u5149\u843d\u5728\u8239\u5934",
		"work_type":"poem",
		"style":"\u6e05\u96c5",
		"dry_run":true
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]any)
	assert.Equal(t, true, data["dry_run"])
	assert.Equal(t, "poem", data["work_type"])
	assert.Contains(t, data["title"], "\u5c11\u5e74")
	assert.Contains(t, data["content"], "\u65b0\u8bd7")
	job := data["job"].(map[string]any)
	assert.Equal(t, database.ImageJobStatusPromptReady, job["status"])
	assert.Equal(t, "story", job["source_type"])

	works, err := repo.ListOriginalWorks(apiKeyID, "all", 10)
	require.NoError(t, err)
	assert.Empty(t, works)

	jobsReq := httptest.NewRequest(http.MethodGet, "/works/reverse-jobs", nil)
	jobsReq.Header.Set("X-API-Key", rawKey)
	jobsW := httptest.NewRecorder()
	router.ServeHTTP(jobsW, jobsReq)
	require.Equal(t, http.StatusOK, jobsW.Code, jobsW.Body.String())
	assert.Contains(t, jobsW.Body.String(), "prompt_ready")
}

func TestReverseCreationSaveCreatesPrivateDraftWork(t *testing.T) {
	router, repo, rawKey, apiKeyID := setupReverseCreationTestRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/works/reverse-create", strings.NewReader(`{
		"source_type":"image",
		"source_text":"\u53e4\u6865\u3001\u96e8\u540e\u9752\u77f3\u8def\u3001\u4e00\u76cf\u5c0f\u706f",
		"image_url":"https://example.test/bridge.png",
		"work_type":"ci",
		"title":"\u96e8\u540e\u53e4\u6865",
		"save":true
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]any)
	assert.Equal(t, false, data["dry_run"])
	assert.Equal(t, true, data["save"])
	assert.Equal(t, "ci", data["work_type"])
	assert.NotEmpty(t, data["content"])
	work := data["work"].(map[string]any)
	assert.Equal(t, "\u96e8\u540e\u53e4\u6865", work["title"])
	assert.Equal(t, "ci", work["work_type"])
	assert.Equal(t, database.WorkStatusDraft, work["status"])
	assert.Equal(t, database.WorkVisibilityPrivate, work["visibility"])
	assert.Equal(t, false, work["original_commitment"])
	assert.Equal(t, false, work["license_accepted"])

	works, err := repo.ListOriginalWorks(apiKeyID, "all", 10)
	require.NoError(t, err)
	require.Len(t, works, 1)
	assert.Equal(t, "\u96e8\u540e\u53e4\u6865", works[0].Title)

	jobs, err := repo.ListReverseCreationJobs(apiKeyID, 10)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, database.ImageJobStatusSucceeded, jobs[0].Status)
	require.NotNil(t, jobs[0].WorkID)
	assert.Equal(t, works[0].ID, *jobs[0].WorkID)
}

func TestReverseCreationRejectsMissingSource(t *testing.T) {
	router, _, rawKey, _ := setupReverseCreationTestRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/works/reverse-create", strings.NewReader(`{
		"source_type":"mood",
		"work_type":"fu"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "source_text or image_url is required")
}
