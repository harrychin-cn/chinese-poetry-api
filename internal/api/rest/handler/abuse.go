package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

// AbuseHandler exposes operator blocklist management.
type AbuseHandler struct {
	repo *database.Repository
}

// NewAbuseHandler creates an abuse protection handler.
func NewAbuseHandler(repo *database.Repository) *AbuseHandler {
	return &AbuseHandler{repo: repo}
}

type createAbuseBlockRequest struct {
	TargetType  string     `json:"target_type"`
	TargetValue string     `json:"target_value"`
	Reason      string     `json:"reason"`
	Enabled     *bool      `json:"enabled"`
	ExpiresAt   *time.Time `json:"expires_at"`
	TTLMinutes  int        `json:"ttl_minutes"`
	Notes       string     `json:"notes"`
}

type updateAbuseBlockRequest struct {
	Reason         *string    `json:"reason"`
	Enabled        *bool      `json:"enabled"`
	ExpiresAt      *time.Time `json:"expires_at"`
	ClearExpiresAt bool       `json:"clear_expires_at"`
	TTLMinutes     *int       `json:"ttl_minutes"`
	Notes          *string    `json:"notes"`
}

// CreateBlock creates or updates a block row.
func (h *AbuseHandler) CreateBlock(c *gin.Context) {
	var req createAbuseBlockRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid json body")
		return
	}

	expiresAt := expiresAtFromRequest(req.ExpiresAt, req.TTLMinutes)
	block, err := h.repo.UpsertAbuseBlock(database.AbuseBlockParams{
		TargetType:  req.TargetType,
		TargetValue: req.TargetValue,
		Reason:      req.Reason,
		Enabled:     req.Enabled,
		ExpiresAt:   expiresAt,
		CreatedBy:   "admin",
		Notes:       req.Notes,
	})
	if errors.Is(err, database.ErrInvalidQueryParam) {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to create abuse block")
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": formatAbuseBlock(*block)})
}

// ListBlocks lists block rows.
func (h *AbuseHandler) ListBlocks(c *gin.Context) {
	items, err := h.repo.ListAbuseBlocks(database.AbuseBlockFilter{
		TargetType: c.Query("target_type"),
		ActiveOnly: c.DefaultQuery("active_only", "false") == "true",
		Limit:      queryInt(c, "limit", 100),
	})
	if errors.Is(err, database.ErrInvalidQueryParam) {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to list abuse blocks")
		return
	}

	data := make([]map[string]any, len(items))
	for i, item := range items {
		data[i] = formatAbuseBlock(item)
	}
	respondOK(c, gin.H{"items": data})
}

// UpdateBlock updates one block row.
func (h *AbuseHandler) UpdateBlock(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		respondError(c, http.StatusBadRequest, "invalid abuse block id")
		return
	}

	var req updateAbuseBlockRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid json body")
		return
	}

	expiresAt := req.ExpiresAt
	if req.TTLMinutes != nil {
		expiresAt = expiresAtFromRequest(nil, *req.TTLMinutes)
	}
	createdBy := "admin"
	block, err := h.repo.UpdateAbuseBlock(id, database.UpdateAbuseBlockParams{
		Reason:    req.Reason,
		Enabled:   req.Enabled,
		ExpiresAt: expiresAt,
		ClearExpiresAt: req.ClearExpiresAt ||
			(req.TTLMinutes != nil && *req.TTLMinutes <= 0 && req.ExpiresAt == nil),
		CreatedBy: &createdBy,
		Notes:     req.Notes,
	})
	if errors.Is(err, database.ErrInvalidQueryParam) {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusNotFound, "abuse block not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to update abuse block")
		return
	}

	respondOK(c, formatAbuseBlock(*block))
}

func expiresAtFromRequest(explicit *time.Time, ttlMinutes int) *time.Time {
	if explicit != nil {
		return explicit
	}
	if ttlMinutes <= 0 {
		return nil
	}
	expiresAt := time.Now().UTC().Add(time.Duration(ttlMinutes) * time.Minute)
	return &expiresAt
}

func formatAbuseBlock(block database.AbuseBlock) map[string]any {
	return map[string]any{
		"id":           block.ID,
		"target_type":  block.TargetType,
		"target_value": block.TargetValue,
		"reason":       block.Reason,
		"enabled":      block.Enabled,
		"expires_at":   block.ExpiresAt,
		"created_by":   block.CreatedBy,
		"notes":        block.Notes,
		"created_at":   block.CreatedAt,
		"updated_at":   block.UpdatedAt,
	}
}
