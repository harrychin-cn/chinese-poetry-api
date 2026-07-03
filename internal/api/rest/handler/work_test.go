package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/palemoky/chinese-poetry-api/internal/api/middleware"
	"github.com/palemoky/chinese-poetry-api/internal/config"
	"github.com/palemoky/chinese-poetry-api/internal/database"
)

func setupWorkTestRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)
	_, rawKey, err := repo.CreateAPIKey(database.CreateAPIKeyParams{Name: "works customer", DailyLimit: 10})
	require.NoError(t, err)

	h := NewWorkHandler(repo)
	router := gin.New()
	router.POST("/works", middleware.APIKeyAuthNoUsage(repo), h.Create)
	router.GET("/works", middleware.APIKeyAuthNoUsage(repo), h.List)
	router.GET("/works/:id", middleware.APIKeyAuthNoUsage(repo), h.Get)
	router.PATCH("/works/:id", middleware.APIKeyAuthNoUsage(repo), h.Update)
	router.POST("/works/:id/publish", middleware.APIKeyAuthNoUsage(repo), h.Publish)
	router.GET("/works/:id/versions", middleware.APIKeyAuthNoUsage(repo), h.Versions)
	router.GET("/works/:id/license-acceptances", middleware.APIKeyAuthNoUsage(repo), h.LicenseAcceptances)
	router.GET("/works/:id/plagiarism-report", middleware.APIKeyAuthNoUsage(repo), h.PlagiarismReport)
	router.GET("/public/works/:code", h.PublicGet)
	return router, rawKey
}

func TestWorkHandlerCreateUpdatePublishPublicFlow(t *testing.T) {
	router, rawKey := setupWorkTestRouter(t)

	createReq := httptest.NewRequest(http.MethodPost, "/works", strings.NewReader(`{
		"title":"Mountain Window",
		"work_type":"poem",
		"content":"line one\nline two",
		"original_commitment":true,
		"license_accepted":true,
		"publish":true
	}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("X-API-Key", rawKey)
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)
	require.Equal(t, http.StatusCreated, createW.Code, createW.Body.String())

	var createResp map[string]any
	require.NoError(t, json.Unmarshal(createW.Body.Bytes(), &createResp))
	work := createResp["data"].(map[string]any)
	id := int64(work["id"].(float64))
	code := work["work_code"].(string)
	assert.Equal(t, "published", work["status"])

	updateReq := httptest.NewRequest(http.MethodPatch, "/works/"+formatTestID(id), strings.NewReader(`{
		"content":"line one\nline two\nline three",
		"change_note":"add third line"
	}`))
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.Header.Set("X-API-Key", rawKey)
	updateW := httptest.NewRecorder()
	router.ServeHTTP(updateW, updateReq)
	require.Equal(t, http.StatusOK, updateW.Code, updateW.Body.String())

	versionsReq := httptest.NewRequest(http.MethodGet, "/works/"+formatTestID(id)+"/versions", nil)
	versionsReq.Header.Set("X-API-Key", rawKey)
	versionsW := httptest.NewRecorder()
	router.ServeHTTP(versionsW, versionsReq)
	require.Equal(t, http.StatusOK, versionsW.Code)
	var versionsResp map[string]any
	require.NoError(t, json.Unmarshal(versionsW.Body.Bytes(), &versionsResp))
	require.Len(t, versionsResp["data"].(map[string]any)["items"].([]any), 2)

	licenseReq := httptest.NewRequest(http.MethodGet, "/works/"+formatTestID(id)+"/license-acceptances", nil)
	licenseReq.Header.Set("X-API-Key", rawKey)
	licenseW := httptest.NewRecorder()
	router.ServeHTTP(licenseW, licenseReq)
	require.Equal(t, http.StatusOK, licenseW.Code)

	reportReq := httptest.NewRequest(http.MethodGet, "/works/"+formatTestID(id)+"/plagiarism-report", nil)
	reportReq.Header.Set("X-API-Key", rawKey)
	reportW := httptest.NewRecorder()
	router.ServeHTTP(reportW, reportReq)
	require.Equal(t, http.StatusOK, reportW.Code)
	var reportResp map[string]any
	require.NoError(t, json.Unmarshal(reportW.Body.Bytes(), &reportResp))
	report := reportResp["data"].(map[string]any)
	assert.Equal(t, "low", report["risk_level"])

	publicReq := httptest.NewRequest(http.MethodGet, "/public/works/"+code, nil)
	publicW := httptest.NewRecorder()
	router.ServeHTTP(publicW, publicReq)
	require.Equal(t, http.StatusOK, publicW.Code)
}

func TestWorkHandlerRejectsPublishWithoutLicense(t *testing.T) {
	router, rawKey := setupWorkTestRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/works", strings.NewReader(`{
		"title":"Draft",
		"content":"line",
		"publish":true
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func setupWorkImageTestRouter(t *testing.T, cfg config.ImageConfig) (*gin.Engine, *database.Repository, string, *database.OriginalWork) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)
	key, rawKey, err := repo.CreateAPIKey(database.CreateAPIKeyParams{Name: "work image customer", DailyLimit: 10})
	require.NoError(t, err)
	work, err := repo.CreateOriginalWork(database.CreateOriginalWorkParams{
		APIKeyID:    key.ID,
		Title:       "山窗夜坐",
		WorkType:    "poem",
		Content:     "山窗灯影薄\n一盏照清风",
		ImagePrompt: "古风水墨，小窗夜灯，清风入梦",
		ChangeNote:  "create",
		Description: "test work",
	})
	require.NoError(t, err)

	h := NewWorkImageHandler(repo, cfg)
	router := gin.New()
	router.POST("/works/:id/images/generate", middleware.APIKeyAuthNoUsage(repo), h.Generate)
	router.GET("/works/:id/media-assets", middleware.APIKeyAuthNoUsage(repo), h.ListMediaAssets)
	router.GET("/works/:id/image-jobs", middleware.APIKeyAuthNoUsage(repo), h.ListImageJobs)
	return router, repo, rawKey, work
}

func TestWorkImageGenerateDryRunPreparesPromptWithoutQuota(t *testing.T) {
	router, repo, rawKey, work := setupWorkImageTestRouter(t, config.ImageConfig{
		BaseURL:        "https://qanlo.test/openai/v1",
		Model:          "gpt-image-2",
		Quality:        "high",
		OutputFormat:   "png",
		TimeoutSeconds: 5,
	})

	req := httptest.NewRequest(http.MethodPost, "/works/"+formatTestID(work.ID)+"/images/generate", strings.NewReader(`{
		"style":"古风水墨",
		"size":"1024x1024",
		"dry_run":true
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "prompt_ready")
	assert.Contains(t, w.Body.String(), "画中题诗")

	usage, err := repo.GetAPIKeyUsageCount(1, time.Now().UTC().Format("2006-01-02"))
	require.NoError(t, err)
	assert.Equal(t, 0, usage, "dry_run must not consume quota")

	jobsReq := httptest.NewRequest(http.MethodGet, "/works/"+formatTestID(work.ID)+"/image-jobs", nil)
	jobsReq.Header.Set("X-API-Key", rawKey)
	jobsW := httptest.NewRecorder()
	router.ServeHTTP(jobsW, jobsReq)
	require.Equal(t, http.StatusOK, jobsW.Code, jobsW.Body.String())
	assert.Contains(t, jobsW.Body.String(), "prompt_ready")

	assetsReq := httptest.NewRequest(http.MethodGet, "/works/"+formatTestID(work.ID)+"/media-assets?asset_type=image", nil)
	assetsReq.Header.Set("X-API-Key", rawKey)
	assetsW := httptest.NewRecorder()
	router.ServeHTTP(assetsW, assetsReq)
	require.Equal(t, http.StatusOK, assetsW.Code, assetsW.Body.String())
	var assetsResp map[string]any
	require.NoError(t, json.Unmarshal(assetsW.Body.Bytes(), &assetsResp))
	assert.Empty(t, assetsResp["data"].(map[string]any)["items"])
}

func TestWorkImageGenerateStoresAssetAndJob(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/openai/v1/images/generations", r.URL.Path)
		require.Equal(t, "Bearer test-image-key", r.Header.Get("Authorization"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "gpt-image-2", body["model"])
		assert.Equal(t, "b64_json", body["response_format"])
		assert.Equal(t, "1024x1024", body["size"])
		prompt, _ := body["prompt"].(string)
		assert.Contains(t, prompt, "画中题诗")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"aGVsbG8=","revised_prompt":"古风水墨题诗画"}]}`))
	}))
	defer upstream.Close()

	router, repo, rawKey, work := setupWorkImageTestRouter(t, config.ImageConfig{
		APIKey:         "test-image-key",
		BaseURL:        upstream.URL + "/openai/v1",
		Model:          "gpt-image-2",
		Quality:        "high",
		OutputFormat:   "png",
		TimeoutSeconds: 5,
	})

	req := httptest.NewRequest(http.MethodPost, "/works/"+formatTestID(work.ID)+"/images/generate", strings.NewReader(`{
		"style":"古风水墨",
		"size":"1024x1024",
		"prompt":"题诗自然融入窗边留白"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "data:image/png;base64,aGVsbG8=")
	assert.Contains(t, w.Body.String(), "succeeded")

	usage, err := repo.GetAPIKeyUsageCount(1, time.Now().UTC().Format("2006-01-02"))
	require.NoError(t, err)
	assert.Equal(t, 1, usage)

	assets, err := repo.ListWorkMediaAssets(1, work.ID, database.MediaAssetTypeImage, 10)
	require.NoError(t, err)
	require.Len(t, assets, 1)
	assert.Equal(t, "image/png", assets[0].MimeType)

	jobs, err := repo.ListWorkImageJobs(1, work.ID, 10)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, database.ImageJobStatusSucceeded, jobs[0].Status)
	require.NotNil(t, jobs[0].MediaAssetID)
}

func TestWorkImageGenerateUsesCachedAssetWithoutQuota(t *testing.T) {
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"aGVsbG8=","revised_prompt":"古风水墨题诗画"}]}`))
	}))
	defer upstream.Close()

	router, repo, rawKey, work := setupWorkImageTestRouter(t, config.ImageConfig{
		APIKey:         "test-image-key",
		BaseURL:        upstream.URL + "/openai/v1",
		Model:          "gpt-image-2",
		Quality:        "high",
		OutputFormat:   "png",
		TimeoutSeconds: 5,
	})

	body := `{"style":"古风水墨","size":"1024x1024","prompt":"题诗自然融入窗边留白"}`
	req := httptest.NewRequest(http.MethodPost, "/works/"+formatTestID(work.ID)+"/images/generate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, 1, upstreamCalls)

	req = httptest.NewRequest(http.MethodPost, "/works/"+formatTestID(work.ID)+"/images/generate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", rawKey)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, 1, upstreamCalls, "same work image request should reuse cached asset")
	assert.Contains(t, w.Body.String(), `"cached":true`)
	assert.Contains(t, w.Body.String(), "data:image/png;base64,aGVsbG8=")

	usage, err := repo.GetAPIKeyUsageCount(1, time.Now().UTC().Format("2006-01-02"))
	require.NoError(t, err)
	assert.Equal(t, 1, usage, "cache hit must not consume another quota unit")

	jobs, err := repo.ListWorkImageJobs(1, work.ID, 10)
	require.NoError(t, err)
	require.Len(t, jobs, 2)
	assert.Equal(t, database.ImageJobStatusSucceeded, jobs[0].Status)
	require.NotNil(t, jobs[0].MediaAssetID)
	require.NotNil(t, jobs[1].MediaAssetID)
	assert.Equal(t, *jobs[1].MediaAssetID, *jobs[0].MediaAssetID)
}
