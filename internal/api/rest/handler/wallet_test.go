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

func setupWalletTestRouter(t *testing.T) (*gin.Engine, *database.Repository, string, *database.APIKey, string, *database.APIKey, *database.OriginalWork) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)

	authorKey, authorRawKey, err := repo.CreateAPIKey(database.CreateAPIKeyParams{Name: "wallet author", DailyLimit: 10})
	require.NoError(t, err)
	tipperKey, tipperRawKey, err := repo.CreateAPIKey(database.CreateAPIKeyParams{Name: "wallet tipper", DailyLimit: 10})
	require.NoError(t, err)

	work, err := repo.CreateOriginalWork(database.CreateOriginalWorkParams{
		APIKeyID:           authorKey.ID,
		Title:              "River Lamp",
		WorkType:           "poem",
		Content:            "river lamp at dusk\nsmall boat under stars",
		OriginalCommitment: true,
		LicenseAccepted:    true,
		Publish:            true,
	})
	require.NoError(t, err)
	require.Equal(t, database.WorkStatusPublished, work.Status)

	h := NewWalletHandler(repo)
	router := gin.New()
	router.GET("/wallet", middleware.APIKeyAuthNoUsage(repo), h.Get)
	router.GET("/wallet/transactions", middleware.APIKeyAuthNoUsage(repo), h.Transactions)
	router.POST("/wallet/top-up", middleware.APIKeyAuthNoUsage(repo), h.TopUp)
	router.POST("/works/:id/tip", middleware.APIKeyAuthNoUsage(repo), h.TipWork)
	router.GET("/works/:id/tips", middleware.APIKeyAuthNoUsage(repo), h.ListWorkTips)
	return router, repo, tipperRawKey, tipperKey, authorRawKey, authorKey, work
}

func TestWalletTopUpAndTransactions(t *testing.T) {
	router, repo, tipperRawKey, tipperKey, _, _, _ := setupWalletTestRouter(t)

	getReq := httptest.NewRequest(http.MethodGet, "/wallet", nil)
	getReq.Header.Set("X-API-Key", tipperRawKey)
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)
	require.Equal(t, http.StatusOK, getW.Code, getW.Body.String())
	assert.Contains(t, getW.Body.String(), "initial_image_credits")

	reqBody := `{"amount":15,"reason":"test_top_up","idempotency_key":"topup-test"}`
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/wallet/top-up", strings.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", tipperRawKey)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		wallet := resp["data"].(map[string]any)["wallet"].(map[string]any)
		assert.Equal(t, float64(35), wallet["balance"])
	}

	transactions, err := repo.ListCreditTransactions(tipperKey.ID, 10)
	require.NoError(t, err)
	require.Len(t, transactions, 2)
	assert.Equal(t, "test_top_up", transactions[0].Reason)
	assert.Equal(t, 15, transactions[0].Amount)
	assert.Equal(t, 35, transactions[0].BalanceAfter)
}

func TestWorkTipTransfersCreditsAndListsSummary(t *testing.T) {
	router, repo, tipperRawKey, _, _, authorKey, work := setupWalletTestRouter(t)

	body := `{"amount":7,"message":"beautiful work","idempotency_key":"tip-once"}`
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/works/"+formatTestID(work.ID)+"/tip", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", tipperRawKey)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		data := resp["data"].(map[string]any)
		sender := data["sender_wallet"].(map[string]any)
		recipient := data["recipient_wallet"].(map[string]any)
		assert.Equal(t, float64(13), sender["balance"])
		assert.Equal(t, float64(7), recipient["balance"])
	}

	listReq := httptest.NewRequest(http.MethodGet, "/works/"+formatTestID(work.ID)+"/tips", nil)
	listReq.Header.Set("X-API-Key", tipperRawKey)
	listW := httptest.NewRecorder()
	router.ServeHTTP(listW, listReq)
	require.Equal(t, http.StatusOK, listW.Code, listW.Body.String())
	var listResp map[string]any
	require.NoError(t, json.Unmarshal(listW.Body.Bytes(), &listResp))
	data := listResp["data"].(map[string]any)
	summary := data["summary"].(map[string]any)
	assert.Equal(t, float64(1), summary["tip_count"])
	assert.Equal(t, float64(7), summary["total_amount"])
	items := data["items"].([]any)
	require.Len(t, items, 1)

	authorTxs, err := repo.ListCreditTransactions(authorKey.ID, 10)
	require.NoError(t, err)
	require.Len(t, authorTxs, 1)
	assert.Equal(t, "work_tip_received", authorTxs[0].Reason)
	assert.Equal(t, 7, authorTxs[0].Amount)
}

func TestWorkTipRejectsInsufficientCreditsAndSelfTip(t *testing.T) {
	router, _, tipperRawKey, _, authorRawKey, _, work := setupWalletTestRouter(t)

	tooMuch := httptest.NewRequest(http.MethodPost, "/works/"+formatTestID(work.ID)+"/tip", strings.NewReader(`{"amount":25}`))
	tooMuch.Header.Set("Content-Type", "application/json")
	tooMuch.Header.Set("X-API-Key", tipperRawKey)
	tooMuchW := httptest.NewRecorder()
	router.ServeHTTP(tooMuchW, tooMuch)
	assert.Equal(t, http.StatusPaymentRequired, tooMuchW.Code)

	self := httptest.NewRequest(http.MethodPost, "/works/"+formatTestID(work.ID)+"/tip", strings.NewReader(`{"amount":1}`))
	self.Header.Set("Content-Type", "application/json")
	self.Header.Set("X-API-Key", authorRawKey)
	selfW := httptest.NewRecorder()
	router.ServeHTTP(selfW, self)
	assert.Equal(t, http.StatusBadRequest, selfW.Code)
}
