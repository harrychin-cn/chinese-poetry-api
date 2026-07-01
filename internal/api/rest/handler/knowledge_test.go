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
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/palemoky/chinese-poetry-api/internal/api/middleware"
	"github.com/palemoky/chinese-poetry-api/internal/database"
)

func setupKnowledgeTestRouter(t *testing.T, authEnabled bool) (*gin.Engine, *database.Repository, string) {
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
	duFuID, err := repo.GetOrCreateAuthor("杜甫", tangID)
	require.NoError(t, err)

	require.NoError(t, repo.InsertPoem(&database.Poem{
		ID:        301,
		Title:     "静夜思",
		Content:   datatypes.JSON([]byte(`["床前明月光","疑是地上霜","举头望明月","低头思故乡"]`)),
		AuthorID:  &liBaiID,
		DynastyID: &tangID,
	}))
	require.NoError(t, repo.InsertPoem(&database.Poem{
		ID:        302,
		Title:     "送友人",
		Content:   datatypes.JSON([]byte(`["青山横北郭","白水绕东城","此地一为别","孤蓬万里征"]`)),
		AuthorID:  &liBaiID,
		DynastyID: &tangID,
	}))
	require.NoError(t, repo.InsertPoem(&database.Poem{
		ID:        303,
		Title:     "春望",
		Content:   datatypes.JSON([]byte(`["国破山河在","城春草木深"]`)),
		AuthorID:  &duFuID,
		DynastyID: &tangID,
	}))

	_, err = repo.AssignTagsToPoem(301, []database.TagInput{
		{Name: "月亮", Category: "theme", Source: "manual"},
		{Name: "思乡", Category: "theme", Source: "manual"},
	})
	require.NoError(t, err)
	_, err = repo.AssignTagsToPoem(302, []database.TagInput{
		{Name: "送别", Category: "scenario", Source: "manual"},
	})
	require.NoError(t, err)

	h := NewKnowledgeHandler(repo)
	router := gin.New()
	router.GET("/knowledge/scenarios", h.ListScenarios)
	if authEnabled {
		router.GET("/knowledge/recall", middleware.APIKeyAuthWithRecharge(repo, "https://qanlo.com/recharge"), h.Recall)
		router.POST("/knowledge/batch", middleware.APIKeyAuthWithRecharge(repo, "https://qanlo.com/recharge"), h.BatchRecall)
	} else {
		router.GET("/knowledge/recall", h.Recall)
		router.POST("/knowledge/batch", h.BatchRecall)
	}

	var rawKey string
	if authEnabled {
		_, rawKey, err = repo.CreateAPIKey(database.CreateAPIKeyParams{Name: "knowledge customer", DailyLimit: 10})
		require.NoError(t, err)
	}
	return router, repo, rawKey
}

func TestKnowledgeScenarios(t *testing.T) {
	router, _, _ := setupKnowledgeTestRouter(t, false)

	req := httptest.NewRequest(http.MethodGet, "/knowledge/scenarios", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	data := body["data"].([]any)
	assert.NotEmpty(t, data)
	first := data[0].(map[string]any)
	assert.NotEmpty(t, first["id"])
	assert.NotEmpty(t, first["example_query"])
}

func TestKnowledgeRecallByScenarioAndTags(t *testing.T) {
	router, _, rawKey := setupKnowledgeTestRouter(t, true)

	req := httptest.NewRequest(http.MethodGet, "/knowledge/recall?q=找中秋月亮诗句&page_size=5", nil)
	req.Header.Set("X-API-Key", rawKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	knowledge := body["knowledge"].(map[string]any)
	assert.Equal(t, "找中秋月亮诗句", knowledge["intent"])
	assert.Contains(t, knowledge["recall_mode"], "scenario_rules")
	assert.Contains(t, knowledge["recall_mode"], "tags")

	data := body["data"].([]any)
	require.NotEmpty(t, data)
	item := data[0].(map[string]any)
	assert.Equal(t, "静夜思", item["title"])
	assert.NotNil(t, item["knowledge"])
	assert.NotEmpty(t, item["tags"])
}

func TestKnowledgeRecallFallsBackToKeywordWhenScenarioTagsDoNotMatch(t *testing.T) {
	router, _, _ := setupKnowledgeTestRouter(t, false)

	req := httptest.NewRequest(http.MethodGet, "/knowledge/recall?q=毕业离别&page_size=5", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	data := body["data"].([]any)
	require.NotEmpty(t, data)

	item := data[0].(map[string]any)
	assert.Equal(t, "送友人", item["title"])
	knowledge := body["knowledge"].(map[string]any)
	assert.Contains(t, knowledge["recall_mode"], "scenario_rules")
}

func TestKnowledgeRecallRequiresAPIKeyWhenEnabled(t *testing.T) {
	router, _, _ := setupKnowledgeTestRouter(t, true)

	req := httptest.NewRequest(http.MethodGet, "/knowledge/recall?q=月亮", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestKnowledgeBatchRecall(t *testing.T) {
	router, _, _ := setupKnowledgeTestRouter(t, false)

	body := `{"page_size":2,"queries":[{"id":"moon","q":"中秋月亮"},{"id":"farewell","q":"毕业离别"}]}`
	req := httptest.NewRequest(http.MethodPost, "/knowledge/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]any)
	assert.Equal(t, float64(2), data["count"])
	results := data["results"].([]any)
	require.Len(t, results, 2)
	first := results[0].(map[string]any)
	assert.Equal(t, "moon", first["id"])
	assert.NotEmpty(t, first["data"])
	assert.NotEmpty(t, first["knowledge"])
}
