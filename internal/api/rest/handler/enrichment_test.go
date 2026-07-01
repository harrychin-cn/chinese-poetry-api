package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/palemoky/chinese-poetry-api/internal/api/middleware"
	"github.com/palemoky/chinese-poetry-api/internal/database"
)

func setupEnrichmentTestRouter(t *testing.T) *gin.Engine {
	gin.SetMode(gin.TestMode)

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db := database.NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)

	tangID, err := repo.GetOrCreateDynasty("唐")
	require.NoError(t, err)
	liBaiID, err := repo.GetOrCreateAuthor("李白", tangID)
	require.NoError(t, err)
	require.NoError(t, repo.InsertPoem(&database.Poem{
		ID:        701,
		Title:     "静夜思",
		Content:   datatypes.JSON([]byte(`["床前明月光","低头思故乡"]`)),
		AuthorID:  &liBaiID,
		DynastyID: &tangID,
	}))

	h := NewEnrichmentHandler(repo)
	router := gin.New()
	admin := router.Group("/admin", middleware.AdminAuth("test-admin-token"))
	admin.POST("/enrichment/jobs", h.CreateJob)
	admin.GET("/enrichment/jobs", h.ListJobs)
	admin.GET("/enrichment/runs/:run_id/summary", h.RunSummary)
	admin.POST("/enrichment/review-items", h.CreateReviewItem)
	admin.GET("/enrichment/review-items", h.ListReviewItems)
	admin.PATCH("/enrichment/review-items/:id", h.CorrectReviewItem)
	admin.POST("/enrichment/review-items/:id/accept", h.AcceptReviewItem)
	admin.POST("/enrichment/review-items/:id/reject", h.RejectReviewItem)
	return router
}

func TestEnrichmentAdminReviewFlow(t *testing.T) {
	router := setupEnrichmentTestRouter(t)

	jobReq := httptest.NewRequest(http.MethodPost, "/admin/enrichment/jobs", strings.NewReader(`{"scope":"sample","total_count":1,"config":{"run_id":"handler-test"}}`))
	jobReq.Header.Set("Content-Type", "application/json")
	jobReq.Header.Set("X-Admin-Token", "test-admin-token")
	jobW := httptest.NewRecorder()
	router.ServeHTTP(jobW, jobReq)
	require.Equal(t, http.StatusCreated, jobW.Code)

	var jobResp map[string]any
	require.NoError(t, json.Unmarshal(jobW.Body.Bytes(), &jobResp))
	jobID := int64(jobResp["data"].(map[string]any)["id"].(float64))

	itemBody := `{
		"job_id": %d,
		"poem_id": 701,
		"proposed_tags": [{"name":"思乡","category":"theme","source":"ai"}],
		"proposed_knowledge": {"summary":"诗人借月色表达思乡之情。","recommendation":"适合中秋与思乡场景。","source":"ai"}
	}`
	itemReq := httptest.NewRequest(http.MethodPost, "/admin/enrichment/review-items", strings.NewReader(fmtJSON(itemBody, jobID)))
	itemReq.Header.Set("Content-Type", "application/json")
	itemReq.Header.Set("X-Admin-Token", "test-admin-token")
	itemW := httptest.NewRecorder()
	router.ServeHTTP(itemW, itemReq)
	require.Equal(t, http.StatusCreated, itemW.Code)

	var itemResp map[string]any
	require.NoError(t, json.Unmarshal(itemW.Body.Bytes(), &itemResp))
	itemID := int64(itemResp["data"].(map[string]any)["id"].(float64))

	correctReq := httptest.NewRequest(http.MethodPatch, "/admin/enrichment/review-items/"+formatTestID(itemID), strings.NewReader(`{
		"proposed_tags": [{"name":"乡愁","category":"mood","source":"manual"}],
		"proposed_knowledge": {"summary":"人工修正：明月触发乡愁。","source":"manual"},
		"reviewer":"tester",
		"notes":"修正后通过"
	}`))
	correctReq.Header.Set("Content-Type", "application/json")
	correctReq.Header.Set("X-Admin-Token", "test-admin-token")
	correctW := httptest.NewRecorder()
	router.ServeHTTP(correctW, correctReq)
	assert.Equal(t, http.StatusOK, correctW.Code)

	acceptReq := httptest.NewRequest(http.MethodPost, "/admin/enrichment/review-items/"+formatTestID(itemID)+"/accept", strings.NewReader(`{"reviewer":"tester","notes":"通过"}`))
	acceptReq.Header.Set("Content-Type", "application/json")
	acceptReq.Header.Set("X-Admin-Token", "test-admin-token")
	acceptW := httptest.NewRecorder()
	router.ServeHTTP(acceptW, acceptReq)
	assert.Equal(t, http.StatusOK, acceptW.Code)

	listReq := httptest.NewRequest(http.MethodGet, "/admin/enrichment/review-items?status=accepted", nil)
	listReq.Header.Set("X-Admin-Token", "test-admin-token")
	listW := httptest.NewRecorder()
	router.ServeHTTP(listW, listReq)
	assert.Equal(t, http.StatusOK, listW.Code)
	assert.Contains(t, listW.Body.String(), "accepted")

	summaryReq := httptest.NewRequest(http.MethodGet, "/admin/enrichment/runs/handler-test/summary", nil)
	summaryReq.Header.Set("X-Admin-Token", "test-admin-token")
	summaryW := httptest.NewRecorder()
	router.ServeHTTP(summaryW, summaryReq)
	require.Equal(t, http.StatusOK, summaryW.Code)
	assert.Contains(t, summaryW.Body.String(), `"run_id":"handler-test"`)
	assert.Contains(t, summaryW.Body.String(), `"accepted_count":1`)
}

func fmtJSON(format string, args ...any) string {
	return strings.TrimSpace(fmt.Sprintf(format, args...))
}

func formatTestID(id int64) string {
	return strconv.FormatInt(id, 10)
}
