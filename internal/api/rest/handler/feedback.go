package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

// FeedbackHandler manages customer feedback.
type FeedbackHandler struct {
	repo *database.Repository
}

// NewFeedbackHandler creates a feedback handler.
func NewFeedbackHandler(repo *database.Repository) *FeedbackHandler {
	return &FeedbackHandler{repo: repo}
}

type createFeedbackRequest struct {
	Type    string `json:"type"`
	Subject string `json:"subject"`
	Message string `json:"message"`
	Contact string `json:"contact"`
}

type updateFeedbackRequest struct {
	Status     *string `json:"status"`
	AdminNotes *string `json:"admin_notes"`
}

// Create stores customer feedback for the current API key.
func (h *FeedbackHandler) Create(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}

	var req createFeedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid json body")
		return
	}

	item, err := h.repo.CreateFeedback(database.CreateFeedbackParams{
		APIKeyID: apiKeyID,
		Type:     req.Type,
		Subject:  req.Subject,
		Message:  req.Message,
		Contact:  req.Contact,
	})
	if errors.Is(err, database.ErrInvalidQueryParam) {
		respondError(c, http.StatusBadRequest, "message is required")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to create feedback")
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": formatFeedback(*item)})
}

// List returns feedback items for admins.
func (h *FeedbackHandler) List(c *gin.Context) {
	apiKeyID, ok := optionalAPIKeyID(c)
	if !ok {
		return
	}
	items, err := h.repo.ListFeedback(c.DefaultQuery("status", "open"), apiKeyID, queryInt(c, "limit", 50))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to list feedback")
		return
	}

	data := make([]map[string]any, len(items))
	for i, item := range items {
		data[i] = formatFeedback(item)
	}
	respondOK(c, gin.H{"items": data})
}

// Update updates admin status/notes for feedback.
func (h *FeedbackHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		respondError(c, http.StatusBadRequest, "invalid feedback id")
		return
	}

	var req updateFeedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid json body")
		return
	}

	item, err := h.repo.UpdateFeedback(id, database.UpdateFeedbackParams{
		Status:     req.Status,
		AdminNotes: req.AdminNotes,
	})
	if errors.Is(err, database.ErrInvalidQueryParam) {
		respondError(c, http.StatusBadRequest, "invalid feedback status")
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusNotFound, "feedback not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to update feedback")
		return
	}

	respondOK(c, formatFeedback(*item))
}

func formatFeedback(item database.FeedbackItem) map[string]any {
	return map[string]any{
		"id":          item.ID,
		"api_key_id":  item.APIKeyID,
		"type":        item.Type,
		"subject":     item.Subject,
		"message":     item.Message,
		"contact":     item.Contact,
		"status":      item.Status,
		"admin_notes": item.AdminNotes,
		"created_at":  item.CreatedAt,
		"updated_at":  item.UpdatedAt,
	}
}
