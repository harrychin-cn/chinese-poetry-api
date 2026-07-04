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

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

func setupPlagiarismAdminTest(t *testing.T) (*gin.Engine, *database.Repository, *database.APIKey) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)
	key, _, err := repo.CreateAPIKey(database.CreateAPIKeyParams{Name: "review customer", DailyLimit: 10})
	require.NoError(t, err)

	h := NewPlagiarismAdminHandler(repo)
	router := gin.New()
	router.POST("/admin/plagiarism/corpus-sources", h.CreateCorpusSource)
	router.GET("/admin/plagiarism/corpus-sources", h.ListCorpusSources)
	router.GET("/admin/plagiarism/review-queue", h.ListReviewQueue)
	router.POST("/admin/plagiarism/review-queue/:id/approve", h.ApproveReviewQueueItem)
	router.POST("/admin/plagiarism/review-queue/:id/reject", h.RejectReviewQueueItem)
	return router, repo, key
}

func TestPlagiarismAdminCorpusAndReviewFlow(t *testing.T) {
	router, repo, key := setupPlagiarismAdminTest(t)

	createSource := httptest.NewRequest(http.MethodPost, "/admin/plagiarism/corpus-sources", strings.NewReader(`{
		"source_type":"dispute_case",
		"title":"Disputed source",
		"source_url":"https://example.test/dispute/2",
		"content":"alpha beta gamma delta epsilon zeta eta theta",
		"created_by":"tester"
	}`))
	createSource.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createSource)
	require.Equal(t, http.StatusCreated, createW.Code, createW.Body.String())
	assert.Contains(t, createW.Body.String(), "dispute_case")

	listSources := httptest.NewRequest(http.MethodGet, "/admin/plagiarism/corpus-sources?source_type=dispute_case", nil)
	listSourcesW := httptest.NewRecorder()
	router.ServeHTTP(listSourcesW, listSources)
	require.Equal(t, http.StatusOK, listSourcesW.Code, listSourcesW.Body.String())
	assert.Contains(t, listSourcesW.Body.String(), "Disputed source")

	work, err := repo.CreateOriginalWork(database.CreateOriginalWorkParams{
		APIKeyID:           key.ID,
		Title:              "Reworked source",
		WorkType:           "poem",
		Content:            "theta eta zeta epsilon delta gamma beta alpha",
		OriginalCommitment: true,
		LicenseAccepted:    true,
		Publish:            true,
	})
	require.NoError(t, err)
	assert.Equal(t, database.WorkStatusReviewRequired, work.Status)

	listQueue := httptest.NewRequest(http.MethodGet, "/admin/plagiarism/review-queue?status=pending", nil)
	listQueueW := httptest.NewRecorder()
	router.ServeHTTP(listQueueW, listQueue)
	require.Equal(t, http.StatusOK, listQueueW.Code, listQueueW.Body.String())

	var queueResp map[string]any
	require.NoError(t, json.Unmarshal(listQueueW.Body.Bytes(), &queueResp))
	items := queueResp["data"].(map[string]any)["items"].([]any)
	require.Len(t, items, 1)
	item := items[0].(map[string]any)
	queueID := int64(item["id"].(float64))
	assert.Equal(t, "pending", item["status"])

	approve := httptest.NewRequest(http.MethodPost, "/admin/plagiarism/review-queue/"+formatTestID(queueID)+"/approve", strings.NewReader(`{
		"reviewer":"operator",
		"notes":"authorized quotation"
	}`))
	approve.Header.Set("Content-Type", "application/json")
	approveW := httptest.NewRecorder()
	router.ServeHTTP(approveW, approve)
	require.Equal(t, http.StatusOK, approveW.Code, approveW.Body.String())
	assert.Contains(t, approveW.Body.String(), "manual_approved")
	assert.Contains(t, approveW.Body.String(), "published")
}
