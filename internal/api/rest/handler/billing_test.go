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
	"github.com/palemoky/chinese-poetry-api/internal/config"
	"github.com/palemoky/chinese-poetry-api/internal/database"
)

func setupBillingTestRouter(t *testing.T) (*gin.Engine, *database.Repository) {
	return setupBillingTestRouterWithQanloConfig(t, config.QanloConfig{
		AgentBaseURL:     "https://qanlo.com",
		OpenAIBaseURL:    "https://qanlo.com/v1",
		RechargeURL:      "https://qanlo.com/purchase?compact=1&from=agent_key&tab=recharge&ui_mode=embedded",
		AgentAppID:       "agent_test",
		AgentName:        "chinese-poetry-api",
		AgentModel:       "deepseek-v4-flash",
		ReturnURL:        "http://localhost:1279/api/v1/billing/qanlo/callback",
		KeyEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
	})
}

func setupBillingTestRouterWithQanloConfig(t *testing.T, qanloCfg config.QanloConfig) (*gin.Engine, *database.Repository) {
	gin.SetMode(gin.TestMode)

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())

	repo := database.NewRepository(db)
	billingHandler := NewBillingHandler(repo, qanloCfg)

	router := gin.New()
	router.POST("/billing/qanlo/provision", middleware.APIKeyAuthNoUsage(repo), billingHandler.ProvisionQanlo)
	router.POST("/billing/qanlo/recharge-session", middleware.APIKeyAuthNoUsage(repo), billingHandler.CreateQanloRechargeSession)
	router.GET("/billing/qanlo/callback", billingHandler.QanloCallback)
	router.GET("/billing/status", middleware.APIKeyAuthNoUsage(repo), billingHandler.BillingStatus)
	return router, repo
}

func TestBillingQanloFlow(t *testing.T) {
	router, repo := setupBillingTestRouter(t)
	key, rawKey, err := repo.CreateAPIKey(database.CreateAPIKeyParams{
		Name:       "billing customer",
		DailyLimit: 2,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/billing/qanlo/provision", nil)
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var provision map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &provision))
	data := provision["data"].(map[string]any)
	assert.Equal(t, "qanlo", data["provider_name"])
	assert.Contains(t, data["connect_url"], "https://qanlo.com/agent/connect?")
	assert.Contains(t, data["connect_url"], "intent=provision")
	assert.Contains(t, data["connect_url"], "return_mode=query")

	_, usage, err := repo.AuthenticateAndRecordAPIKey(rawKey)
	require.NoError(t, err)
	assert.Equal(t, 1, usage, "billing provision should not consume daily API quota before a real protected API call")

	binding, err := repo.GetQanloBindingByAPIKeyID(key.ID)
	require.NoError(t, err)
	require.NotEmpty(t, binding.CallbackState)

	callbackPath := "/billing/qanlo/callback?state=" + binding.CallbackState + "&key=sk-qanlo-secret&base_url=https%3A%2F%2Fqanlo.com%2Fv1"
	callbackReq := httptest.NewRequest(http.MethodGet, callbackPath, nil)
	callbackW := httptest.NewRecorder()
	router.ServeHTTP(callbackW, callbackReq)
	assert.Equal(t, http.StatusOK, callbackW.Code)
	assert.True(t, strings.Contains(callbackW.Body.String(), "Qanlo"))

	statusReq := httptest.NewRequest(http.MethodGet, "/billing/status", nil)
	statusReq.Header.Set("X-API-Key", rawKey)
	statusW := httptest.NewRecorder()
	router.ServeHTTP(statusW, statusReq)

	assert.Equal(t, http.StatusOK, statusW.Code)
	var status map[string]any
	require.NoError(t, json.Unmarshal(statusW.Body.Bytes(), &status))
	statusData := status["data"].(map[string]any)
	qanloStatus := statusData["qanlo"].(map[string]any)
	assert.Equal(t, "linked", qanloStatus["status"])
	assert.Equal(t, true, qanloStatus["has_qanlo_key"])
}

func TestBillingProvisionWithoutQanloAppIDReturnsActionableFallback(t *testing.T) {
	router, repo := setupBillingTestRouterWithQanloConfig(t, config.QanloConfig{
		AgentBaseURL:     "https://qanlo.com",
		OpenAIBaseURL:    "https://qanlo.com/v1",
		RechargeURL:      "https://qanlo.com/purchase?compact=1&from=agent_key&tab=recharge&ui_mode=embedded",
		AgentName:        "chinese-poetry-api",
		AgentModel:       "deepseek-v4-flash",
		ReturnURL:        "http://localhost:1279/api/v1/billing/qanlo/callback",
		KeyEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
	})
	_, rawKey, err := repo.CreateAPIKey(database.CreateAPIKeyParams{Name: "local customer"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/billing/qanlo/provision", nil)
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var provision map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &provision))
	data := provision["data"].(map[string]any)
	assert.Equal(t, false, data["configured"])
	assert.Equal(t, "", data["connect_url"])
	assert.Contains(t, data["recharge_url"], "https://qanlo.com/purchase")
	assert.Contains(t, data["message"], "QANLO_AGENT_APP_ID")
}

func TestBillingStatusRequiresAPIKey(t *testing.T) {
	router, _ := setupBillingTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/billing/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
