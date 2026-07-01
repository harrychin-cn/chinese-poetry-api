package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

// TagHandler manages value-added poem tags.
type TagHandler struct {
	repo *database.Repository
}

// NewTagHandler creates a tag handler.
func NewTagHandler(repo *database.Repository) *TagHandler {
	return &TagHandler{repo: repo}
}

type upsertTagRequest struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

type assignPoemTagsRequest struct {
	Tags []upsertTagRequest `json:"tags"`
}

// ListTags lists available tags.
func (h *TagHandler) ListTags(c *gin.Context) {
	tags, err := h.repo.ListTags(c.Query("category"))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to list tags")
		return
	}

	data := make([]map[string]any, len(tags))
	for i, tag := range tags {
		data[i] = formatTag(tag)
	}
	respondOK(c, data)
}

// UpsertTag creates or updates a tag.
func (h *TagHandler) UpsertTag(c *gin.Context) {
	var req upsertTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid json body")
		return
	}

	tag, err := h.repo.UpsertTag(database.TagInput{
		Name:        req.Name,
		Category:    req.Category,
		Description: req.Description,
		Source:      req.Source,
	})
	if err != nil {
		if errors.Is(err, database.ErrInvalidQueryParam) {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondError(c, http.StatusInternalServerError, "failed to save tag")
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": formatTag(*tag)})
}

// AssignPoemTags assigns tags to a poem.
func (h *TagHandler) AssignPoemTags(c *gin.Context) {
	poemID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || poemID < 1 {
		respondError(c, http.StatusBadRequest, "invalid poem id")
		return
	}

	var req assignPoemTagsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid json body")
		return
	}

	inputs := make([]database.TagInput, len(req.Tags))
	for i, tag := range req.Tags {
		inputs[i] = database.TagInput{
			Name:        tag.Name,
			Category:    tag.Category,
			Description: tag.Description,
			Source:      tag.Source,
		}
	}

	tags, err := h.repo.AssignTagsToPoem(poemID, inputs)
	if err != nil {
		if errors.Is(err, database.ErrInvalidQueryParam) {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondError(c, http.StatusNotFound, "poem not found")
			return
		}
		respondError(c, http.StatusInternalServerError, "failed to assign tags")
		return
	}

	data := make([]map[string]any, len(tags))
	for i, tag := range tags {
		data[i] = formatTag(tag)
	}
	respondOK(c, data)
}

func formatTag(tag database.Tag) map[string]any {
	result := map[string]any{
		"id":       tag.ID,
		"name":     tag.Name,
		"category": tag.Category,
		"source":   tag.Source,
	}
	if tag.Description != "" {
		result["description"] = tag.Description
	}
	return result
}
