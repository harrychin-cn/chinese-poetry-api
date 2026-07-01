package handler

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

// KnowledgeHandler exposes AI/RAG-friendly poem recall endpoints.
type KnowledgeHandler struct {
	repo *database.Repository
}

// NewKnowledgeHandler creates a knowledge handler.
func NewKnowledgeHandler(repo *database.Repository) *KnowledgeHandler {
	return &KnowledgeHandler{repo: repo}
}

type knowledgeScenario struct {
	ID              string
	Name            string
	Category        string
	Description     string
	ExampleQuery    string
	Keywords        []string
	FallbackKeyword string
	Tags            []database.TagInput
}

type knowledgeBatchRequest struct {
	PageSize int                   `json:"page_size"`
	Queries  []knowledgeBatchQuery `json:"queries"`
}

type knowledgeBatchQuery struct {
	ID         string              `json:"id"`
	Query      string              `json:"q"`
	Intent     string              `json:"intent"`
	ScenarioID string              `json:"scenario_id"`
	Tags       []database.TagInput `json:"tags"`
}

type knowledgeRecallOptions struct {
	Intent       string
	ScenarioID   string
	ExplicitTags []database.TagInput
	BaseFilter   database.PoemQueryFilter
}

var knowledgeScenarios = []knowledgeScenario{
	{
		ID:              "farewell_graduation",
		Name:            "毕业送别",
		Category:        "education",
		Description:     "毕业、离别、送别、同窗分别等场景。",
		ExampleQuery:    "找适合毕业离别的诗",
		Keywords:        []string{"毕业", "离别", "送别", "分别", "同窗", "赠别", "farewell"},
		FallbackKeyword: "别",
		Tags: []database.TagInput{
			{Name: "送别", Category: "scenario"},
			{Name: "离别", Category: "theme"},
			{Name: "友情", Category: "theme"},
		},
	},
	{
		ID:              "mid_autumn_moon",
		Name:            "中秋月亮",
		Category:        "festival",
		Description:     "中秋、月亮、团圆、望月怀人。",
		ExampleQuery:    "找中秋月亮诗句",
		Keywords:        []string{"中秋", "月亮", "明月", "月色", "团圆", "望月", "moon"},
		FallbackKeyword: "月",
		Tags: []database.TagInput{
			{Name: "中秋", Category: "festival"},
			{Name: "月亮", Category: "theme"},
			{Name: "团圆", Category: "mood"},
		},
	},
	{
		ID:              "homesickness",
		Name:            "思乡怀人",
		Category:        "emotion",
		Description:     "故乡、乡愁、旅居、怀人。",
		ExampleQuery:    "找表达思乡的诗",
		Keywords:        []string{"思乡", "故乡", "乡愁", "怀乡", "归乡", "家乡", "homesick"},
		FallbackKeyword: "乡",
		Tags: []database.TagInput{
			{Name: "思乡", Category: "theme"},
			{Name: "故乡", Category: "theme"},
			{Name: "怀人", Category: "mood"},
		},
	},
	{
		ID:              "frontier_war",
		Name:            "边塞战争",
		Category:        "theme",
		Description:     "边塞、战争、军旅、报国。",
		ExampleQuery:    "找边塞战争诗",
		Keywords:        []string{"边塞", "战争", "军旅", "塞外", "将军", "征战", "war"},
		FallbackKeyword: "塞",
		Tags: []database.TagInput{
			{Name: "边塞", Category: "theme"},
			{Name: "战争", Category: "theme"},
			{Name: "军旅", Category: "scenario"},
		},
	},
	{
		ID:              "spring",
		Name:            "春天景物",
		Category:        "season",
		Description:     "春天、春风、春雨、花鸟等内容场景。",
		ExampleQuery:    "找描写春天的诗",
		Keywords:        []string{"春天", "春日", "春风", "春雨", "春色", "花", "spring"},
		FallbackKeyword: "春",
		Tags: []database.TagInput{
			{Name: "春天", Category: "season"},
			{Name: "花", Category: "image"},
		},
	},
	{
		ID:              "love_longing",
		Name:            "爱情相思",
		Category:        "emotion",
		Description:     "爱情、相思、思念、闺怨。",
		ExampleQuery:    "找爱情相思的诗词",
		Keywords:        []string{"爱情", "相思", "思念", "恋人", "情人", "闺怨", "love"},
		FallbackKeyword: "相思",
		Tags: []database.TagInput{
			{Name: "爱情", Category: "theme"},
			{Name: "相思", Category: "mood"},
		},
	},
	{
		ID:              "landscape_travel",
		Name:            "山水文旅",
		Category:        "travel",
		Description:     "山水、登高、江河、文旅内容引用。",
		ExampleQuery:    "找适合文旅山水宣传的诗",
		Keywords:        []string{"山水", "文旅", "旅行", "旅游", "登高", "江河", "landscape"},
		FallbackKeyword: "山",
		Tags: []database.TagInput{
			{Name: "山水", Category: "theme"},
			{Name: "文旅", Category: "scenario"},
		},
	},
	{
		ID:              "patriotic",
		Name:            "家国情怀",
		Category:        "theme",
		Description:     "爱国、报国、山河、忧国忧民。",
		ExampleQuery:    "找家国情怀的诗",
		Keywords:        []string{"爱国", "报国", "家国", "山河", "忧国", "patriotic"},
		FallbackKeyword: "国",
		Tags: []database.TagInput{
			{Name: "爱国", Category: "theme"},
			{Name: "家国", Category: "theme"},
		},
	},
	{
		ID:              "short_video_copy",
		Name:            "短视频文案",
		Category:        "content",
		Description:     "适合短视频、海报、社媒内容的诗句引用。",
		ExampleQuery:    "找适合短视频文案的诗句",
		Keywords:        []string{"短视频", "文案", "海报", "社媒", "小红书", "抖音", "copywriting"},
		FallbackKeyword: "风",
		Tags: []database.TagInput{
			{Name: "短视频文案", Category: "scenario"},
			{Name: "金句", Category: "scenario"},
		},
	},
}

// ListScenarios returns built-in scenario presets for AI knowledge recall.
func (h *KnowledgeHandler) ListScenarios(c *gin.Context) {
	data := make([]map[string]any, len(knowledgeScenarios))
	for i, scenario := range knowledgeScenarios {
		data[i] = formatKnowledgeScenario(scenario)
	}
	respondOK(c, data)
}

// Recall returns poems in an AI/RAG-friendly knowledge structure.
func (h *KnowledgeHandler) Recall(c *gin.Context) {
	lang := parseLang(c)
	repo := h.repo.WithLang(lang)
	pagination := ParsePagination(c)

	intent := strings.TrimSpace(firstNonEmpty(c.Query("q"), c.Query("intent"), c.Query("scenario")))
	if intent == "" {
		respondError(c, http.StatusBadRequest, "query parameter 'q' or 'intent' is required")
		return
	}

	filter, ok := buildKnowledgeQueryFilter(c, repo, pagination)
	if !ok {
		return
	}

	data, total, metadata, err := h.runKnowledgeRecall(repo, pagination, knowledgeRecallOptions{
		Intent:       intent,
		ScenarioID:   c.Query("scenario_id"),
		ExplicitTags: knowledgeTagsFromQuery(c),
		BaseFilter:   filter,
	})
	if err != nil {
		if errors.Is(err, database.ErrInvalidQueryParam) {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondError(c, http.StatusInternalServerError, "knowledge recall failed")
		return
	}

	response := NewPaginationResponse(data, pagination, total)
	response["knowledge"] = metadata
	c.JSON(http.StatusOK, response)
}

// BatchRecall runs multiple small knowledge recall queries in one request.
func (h *KnowledgeHandler) BatchRecall(c *gin.Context) {
	lang := parseLang(c)
	repo := h.repo.WithLang(lang)

	var req knowledgeBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid json body")
		return
	}
	if len(req.Queries) == 0 {
		respondError(c, http.StatusBadRequest, "queries is required")
		return
	}
	if len(req.Queries) > 20 {
		respondError(c, http.StatusBadRequest, "queries cannot exceed 20 items")
		return
	}

	pageSize := req.PageSize
	if pageSize < 1 {
		pageSize = 5
	}
	if pageSize > 20 {
		pageSize = 20
	}
	pagination := PaginationParams{Page: 1, PageSize: pageSize}

	results := make([]map[string]any, 0, len(req.Queries))
	for _, query := range req.Queries {
		intent := strings.TrimSpace(firstNonEmpty(query.Query, query.Intent))
		if intent == "" {
			respondError(c, http.StatusBadRequest, "each query requires q or intent")
			return
		}

		data, total, metadata, err := h.runKnowledgeRecall(repo, pagination, knowledgeRecallOptions{
			Intent:       intent,
			ScenarioID:   query.ScenarioID,
			ExplicitTags: query.Tags,
			BaseFilter: database.PoemQueryFilter{
				SearchIn: "all",
				Page:     pagination.Page,
				PageSize: pagination.PageSize,
				Sort:     "id_desc",
			},
		})
		if err != nil {
			if errors.Is(err, database.ErrInvalidQueryParam) {
				respondError(c, http.StatusBadRequest, err.Error())
				return
			}
			respondError(c, http.StatusInternalServerError, "knowledge batch recall failed")
			return
		}

		results = append(results, map[string]any{
			"id":         firstNonEmpty(query.ID, intent),
			"query":      intent,
			"data":       data,
			"pagination": NewPaginationResponse(data, pagination, total)["pagination"],
			"knowledge":  metadata,
		})
	}

	respondOK(c, gin.H{
		"count":   len(results),
		"results": results,
	})
}

func (h *KnowledgeHandler) runKnowledgeRecall(repo *database.Repository, pagination PaginationParams, opts knowledgeRecallOptions) ([]map[string]any, int64, gin.H, error) {
	intent := strings.TrimSpace(opts.Intent)
	matchedScenario, scenarioMatched := matchKnowledgeScenario(intent)
	if explicitScenario := strings.TrimSpace(opts.ScenarioID); explicitScenario != "" {
		if scenario, ok := getKnowledgeScenarioByID(explicitScenario); ok {
			matchedScenario = scenario
			scenarioMatched = true
		} else {
			return nil, 0, nil, fmtInvalidQuery("unsupported scenario_id")
		}
	}

	scenarioTagIDs, resolvedScenarioTags, err := repo.ResolveExistingTagIDsForQuery(scenarioTags(matchedScenario, scenarioMatched))
	if err != nil {
		return nil, 0, nil, err
	}
	explicitTagIDs, resolvedExplicitTags, err := repo.ResolveExistingTagIDsForQuery(opts.ExplicitTags)
	if err != nil {
		return nil, 0, nil, err
	}
	resolvedTags := append(resolvedScenarioTags, resolvedExplicitTags...)

	filter := opts.BaseFilter
	filter.Keyword = recallKeyword(intent, matchedScenario, scenarioMatched)
	if strings.TrimSpace(filter.SearchIn) == "" {
		filter.SearchIn = "all"
	}
	if filter.Page < 1 {
		filter.Page = pagination.Page
	}
	if filter.PageSize < 1 {
		filter.PageSize = pagination.PageSize
	}
	if strings.TrimSpace(filter.Sort) == "" {
		filter.Sort = "id_desc"
	}
	filter.TagIDs = mergeInt64s(filter.TagIDs, scenarioTagIDs, explicitTagIDs)

	poems, total, usedTags, err := queryKnowledgePoems(repo, filter, scenarioTagIDs, explicitTagIDs)
	if err != nil {
		return nil, 0, nil, err
	}

	poemIDs := make([]int64, len(poems))
	for i, poem := range poems {
		poemIDs[i] = poem.ID
	}
	tagsByPoemID, err := repo.ListTagsByPoemIDs(poemIDs)
	if err != nil {
		return nil, 0, nil, err
	}
	knowledgeByPoemID, err := repo.ListPoemKnowledgeByPoemIDs(poemIDs)
	if err != nil {
		return nil, 0, nil, err
	}

	data := make([]map[string]any, len(poems))
	for i, poem := range poems {
		poemTags := tagsByPoemID[poem.ID]
		data[i] = formatKnowledgePoem(&poem, poemTags, knowledgeByPoemID[poem.ID], matchedScenario, scenarioMatched, intent, usedTags)
	}

	metadata := gin.H{
		"intent":        intent,
		"scenario":      scenarioResponse(matchedScenario, scenarioMatched),
		"matched_tags":  formatTags(resolvedTags),
		"recall_mode":   recallMode(scenarioMatched, usedTags, filter.Keyword),
		"citation_hint": "建议引用 title、author.name、dynasty.name 和 content 中的原句；不要把推荐理由当作原文。",
	}
	return data, total, metadata, nil
}

func buildKnowledgeQueryFilter(c *gin.Context, repo *database.Repository, pagination PaginationParams) (database.PoemQueryFilter, bool) {
	filter := database.PoemQueryFilter{
		SearchIn: c.DefaultQuery("search_in", "all"),
		Page:     pagination.Page,
		PageSize: pagination.PageSize,
		Sort:     c.DefaultQuery("sort", "id_desc"),
	}

	authorID, ok := resolveAuthorID(c, repo)
	if !ok {
		return filter, false
	}
	filter.AuthorID = authorID

	dynastyID, ok := resolveDynastyID(c, repo)
	if !ok {
		return filter, false
	}
	filter.DynastyID = dynastyID

	typeIDs, ok := resolveTypeIDs(c, repo)
	if !ok {
		return filter, false
	}
	filter.TypeIDs = typeIDs

	lines, ok := parseOptionalPositiveInt(c, "lines")
	if !ok {
		return filter, false
	}
	filter.Lines = lines

	charsPerLine, ok := parseOptionalPositiveInt(c, "chars_per_line")
	if !ok {
		return filter, false
	}
	filter.CharsPerLine = charsPerLine

	return filter, true
}

func queryKnowledgePoems(repo *database.Repository, filter database.PoemQueryFilter, scenarioTagIDs, explicitTagIDs []int64) ([]database.Poem, int64, bool, error) {
	poems, total, err := repo.QueryPoems(filter)
	if err != nil {
		return nil, 0, len(filter.TagIDs) > 0, err
	}
	if total > 0 || len(scenarioTagIDs) == 0 || filter.Keyword == "" {
		return poems, total, len(filter.TagIDs) > 0, nil
	}

	// If scenario tags exist but current data is still sparse, fall back to keyword
	// recall so early customers still get usable AI knowledge results.
	fallback := filter
	fallback.TagIDs = mergeInt64s(explicitTagIDs)
	poems, total, err = repo.QueryPoems(fallback)
	return poems, total, len(fallback.TagIDs) > 0, err
}

func knowledgeTagsFromQuery(c *gin.Context) []database.TagInput {
	tagNames := c.QueryArray("tag")
	tagCategory := c.Query("tag_category")
	inputs := make([]database.TagInput, 0, len(tagNames))
	for _, name := range tagNames {
		inputs = append(inputs, database.TagInput{Name: name, Category: tagCategory})
	}
	return inputs
}

func recallKeyword(intent string, scenario knowledgeScenario, matched bool) string {
	if matched && scenario.FallbackKeyword != "" {
		return scenario.FallbackKeyword
	}
	return strings.TrimSpace(intent)
}

func recallMode(matchedScenario, usedTags bool, keyword string) string {
	parts := make([]string, 0, 3)
	if matchedScenario {
		parts = append(parts, "scenario_rules")
	}
	if usedTags {
		parts = append(parts, "tags")
	}
	if keyword != "" {
		parts = append(parts, "keyword")
	}
	if len(parts) == 0 {
		return "keyword"
	}
	return strings.Join(parts, "+")
}

func formatKnowledgePoem(poem *database.Poem, tags []database.Tag, enriched database.PoemKnowledge, scenario knowledgeScenario, scenarioMatched bool, intent string, usedTags bool) map[string]any {
	item := formatPoem(poem)
	item["tags"] = formatTags(tags)

	citationTitle := poem.Title
	if poem.Author != nil {
		citationTitle += " / " + poem.Author.Name
	}
	if poem.Dynasty != nil {
		citationTitle += " / " + poem.Dynasty.Name
	}

	reason := "按关键词与诗词正文、标题、作者匹配，适合作为 AI 知识库候选引用。"
	if scenarioMatched {
		reason = "命中“" + scenario.Name + "”场景，适合作为“" + intent + "”的候选引用。"
	}
	if usedTags && len(tags) > 0 {
		reason += " 已返回该诗的标签，便于 RAG 继续筛选。"
	}

	knowledge := gin.H{
		"reason":          reason,
		"scenario_id":     scenarioID(scenario, scenarioMatched),
		"citation_format": citationTitle,
		"citation_text":   poem.Content,
		"source":          "chinese-poetry-api",
	}
	if enriched.ID > 0 {
		knowledge["summary"] = enriched.Summary
		knowledge["translation"] = enriched.Translation
		knowledge["annotation"] = enriched.Annotation
		knowledge["recommendation"] = enriched.Recommendation
		knowledge["quality_status"] = enriched.QualityStatus
		knowledge["enrichment_source"] = enriched.Source
	}
	item["knowledge"] = knowledge
	return item
}

func formatKnowledgeScenario(scenario knowledgeScenario) map[string]any {
	return map[string]any{
		"id":            scenario.ID,
		"name":          scenario.Name,
		"category":      scenario.Category,
		"description":   scenario.Description,
		"example_query": scenario.ExampleQuery,
		"tags":          tagInputsForResponse(scenario.Tags),
	}
}

func fmtInvalidQuery(message string) error {
	return fmt.Errorf("%w: %s", database.ErrInvalidQueryParam, message)
}

func scenarioResponse(scenario knowledgeScenario, matched bool) any {
	if !matched {
		return nil
	}
	return formatKnowledgeScenario(scenario)
}

func scenarioID(scenario knowledgeScenario, matched bool) string {
	if !matched {
		return ""
	}
	return scenario.ID
}

func scenarioTags(scenario knowledgeScenario, matched bool) []database.TagInput {
	if !matched {
		return nil
	}
	out := make([]database.TagInput, len(scenario.Tags))
	copy(out, scenario.Tags)
	return out
}

func tagInputsForResponse(tags []database.TagInput) []map[string]string {
	data := make([]map[string]string, len(tags))
	for i, tag := range tags {
		data[i] = map[string]string{
			"name":     tag.Name,
			"category": tag.Category,
		}
	}
	return data
}

func formatTags(tags []database.Tag) []map[string]any {
	data := make([]map[string]any, len(tags))
	for i, tag := range tags {
		data[i] = formatTag(tag)
	}
	return data
}

func matchKnowledgeScenario(intent string) (knowledgeScenario, bool) {
	normalized := strings.ToLower(strings.TrimSpace(intent))
	for _, scenario := range knowledgeScenarios {
		if strings.Contains(normalized, strings.ToLower(scenario.ID)) || strings.Contains(normalized, strings.ToLower(scenario.Name)) {
			return scenario, true
		}
		for _, keyword := range scenario.Keywords {
			keyword = strings.ToLower(strings.TrimSpace(keyword))
			if keyword != "" && strings.Contains(normalized, keyword) {
				return scenario, true
			}
		}
	}
	return knowledgeScenario{}, false
}

func getKnowledgeScenarioByID(id string) (knowledgeScenario, bool) {
	id = strings.TrimSpace(id)
	for _, scenario := range knowledgeScenarios {
		if scenario.ID == id {
			return scenario, true
		}
	}
	return knowledgeScenario{}, false
}

func mergeInt64s(values ...[]int64) []int64 {
	seen := make(map[int64]bool)
	out := make([]int64, 0)
	for _, group := range values {
		for _, value := range group {
			if value < 1 || seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
