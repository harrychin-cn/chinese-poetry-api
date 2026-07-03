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

func setupImageHandlerTest(t *testing.T, cfg config.ImageConfig, dailyLimit int) (*gin.Engine, *database.Repository, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)

	_, rawKey, err := repo.CreateAPIKey(database.CreateAPIKeyParams{
		Name:       "image customer",
		Tier:       "trial",
		DailyLimit: dailyLimit,
	})
	require.NoError(t, err)

	router := gin.New()
	router.POST("/images/generate", middleware.APIKeyAuthNoUsage(repo), NewImageHandler(repo, cfg).Generate)
	return router, repo, rawKey
}

func TestGenerateImageRequiresServerSideImageKey(t *testing.T) {
	router, repo, rawKey := setupImageHandlerTest(t, config.ImageConfig{
		Enabled:        true,
		BaseURL:        "https://qanlo.test/openai/v1",
		Model:          "gpt-image-2",
		TimeoutSeconds: 5,
	}, 1)

	req := httptest.NewRequest(http.MethodPost, "/images/generate", strings.NewReader(`{"prompt":"古风水墨山水","size":"1024x1024"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "image_config_missing")

	usage, err := repo.GetAPIKeyUsageCount(1, time.Now().UTC().Format("2006-01-02"))
	require.NoError(t, err)
	assert.Equal(t, 0, usage, "missing IMAGE_API_KEY must not consume quota")
}

func TestGenerateImageDisabledBeforeGatewayKey(t *testing.T) {
	router, repo, rawKey := setupImageHandlerTest(t, config.ImageConfig{
		Enabled:        false,
		APIKey:         "test-image-key",
		BaseURL:        "https://qanlo.test/openai/v1",
		Model:          "gpt-image-2",
		TimeoutSeconds: 5,
	}, 1)

	req := httptest.NewRequest(http.MethodPost, "/images/generate", strings.NewReader(`{"prompt":"鍙ら姘村ⅷ灞辨按","size":"1024x1024"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "image_generation_disabled")

	usage, err := repo.GetAPIKeyUsageCount(1, time.Now().UTC().Format("2006-01-02"))
	require.NoError(t, err)
	assert.Equal(t, 0, usage, "disabled image generation must not consume quota")
}

func TestGenerateImageProxiesAndReturnsDataURL(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/openai/v1/images/generations", r.URL.Path)
		require.Equal(t, "Bearer test-image-key", r.Header.Get("Authorization"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "gpt-image-2", body["model"])
		assert.Equal(t, "b64_json", body["response_format"])
		assert.Equal(t, "1024x1536", body["size"])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"aGVsbG8=","revised_prompt":"水墨山川"}]}`))
	}))
	defer upstream.Close()

	router, repo, rawKey := setupImageHandlerTest(t, config.ImageConfig{
		Enabled:        true,
		APIKey:         "test-image-key",
		BaseURL:        upstream.URL + "/openai/v1",
		Model:          "gpt-image-2",
		Quality:        "high",
		OutputFormat:   "png",
		TimeoutSeconds: 5,
	}, 2)

	req := httptest.NewRequest(http.MethodPost, "/images/generate", strings.NewReader(`{"prompt":"古风水墨山水","size":"1024x1536"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "data:image/png;base64,aGVsbG8=")

	usage, err := repo.GetAPIKeyUsageCount(1, time.Now().UTC().Format("2006-01-02"))
	require.NoError(t, err)
	assert.Equal(t, 1, usage, "successful image generation should consume one local quota")
}

func TestGenerateImageAcceptsRequestScopedImageKey(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer request-image-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"aGVsbG8="}]}`))
	}))
	defer upstream.Close()

	router, repo, rawKey := setupImageHandlerTest(t, config.ImageConfig{
		Enabled:        true,
		BaseURL:        upstream.URL + "/openai/v1",
		Model:          "gpt-image-2",
		Quality:        "high",
		OutputFormat:   "png",
		TimeoutSeconds: 5,
	}, 2)

	req := httptest.NewRequest(http.MethodPost, "/images/generate", strings.NewReader(`{"prompt":"古风水墨山水","size":"1024x1024"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", rawKey)
	req.Header.Set("X-Image-API-Key", "request-image-key")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "request-image-key")

	usage, err := repo.GetAPIKeyUsageCount(1, time.Now().UTC().Format("2006-01-02"))
	require.NoError(t, err)
	assert.Equal(t, 1, usage)
}
