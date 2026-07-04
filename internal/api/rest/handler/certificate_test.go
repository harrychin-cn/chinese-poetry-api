package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/palemoky/chinese-poetry-api/internal/api/middleware"
	"github.com/palemoky/chinese-poetry-api/internal/database"
)

func setupCertificateTestRouter(t *testing.T) (*gin.Engine, *database.Repository, string, *database.APIKey, *database.OriginalWork) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)

	key, rawKey, err := repo.CreateAPIKey(database.CreateAPIKeyParams{Name: "certificate customer", DailyLimit: 10})
	require.NoError(t, err)
	work, err := repo.CreateOriginalWork(database.CreateOriginalWorkParams{
		APIKeyID:           key.ID,
		Title:              "Certificate Work",
		WorkType:           "poem",
		Content:            "river moon enters the window\nold pine answers the bell",
		OriginalCommitment: true,
		LicenseAccepted:    true,
		Publish:            true,
	})
	require.NoError(t, err)
	require.Equal(t, database.WorkStatusPublished, work.Status)
	require.NotEmpty(t, work.WorkCode)

	h := NewCertificateHandler(repo)
	router := gin.New()
	router.GET("/certificates/:code", CertificatePage)
	router.POST("/works/:id/certificate", middleware.APIKeyAuthNoUsage(repo), h.Issue)
	router.GET("/works/:id/certificate", middleware.APIKeyAuthNoUsage(repo), h.Get)
	router.POST("/works/:id/certificate/anchor", middleware.APIKeyAuthNoUsage(repo), h.Anchor)
	router.GET("/public/works/:code/certificate", h.PublicGet)
	return router, repo, rawKey, key, work
}

func readCertificateData(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data, ok := resp["data"].(map[string]any)
	require.True(t, ok, "certificate response must include data object")
	return data
}

func TestCertificateIssueGetPublicAndAnchorFlow(t *testing.T) {
	router, _, rawKey, _, work := setupCertificateTestRouter(t)

	issueReq := httptest.NewRequest(http.MethodPost, "/works/"+formatTestID(work.ID)+"/certificate", nil)
	issueReq.Header.Set("X-API-Key", rawKey)
	issueW := httptest.NewRecorder()
	router.ServeHTTP(issueW, issueReq)
	require.Equal(t, http.StatusOK, issueW.Code, issueW.Body.String())
	issued := readCertificateData(t, issueW)
	assert.Equal(t, "CERT-"+work.WorkCode+"-V1", issued["certificate_code"])
	assert.Equal(t, work.WorkCode, issued["work_code"])
	assert.Len(t, issued["certificate_hash"].(string), 64)
	assert.Len(t, issued["signature"].(string), 64)
	assert.Equal(t, "local_anchored", issued["anchor_status"])
	assert.Equal(t, "/certificates/"+work.WorkCode, issued["public_certificate_url"])

	time.Sleep(1100 * time.Millisecond)
	repeatReq := httptest.NewRequest(http.MethodPost, "/works/"+formatTestID(work.ID)+"/certificate", nil)
	repeatReq.Header.Set("X-API-Key", rawKey)
	repeatW := httptest.NewRecorder()
	router.ServeHTTP(repeatW, repeatReq)
	require.Equal(t, http.StatusOK, repeatW.Code, repeatW.Body.String())
	repeated := readCertificateData(t, repeatW)
	assert.Equal(t, issued["certificate_code"], repeated["certificate_code"])
	assert.Equal(t, issued["certificate_hash"], repeated["certificate_hash"])
	assert.Equal(t, issued["anchor_tx_id"], repeated["anchor_tx_id"])

	getReq := httptest.NewRequest(http.MethodGet, "/works/"+formatTestID(work.ID)+"/certificate", nil)
	getReq.Header.Set("X-API-Key", rawKey)
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)
	require.Equal(t, http.StatusOK, getW.Code, getW.Body.String())
	got := readCertificateData(t, getW)
	assert.Equal(t, issued["certificate_hash"], got["certificate_hash"])

	publicReq := httptest.NewRequest(http.MethodGet, "/public/works/"+work.WorkCode+"/certificate", nil)
	publicW := httptest.NewRecorder()
	router.ServeHTTP(publicW, publicReq)
	require.Equal(t, http.StatusOK, publicW.Code, publicW.Body.String())
	publicData := readCertificateData(t, publicW)
	assert.Equal(t, issued["certificate_hash"], publicData["certificate_hash"])

	anchorReq := httptest.NewRequest(http.MethodPost, "/works/"+formatTestID(work.ID)+"/certificate/anchor", nil)
	anchorReq.Header.Set("X-API-Key", rawKey)
	anchorW := httptest.NewRecorder()
	router.ServeHTTP(anchorW, anchorReq)
	require.Equal(t, http.StatusOK, anchorW.Code, anchorW.Body.String())
	anchor := readCertificateData(t, anchorW)
	assert.Equal(t, issued["anchor_tx_id"], anchor["anchor_tx_id"])
}

func TestCertificateRejectsDraftWork(t *testing.T) {
	router, repo, rawKey, key, _ := setupCertificateTestRouter(t)
	draft, err := repo.CreateOriginalWork(database.CreateOriginalWorkParams{
		APIKeyID:           key.ID,
		Title:              "Draft Work",
		WorkType:           "poem",
		Content:            "draft line",
		OriginalCommitment: true,
		LicenseAccepted:    true,
		Publish:            false,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/works/"+formatTestID(draft.ID)+"/certificate", nil)
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestCertificatePageRendersShell(t *testing.T) {
	router, _, _, _, work := setupCertificateTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/certificates/"+work.WorkCode, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Qanlo Poetry Work Certificate")
	assert.Contains(t, w.Body.String(), "/api/v1/public/works/")
}
