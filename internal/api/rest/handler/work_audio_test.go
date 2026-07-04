package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

func setupWorkAudioTestRouter(t *testing.T, audioCfg config.AudioConfig, mediaCfg config.ImageConfig) (*gin.Engine, *database.Repository, string, *database.OriginalWork) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if strings.TrimSpace(mediaCfg.StorageDir) == "" {
		mediaCfg.StorageDir = t.TempDir()
	}
	if strings.TrimSpace(mediaCfg.PublicBasePath) == "" {
		mediaCfg.PublicBasePath = "/media-assets"
	}

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)
	key, rawKey, err := repo.CreateAPIKey(database.CreateAPIKeyParams{Name: "work audio customer", DailyLimit: 10})
	require.NoError(t, err)
	work, err := repo.CreateOriginalWork(database.CreateOriginalWorkParams{
		APIKeyID:    key.ID,
		Title:       "\u5c71\u7a97\u591c\u5750",
		WorkType:    "poem",
		Content:     "\u5c71\u7a97\u706f\u5f71\u8584\n\u4e00\u76cf\u7167\u6e05\u98ce",
		ImagePrompt: "\u53e4\u98ce\u6c34\u58a8\uff0c\u5c0f\u7a97\u591c\u706f\uff0c\u6e05\u98ce\u5165\u68a6",
		ChangeNote:  "create",
		Description: "test work",
	})
	require.NoError(t, err)

	audioHandler := NewWorkAudioHandler(repo, audioCfg, mediaCfg)
	imageHandler := NewWorkImageHandler(repo, mediaCfg)
	router := gin.New()
	router.POST("/works/:id/audio/generate", middleware.APIKeyAuthNoUsage(repo), audioHandler.GenerateAudio)
	router.POST("/works/:id/music/generate", middleware.APIKeyAuthNoUsage(repo), audioHandler.GenerateMusic)
	router.GET("/works/:id/audio-jobs", middleware.APIKeyAuthNoUsage(repo), audioHandler.ListAudioJobs)
	router.GET("/works/:id/music-jobs", middleware.APIKeyAuthNoUsage(repo), audioHandler.ListMusicJobs)
	router.GET("/works/:id/media-assets", middleware.APIKeyAuthNoUsage(repo), imageHandler.ListMediaAssets)
	return router, repo, rawKey, work
}

func TestWorkAudioGenerateDryRunPreparesJobWithoutQuota(t *testing.T) {
	router, repo, rawKey, work := setupWorkAudioTestRouter(t, config.AudioConfig{
		BaseURL:        "https://qanlo.test/openai/v1",
		Model:          "gpt-4o-mini-tts",
		Voice:          "alloy",
		OutputFormat:   "mp3",
		TimeoutSeconds: 5,
	}, config.ImageConfig{})

	req := httptest.NewRequest(http.MethodPost, "/works/"+formatTestID(work.ID)+"/audio/generate", strings.NewReader(`{
		"style":"\u53e4\u98ce\u541f\u8bf5",
		"background_style":"\u53e4\u7434\u6e05\u96c5",
		"dry_run":true
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "prompt_ready")
	assert.Contains(t, w.Body.String(), "\u5c71\u7a97")

	usage, err := repo.GetAPIKeyUsageCount(work.APIKeyID, time.Now().UTC().Format("2006-01-02"))
	require.NoError(t, err)
	assert.Equal(t, 0, usage, "dry_run must not consume quota")

	jobsReq := httptest.NewRequest(http.MethodGet, "/works/"+formatTestID(work.ID)+"/audio-jobs", nil)
	jobsReq.Header.Set("X-API-Key", rawKey)
	jobsW := httptest.NewRecorder()
	router.ServeHTTP(jobsW, jobsReq)
	require.Equal(t, http.StatusOK, jobsW.Code, jobsW.Body.String())
	assert.Contains(t, jobsW.Body.String(), "prompt_ready")

	assetsReq := httptest.NewRequest(http.MethodGet, "/works/"+formatTestID(work.ID)+"/media-assets?asset_type=audio", nil)
	assetsReq.Header.Set("X-API-Key", rawKey)
	assetsW := httptest.NewRecorder()
	router.ServeHTTP(assetsW, assetsReq)
	require.Equal(t, http.StatusOK, assetsW.Code, assetsW.Body.String())
	var assetsResp map[string]any
	require.NoError(t, json.Unmarshal(assetsW.Body.Bytes(), &assetsResp))
	assert.Empty(t, assetsResp["data"].(map[string]any)["items"])
}

func TestWorkAudioGenerateStoresRecitationAssetAndJob(t *testing.T) {
	audioBytes := []byte("ID3testaudio")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/openai/v1/audio/speech", r.URL.Path)
		require.Equal(t, "Bearer test-audio-key", r.Header.Get("Authorization"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "gpt-4o-mini-tts", body["model"])
		assert.Equal(t, "alloy", body["voice"])
		assert.Equal(t, "mp3", body["response_format"])
		input, _ := body["input"].(string)
		assert.Contains(t, input, "\u5c71\u7a97\u591c\u5750")
		assert.Contains(t, input, "\u4e00\u76cf\u7167\u6e05\u98ce")
		instructions, _ := body["instructions"].(string)
		assert.Contains(t, instructions, "\u53e4\u98ce\u541f\u8bf5")
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write(audioBytes)
	}))
	defer upstream.Close()

	router, repo, rawKey, work := setupWorkAudioTestRouter(t, config.AudioConfig{
		APIKey:         "test-audio-key",
		BaseURL:        upstream.URL + "/openai/v1",
		Model:          "gpt-4o-mini-tts",
		Voice:          "alloy",
		OutputFormat:   "mp3",
		TimeoutSeconds: 5,
		CreditCost:     1,
		InitialCredits: 20,
	}, config.ImageConfig{})

	req := httptest.NewRequest(http.MethodPost, "/works/"+formatTestID(work.ID)+"/audio/generate", strings.NewReader(`{
		"style":"\u53e4\u98ce\u541f\u8bf5",
		"background_style":"\u53e4\u7434\u6e05\u96c5",
		"prompt":"\u5c3e\u53e5\u7559\u767d"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "succeeded")
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]any)
	assert.Equal(t, float64(1), data["credit_cost"])
	assert.Contains(t, data["audio_url"].(string), "/media-assets/audio/")
	credits := data["credits"].(map[string]any)
	assert.Equal(t, float64(19), credits["balance"])

	usage, err := repo.GetAPIKeyUsageCount(work.APIKeyID, time.Now().UTC().Format("2006-01-02"))
	require.NoError(t, err)
	assert.Equal(t, 1, usage)

	assets, err := repo.ListWorkMediaAssets(work.APIKeyID, work.ID, database.MediaAssetTypeAudio, 10)
	require.NoError(t, err)
	require.Len(t, assets, 1)
	assert.Equal(t, database.MediaAssetTypeAudio, assets[0].AssetType)
	assert.Equal(t, "audio/mpeg", assets[0].MimeType)
	assert.True(t, strings.HasPrefix(assets[0].URL, "/media-assets/audio/"))
	assert.Equal(t, "local", assets[0].StorageProvider)
	assert.Equal(t, int64(len(audioBytes)), assets[0].ByteSize)
	assert.Equal(t, 1, assets[0].CreditCost)
	_, err = os.Stat(assets[0].FilePath)
	require.NoError(t, err)

	jobs, err := repo.ListWorkAudioJobs(work.APIKeyID, work.ID, 10)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, database.ImageJobStatusSucceeded, jobs[0].Status)
	require.NotNil(t, jobs[0].MediaAssetID)
}

func TestWorkMusicGenerateStoresDraftAssetAndJob(t *testing.T) {
	router, repo, rawKey, work := setupWorkAudioTestRouter(t, config.AudioConfig{
		BaseURL:         "https://qanlo.test/openai/v1",
		Model:           "gpt-4o-mini-tts",
		Voice:           "alloy",
		OutputFormat:    "mp3",
		TimeoutSeconds:  5,
		MusicCreditCost: 0,
		InitialCredits:  20,
	}, config.ImageConfig{})

	req := httptest.NewRequest(http.MethodPost, "/works/"+formatTestID(work.ID)+"/music/generate", strings.NewReader(`{
		"mode":"background",
		"music_style":"\u53e4\u7434\u6e05\u96c5",
		"prompt":"\u4eba\u58f0\u4e0b\u65b9\u8f7b\u58f0\u94fa\u5e95"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]any)
	assert.Equal(t, float64(0), data["credit_cost"])
	assert.Contains(t, data["music_url"].(string), "/media-assets/music/")

	assets, err := repo.ListWorkMediaAssets(work.APIKeyID, work.ID, database.MediaAssetTypeMusic, 10)
	require.NoError(t, err)
	require.Len(t, assets, 1)
	assert.Equal(t, database.MediaAssetTypeMusic, assets[0].AssetType)
	assert.Equal(t, "application/json", assets[0].MimeType)
	assert.Equal(t, "stage4-music-draft", assets[0].Model)
	assert.Equal(t, 0, assets[0].CreditCost)
	_, err = os.Stat(assets[0].FilePath)
	require.NoError(t, err)

	jobs, err := repo.ListWorkMusicJobs(work.APIKeyID, work.ID, 10)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, database.ImageJobStatusSucceeded, jobs[0].Status)
	require.NotNil(t, jobs[0].MediaAssetID)
}

func TestWorkAudioGenerateRejectsInsufficientCreditsBeforeUpstream(t *testing.T) {
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3testaudio"))
	}))
	defer upstream.Close()

	router, repo, rawKey, work := setupWorkAudioTestRouter(t, config.AudioConfig{
		APIKey:         "test-audio-key",
		BaseURL:        upstream.URL + "/openai/v1",
		Model:          "gpt-4o-mini-tts",
		Voice:          "alloy",
		OutputFormat:   "mp3",
		TimeoutSeconds: 5,
		CreditCost:     3,
		InitialCredits: 2,
	}, config.ImageConfig{})

	req := httptest.NewRequest(http.MethodPost, "/works/"+formatTestID(work.ID)+"/audio/generate", strings.NewReader(`{
		"style":"\u53e4\u98ce\u541f\u8bf5"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusPaymentRequired, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "insufficient audio credits")
	assert.Equal(t, 0, upstreamCalls, "credit check should happen before upstream audio call")

	usage, err := repo.GetAPIKeyUsageCount(work.APIKeyID, time.Now().UTC().Format("2006-01-02"))
	require.NoError(t, err)
	assert.Equal(t, 0, usage)
	wallet, err := repo.GetCreditWalletByAPIKeyID(work.APIKeyID)
	require.NoError(t, err)
	assert.Equal(t, 2, wallet.Balance)
}
