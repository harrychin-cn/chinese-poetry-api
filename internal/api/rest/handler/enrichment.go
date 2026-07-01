package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

// EnrichmentHandler manages AI-generated candidates and manual review.
type EnrichmentHandler struct {
	repo *database.Repository
}

// NewEnrichmentHandler creates a handler for data-enrichment operations.
func NewEnrichmentHandler(repo *database.Repository) *EnrichmentHandler {
	return &EnrichmentHandler{repo: repo}
}

type createEnrichmentJobRequest struct {
	Scope      string         `json:"scope"`
	TotalCount int            `json:"total_count"`
	Config     map[string]any `json:"config"`
}

type createReviewItemRequest struct {
	JobID             *int64                          `json:"job_id"`
	PoemID            int64                           `json:"poem_id"`
	ProposedTags      []upsertTagRequest              `json:"proposed_tags"`
	ProposedKnowledge database.ProposedKnowledgeInput `json:"proposed_knowledge"`
}

type reviewDecisionRequest struct {
	Reviewer string `json:"reviewer"`
	Notes    string `json:"notes"`
}

// CreateJob creates a batch enrichment job record.
func (h *EnrichmentHandler) CreateJob(c *gin.Context) {
	var req createEnrichmentJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid json body")
		return
	}

	job, err := h.repo.CreateEnrichmentJob(database.CreateEnrichmentJobParams{
		Scope:      req.Scope,
		TotalCount: req.TotalCount,
		Config:     req.Config,
	})
	if err != nil {
		if errors.Is(err, database.ErrInvalidQueryParam) {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondError(c, http.StatusInternalServerError, "failed to create enrichment job")
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": formatEnrichmentJob(*job)})
}

// ListJobs lists recent enrichment jobs.
func (h *EnrichmentHandler) ListJobs(c *gin.Context) {
	limit, ok := parseOptionalLimit(c.Query("limit"), 20, 100)
	if !ok {
		respondError(c, http.StatusBadRequest, "invalid limit")
		return
	}

	jobs, err := h.repo.ListEnrichmentJobs(limit)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to list enrichment jobs")
		return
	}

	data := make([]map[string]any, len(jobs))
	for i, job := range jobs {
		data[i] = formatEnrichmentJob(job)
	}
	respondOK(c, data)
}

// RunSummary reports review pass rate and top rejection reasons for a run.
func (h *EnrichmentHandler) RunSummary(c *gin.Context) {
	runID := strings.TrimSpace(c.Param("run_id"))
	if runID == "" {
		runID = strings.TrimSpace(c.Query("run_id"))
	}
	if runID == "" {
		respondError(c, http.StatusBadRequest, "run_id is required")
		return
	}

	summary, err := h.repo.GetEnrichmentRunSummary(runID)
	if err != nil {
		if errors.Is(err, database.ErrInvalidQueryParam) {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondError(c, http.StatusNotFound, "enrichment run not found")
			return
		}
		respondError(c, http.StatusInternalServerError, "failed to summarize enrichment run")
		return
	}

	respondOK(c, summary)
}

// CreateReviewItem imports one AI-generated candidate into the pending review queue.
func (h *EnrichmentHandler) CreateReviewItem(c *gin.Context) {
	var req createReviewItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid json body")
		return
	}

	item, err := h.repo.CreateEnrichmentReviewItem(database.CreateReviewItemParams{
		JobID:             req.JobID,
		PoemID:            req.PoemID,
		ProposedTags:      tagRequestsToInputs(req.ProposedTags),
		ProposedKnowledge: req.ProposedKnowledge,
	})
	if err != nil {
		if errors.Is(err, database.ErrInvalidQueryParam) {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondError(c, http.StatusNotFound, "poem or enrichment job not found")
			return
		}
		respondError(c, http.StatusInternalServerError, "failed to create review item")
		return
	}

	data, err := formatReviewItem(*item)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to format review item")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": data})
}

// ListReviewItems lists review candidates, defaulting to pending.
func (h *EnrichmentHandler) ListReviewItems(c *gin.Context) {
	status := strings.TrimSpace(c.DefaultQuery("status", database.EnrichmentStatusPending))
	limit, ok := parseOptionalLimit(c.Query("limit"), 20, 100)
	if !ok {
		respondError(c, http.StatusBadRequest, "invalid limit")
		return
	}

	items, err := h.repo.ListEnrichmentReviewItems(status, limit)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to list review items")
		return
	}

	data := make([]map[string]any, len(items))
	for i, item := range items {
		formatted, err := formatReviewItem(item)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "failed to format review item")
			return
		}
		data[i] = formatted
	}
	respondOK(c, data)
}

// AcceptReviewItem applies accepted tags and knowledge to the public data layer.
func (h *EnrichmentHandler) AcceptReviewItem(c *gin.Context) {
	h.decideReviewItem(c, true)
}

// CorrectReviewItem updates a pending candidate after manual correction.
func (h *EnrichmentHandler) CorrectReviewItem(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		respondError(c, http.StatusBadRequest, "invalid review item id")
		return
	}

	var req struct {
		ProposedTags      []upsertTagRequest              `json:"proposed_tags"`
		ProposedKnowledge database.ProposedKnowledgeInput `json:"proposed_knowledge"`
		Reviewer          string                          `json:"reviewer"`
		Notes             string                          `json:"notes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid json body")
		return
	}

	item, err := h.repo.CorrectEnrichmentReviewItem(
		id,
		tagRequestsToInputs(req.ProposedTags),
		req.ProposedKnowledge,
		database.ReviewDecisionParams{Reviewer: req.Reviewer, Notes: req.Notes},
	)
	if err != nil {
		if errors.Is(err, database.ErrInvalidQueryParam) {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondError(c, http.StatusNotFound, "review item not found")
			return
		}
		respondError(c, http.StatusInternalServerError, "failed to correct review item")
		return
	}

	data, err := formatReviewItem(*item)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to format review item")
		return
	}
	respondOK(c, data)
}

// RejectReviewItem rejects a candidate without publishing it.
func (h *EnrichmentHandler) RejectReviewItem(c *gin.Context) {
	h.decideReviewItem(c, false)
}

func (h *EnrichmentHandler) decideReviewItem(c *gin.Context, accept bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		respondError(c, http.StatusBadRequest, "invalid review item id")
		return
	}

	var req reviewDecisionRequest
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid json body")
			return
		}
	}

	var item *database.EnrichmentReviewItem
	if accept {
		item, err = h.repo.AcceptEnrichmentReviewItem(id, database.ReviewDecisionParams{Reviewer: req.Reviewer, Notes: req.Notes})
	} else {
		item, err = h.repo.RejectEnrichmentReviewItem(id, database.ReviewDecisionParams{Reviewer: req.Reviewer, Notes: req.Notes})
	}
	if err != nil {
		if errors.Is(err, database.ErrInvalidQueryParam) {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondError(c, http.StatusNotFound, "review item not found")
			return
		}
		respondError(c, http.StatusInternalServerError, "failed to update review item")
		return
	}

	data, err := formatReviewItem(*item)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to format review item")
		return
	}
	respondOK(c, data)
}

func parseOptionalLimit(value string, fallback, maxValue int) (int, bool) {
	if strings.TrimSpace(value) == "" {
		return fallback, true
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 {
		return 0, false
	}
	if limit > maxValue {
		limit = maxValue
	}
	return limit, true
}

func tagRequestsToInputs(tags []upsertTagRequest) []database.TagInput {
	inputs := make([]database.TagInput, len(tags))
	for i, tag := range tags {
		inputs[i] = database.TagInput{
			Name:        tag.Name,
			Category:    tag.Category,
			Description: tag.Description,
			Source:      tag.Source,
		}
	}
	return inputs
}

func formatEnrichmentJob(job database.EnrichmentJob) map[string]any {
	result := map[string]any{
		"id":              job.ID,
		"status":          job.Status,
		"scope":           job.Scope,
		"total_count":     job.TotalCount,
		"processed_count": job.ProcessedCount,
		"accepted_count":  job.AcceptedCount,
		"rejected_count":  job.RejectedCount,
		"error_count":     job.ErrorCount,
		"started_at":      job.StartedAt,
		"finished_at":     job.FinishedAt,
		"created_at":      job.CreatedAt,
		"updated_at":      job.UpdatedAt,
	}
	if strings.TrimSpace(job.ConfigJSON) != "" {
		var config any
		if err := json.Unmarshal([]byte(job.ConfigJSON), &config); err == nil {
			result["config"] = config
		}
	}
	return result
}

func formatReviewItem(item database.EnrichmentReviewItem) (map[string]any, error) {
	var tags []database.TagInput
	if strings.TrimSpace(item.ProposedTagsJSON) != "" {
		if err := json.Unmarshal([]byte(item.ProposedTagsJSON), &tags); err != nil {
			return nil, err
		}
	}

	var knowledge database.ProposedKnowledgeInput
	if strings.TrimSpace(item.ProposedKnowledgeJSON) != "" {
		if err := json.Unmarshal([]byte(item.ProposedKnowledgeJSON), &knowledge); err != nil {
			return nil, err
		}
	}

	return map[string]any{
		"id":                 item.ID,
		"job_id":             item.JobID,
		"poem_id":            item.PoemID,
		"status":             item.Status,
		"proposed_tags":      tagInputsForResponse(tags),
		"proposed_knowledge": knowledge,
		"reviewer":           item.Reviewer,
		"review_notes":       item.ReviewNotes,
		"created_at":         item.CreatedAt,
		"updated_at":         item.UpdatedAt,
	}, nil
}
