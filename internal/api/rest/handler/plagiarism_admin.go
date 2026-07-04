package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

// PlagiarismAdminHandler manages corpus sources and manual originality reviews.
type PlagiarismAdminHandler struct {
	repo *database.Repository
}

func NewPlagiarismAdminHandler(repo *database.Repository) *PlagiarismAdminHandler {
	return &PlagiarismAdminHandler{repo: repo}
}

type createCorpusSourceRequest struct {
	SourceType string `json:"source_type"`
	Title      string `json:"title"`
	Author     string `json:"author"`
	SourceURL  string `json:"source_url"`
	Content    string `json:"content"`
	Status     string `json:"status"`
	Notes      string `json:"notes"`
	CreatedBy  string `json:"created_by"`
}

type manualReviewDecisionRequest struct {
	Reviewer string `json:"reviewer"`
	Notes    string `json:"notes"`
}

func (h *PlagiarismAdminHandler) CreateCorpusSource(c *gin.Context) {
	var req createCorpusSourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid json body")
		return
	}

	source, err := h.repo.CreatePlagiarismCorpusSource(database.CreatePlagiarismCorpusSourceParams{
		SourceType: req.SourceType,
		Title:      req.Title,
		Author:     req.Author,
		SourceURL:  req.SourceURL,
		Content:    req.Content,
		Status:     req.Status,
		Notes:      req.Notes,
		CreatedBy:  req.CreatedBy,
	})
	if errors.Is(err, database.ErrInvalidQueryParam) {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to create plagiarism corpus source")
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": formatPlagiarismCorpusSource(*source)})
}

func (h *PlagiarismAdminHandler) ListCorpusSources(c *gin.Context) {
	limit, ok := parseOptionalLimit(c.Query("limit"), 50, 200)
	if !ok {
		respondError(c, http.StatusBadRequest, "invalid limit")
		return
	}
	sources, err := h.repo.ListPlagiarismCorpusSources(database.ListPlagiarismCorpusSourcesParams{
		SourceType: c.DefaultQuery("source_type", "all"),
		Status:     c.DefaultQuery("status", database.PlagiarismCorpusStatusEnabled),
		Limit:      limit,
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to list plagiarism corpus sources")
		return
	}

	data := make([]map[string]any, len(sources))
	for i, source := range sources {
		data[i] = formatPlagiarismCorpusSource(source)
	}
	respondOK(c, gin.H{"items": data})
}

func (h *PlagiarismAdminHandler) ListReviewQueue(c *gin.Context) {
	limit, ok := parseOptionalLimit(c.Query("limit"), 50, 200)
	if !ok {
		respondError(c, http.StatusBadRequest, "invalid limit")
		return
	}
	items, err := h.repo.ListManualReviewQueue(c.DefaultQuery("status", database.ManualReviewStatusPending), limit)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to list plagiarism review queue")
		return
	}

	data := make([]map[string]any, len(items))
	for i, item := range items {
		data[i] = formatManualReviewQueueEntry(item)
	}
	respondOK(c, gin.H{"items": data})
}

func (h *PlagiarismAdminHandler) ApproveReviewQueueItem(c *gin.Context) {
	h.decideReviewQueueItem(c, true)
}

func (h *PlagiarismAdminHandler) RejectReviewQueueItem(c *gin.Context) {
	h.decideReviewQueueItem(c, false)
}

func (h *PlagiarismAdminHandler) decideReviewQueueItem(c *gin.Context, approve bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		respondError(c, http.StatusBadRequest, "invalid review queue id")
		return
	}

	var req manualReviewDecisionRequest
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid json body")
			return
		}
	}

	entry, err := h.repo.DecideManualReviewQueue(id, approve, database.ManualReviewDecisionParams{
		Reviewer: req.Reviewer,
		Notes:    req.Notes,
	})
	if errors.Is(err, database.ErrInvalidQueryParam) {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusNotFound, "review queue item not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to update review queue item")
		return
	}

	respondOK(c, formatManualReviewQueueEntry(*entry))
}

func formatPlagiarismCorpusSource(source database.PlagiarismCorpusSource) map[string]any {
	return map[string]any{
		"id":          source.ID,
		"source_type": source.SourceType,
		"title":       source.Title,
		"author":      source.Author,
		"source_url":  source.SourceURL,
		"content":     source.Content,
		"status":      source.Status,
		"notes":       source.Notes,
		"created_by":  source.CreatedBy,
		"created_at":  source.CreatedAt,
		"updated_at":  source.UpdatedAt,
	}
}

func formatManualReviewQueueEntry(entry database.ManualReviewQueueEntry) map[string]any {
	return map[string]any{
		"id":           entry.Queue.ID,
		"status":       entry.Queue.Status,
		"risk_level":   entry.Queue.RiskLevel,
		"reason":       entry.Queue.Reason,
		"reviewer":     entry.Queue.Reviewer,
		"review_notes": entry.Queue.ReviewNotes,
		"decided_at":   entry.Queue.DecidedAt,
		"created_at":   entry.Queue.CreatedAt,
		"updated_at":   entry.Queue.UpdatedAt,
		"work": map[string]any{
			"id":                entry.Work.ID,
			"work_code":         entry.Work.WorkCode,
			"title":             entry.Work.Title,
			"status":            entry.Work.Status,
			"visibility":        entry.Work.Visibility,
			"plagiarism_status": entry.Work.PlagiarismStatus,
		},
		"report": map[string]any{
			"id":            entry.Report.ID,
			"risk_level":    entry.Report.RiskLevel,
			"risk_reason":   entry.Report.RiskReason,
			"review_status": entry.Report.ReviewStatus,
		},
	}
}
