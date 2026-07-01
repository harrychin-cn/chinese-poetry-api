package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

// PoemHandler handles poem-related requests
type PoemHandler struct {
	repo *database.Repository
}

// NewPoemHandler creates a new poem handler
func NewPoemHandler(repo *database.Repository) *PoemHandler {
	return &PoemHandler{
		repo: repo,
	}
}

// ListPoems retrieves a paginated list of poems
// Supports ?lang=zh-Hans (default) or ?lang=zh-Hant
func (h *PoemHandler) ListPoems(c *gin.Context) {
	lang := parseLang(c)
	repo := h.repo.WithLang(lang)
	pagination := ParsePagination(c)

	poems, err := repo.ListPoems(pagination.PageSize, pagination.Offset())
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to retrieve poems")
		return
	}

	total, err := repo.CountPoems()
	if err != nil {
		total = 0
	}

	data := make([]map[string]any, len(poems))
	for i, poem := range poems {
		data[i] = formatPoem(&poem)
	}

	c.JSON(http.StatusOK, NewPaginationResponse(data, pagination, int64(total)))
}

// SearchPoems searches for poems by query string
func (h *PoemHandler) SearchPoems(c *gin.Context) {
	lang := parseLang(c)
	repo := h.repo.WithLang(lang)

	query := c.Query("q")
	if query == "" {
		respondError(c, http.StatusBadRequest, "query parameter 'q' is required")
		return
	}

	searchType := c.DefaultQuery("type", "all")
	pagination := ParsePagination(c)

	// Use repository's search method instead of search engine
	poems, total, err := repo.SearchPoems(query, searchType, pagination.Page, pagination.PageSize)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "search failed")
		return
	}

	data := make([]map[string]any, len(poems))
	for i, poem := range poems {
		data[i] = formatPoem(&poem)
	}

	c.JSON(http.StatusOK, NewPaginationResponse(data, pagination, total))
}

// QueryPoems runs a safe compound query for poems.
// It supports keyword, author, dynasty, type, lines, chars_per_line, pagination, and sorting.
func (h *PoemHandler) QueryPoems(c *gin.Context) {
	lang := parseLang(c)
	repo := h.repo.WithLang(lang)
	pagination := ParsePagination(c)

	filter, ok := buildPoemQueryFilter(c, repo, pagination, "id_desc")
	if !ok {
		return
	}

	poems, total, err := repo.QueryPoems(filter)
	if err != nil {
		if errors.Is(err, database.ErrInvalidQueryParam) {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondError(c, http.StatusInternalServerError, "query failed")
		return
	}

	data := make([]map[string]any, len(poems))
	for i, poem := range poems {
		data[i] = formatPoem(&poem)
	}

	c.JSON(http.StatusOK, NewPaginationResponse(data, pagination, total))
}

// SearchPoemsFTS runs full-text search with the same safe filters as QueryPoems.
func (h *PoemHandler) SearchPoemsFTS(c *gin.Context) {
	lang := parseLang(c)
	repo := h.repo.WithLang(lang)
	pagination := ParsePagination(c)

	filter, ok := buildPoemQueryFilter(c, repo, pagination, "relevance")
	if !ok {
		return
	}
	if filter.Keyword == "" {
		respondError(c, http.StatusBadRequest, "query parameter 'q' is required")
		return
	}

	if err := repo.EnsurePoemFTSIndex(); err != nil {
		if errors.Is(err, database.ErrSearchUnavailable) {
			respondError(c, http.StatusServiceUnavailable, err.Error())
			return
		}
		respondError(c, http.StatusInternalServerError, "failed to prepare search index")
		return
	}

	results, total, err := repo.SearchPoemsFTS(filter)
	if err != nil {
		if errors.Is(err, database.ErrInvalidQueryParam) {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, database.ErrSearchUnavailable) {
			respondError(c, http.StatusServiceUnavailable, err.Error())
			return
		}
		respondError(c, http.StatusInternalServerError, "full-text search failed")
		return
	}

	data := make([]map[string]any, len(results))
	for i, result := range results {
		item := formatPoem(&result.Poem)
		item["search"] = gin.H{
			"rank":       result.Rank,
			"hit_fields": result.HitFields,
			"snippets":   result.Snippets,
		}
		data[i] = item
	}

	c.JSON(http.StatusOK, NewPaginationResponse(data, pagination, total))
}

// RebuildSearchIndex rebuilds SQLite FTS5 indexes for one or both languages.
func (h *PoemHandler) RebuildSearchIndex(c *gin.Context) {
	if !database.SQLiteFTS5Enabled() {
		respondError(c, http.StatusServiceUnavailable, "full-text search is unavailable; rebuild with -tags sqlite_fts5")
		return
	}

	langParam := c.Query("lang")
	langs := []database.Lang{database.LangHans, database.LangHant}
	if langParam != "" && langParam != "all" {
		langs = []database.Lang{database.ParseLang(langParam)}
	}

	rebuilt := make([]string, 0, len(langs))
	for _, lang := range langs {
		if err := h.repo.WithLang(lang).RebuildPoemFTSIndex(); err != nil {
			respondError(c, http.StatusInternalServerError, "failed to rebuild search index")
			return
		}
		rebuilt = append(rebuilt, string(lang))
	}

	respondOK(c, gin.H{
		"fts5_enabled":      true,
		"rebuilt_languages": rebuilt,
	})
}

// RandomPoem returns a random poem with optional filters
// Supports ?lang=zh-Hans (default) or ?lang=zh-Hant
// Supports filters: ?author=李白&type=五言绝句&type=七言绝句&dynasty=唐
// Or by ID: ?author_id=123&type_id=456&type_id=789&dynasty_id=789
func (h *PoemHandler) RandomPoem(c *gin.Context) {
	lang := parseLang(c)
	repo := h.repo.WithLang(lang)

	// Parse filter parameters
	var authorID, dynastyID *int64
	var typeIDs []int64

	// Parse author filter (by ID or name)
	if authorIDStr := c.Query("author_id"); authorIDStr != "" {
		if id, err := strconv.ParseInt(authorIDStr, 10, 64); err == nil {
			authorID = &id
		}
	} else if authorName := c.Query("author"); authorName != "" {
		// Look up author by name
		author, err := repo.GetAuthorByName(authorName)
		if err != nil {
			respondError(c, http.StatusNotFound, "author not found")
			return
		}
		authorID = &author.ID
	}

	// Parse type filter (by ID or name) - supports multiple values
	typeIDStrs := c.QueryArray("type_id")
	typeNames := c.QueryArray("type")

	if len(typeIDStrs) > 0 {
		// Parse type IDs
		for _, idStr := range typeIDStrs {
			if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
				typeIDs = append(typeIDs, id)
			}
		}
	} else if len(typeNames) > 0 {
		// Batch lookup types by name in a single query
		ids, err := repo.GetPoetryTypeIDs(typeNames)
		if err != nil {
			respondError(c, http.StatusNotFound, "poetry type not found")
			return
		}
		typeIDs = ids
	}

	// Parse dynasty filter (by ID or name)
	if dynastyIDStr := c.Query("dynasty_id"); dynastyIDStr != "" {
		if id, err := strconv.ParseInt(dynastyIDStr, 10, 64); err == nil {
			dynastyID = &id
		}
	} else if dynastyName := c.Query("dynasty"); dynastyName != "" {
		// Look up dynasty by name
		dynasty, err := repo.GetDynastyByName(dynastyName)
		if err != nil {
			respondError(c, http.StatusNotFound, "dynasty not found")
			return
		}
		dynastyID = &dynasty.ID
	}

	// Get a random poem with filters
	poem, err := repo.GetRandomPoem(dynastyID, authorID, typeIDs)
	if err != nil {
		respondError(c, http.StatusNotFound, "no poems found matching the criteria")
		return
	}

	c.JSON(http.StatusOK, formatPoem(poem))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func buildPoemQueryFilter(c *gin.Context, repo *database.Repository, pagination PaginationParams, defaultSort string) (database.PoemQueryFilter, bool) {
	filter := database.PoemQueryFilter{
		Keyword:  firstNonEmpty(c.Query("q"), c.Query("keyword")),
		SearchIn: c.DefaultQuery("search_in", "all"),
		Page:     pagination.Page,
		PageSize: pagination.PageSize,
		Sort:     c.DefaultQuery("sort", defaultSort),
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

	tagIDs, ok := resolveTagIDs(c, repo)
	if !ok {
		return filter, false
	}
	filter.TagIDs = tagIDs

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

func parseOptionalPositiveInt(c *gin.Context, name string) (*int, bool) {
	raw := c.Query(name)
	if raw == "" {
		return nil, true
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		respondError(c, http.StatusBadRequest, name+" must be a positive integer")
		return nil, false
	}

	return &value, true
}

func resolveAuthorID(c *gin.Context, repo *database.Repository) (*int64, bool) {
	if idStr := c.Query("author_id"); idStr != "" {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id < 1 {
			respondError(c, http.StatusBadRequest, "author_id must be a positive integer")
			return nil, false
		}
		return &id, true
	}

	if name := c.Query("author"); name != "" {
		author, err := repo.GetAuthorByName(name)
		if err != nil {
			respondError(c, http.StatusNotFound, "author not found")
			return nil, false
		}
		id := author.ID
		return &id, true
	}

	return nil, true
}

func resolveDynastyID(c *gin.Context, repo *database.Repository) (*int64, bool) {
	if idStr := c.Query("dynasty_id"); idStr != "" {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id < 1 {
			respondError(c, http.StatusBadRequest, "dynasty_id must be a positive integer")
			return nil, false
		}
		return &id, true
	}

	if name := c.Query("dynasty"); name != "" {
		dynasty, err := repo.GetDynastyByName(name)
		if err != nil {
			respondError(c, http.StatusNotFound, "dynasty not found")
			return nil, false
		}
		id := dynasty.ID
		return &id, true
	}

	return nil, true
}

func resolveTypeIDs(c *gin.Context, repo *database.Repository) ([]int64, bool) {
	var typeIDs []int64

	for _, idStr := range c.QueryArray("type_id") {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id < 1 {
			respondError(c, http.StatusBadRequest, "type_id must be a positive integer")
			return nil, false
		}
		typeIDs = append(typeIDs, id)
	}

	typeNames := c.QueryArray("type")
	if len(typeNames) == 0 {
		return typeIDs, true
	}

	ids, err := repo.GetPoetryTypeIDs(typeNames)
	if err != nil {
		respondError(c, http.StatusNotFound, "poetry type not found")
		return nil, false
	}

	typeIDs = append(typeIDs, ids...)
	return typeIDs, true
}

func resolveTagIDs(c *gin.Context, repo *database.Repository) ([]int64, bool) {
	tagNames := c.QueryArray("tag")
	tagCategory := c.Query("tag_category")
	if len(tagNames) == 0 {
		return nil, true
	}

	inputs := make([]database.TagInput, len(tagNames))
	for i, name := range tagNames {
		inputs[i] = database.TagInput{Name: name, Category: tagCategory}
	}

	ids, err := repo.ResolveTagIDsForQuery(inputs)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return nil, false
	}
	return ids, true
}
