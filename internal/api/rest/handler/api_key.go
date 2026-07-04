package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/palemoky/chinese-poetry-api/internal/config"
	"github.com/palemoky/chinese-poetry-api/internal/database"
)

// APIKeyHandler manages commercial API keys.
type APIKeyHandler struct {
	repo              *database.Repository
	defaultDailyLimit int
}

// NewAPIKeyHandler creates a new API key admin handler.
func NewAPIKeyHandler(repo *database.Repository, authCfg config.APIAuthConfig) *APIKeyHandler {
	return &APIKeyHandler{
		repo:              repo,
		defaultDailyLimit: authCfg.DefaultDailyLimit,
	}
}

type createAPIKeyRequest struct {
	Name       string `json:"name"`
	Tier       string `json:"tier"`
	DailyLimit *int   `json:"daily_limit"`
	Notes      string `json:"notes"`
}

type updateAPIKeyRequest struct {
	Name       *string `json:"name"`
	Tier       *string `json:"tier"`
	DailyLimit *int    `json:"daily_limit"`
	Enabled    *bool   `json:"enabled"`
	Notes      *string `json:"notes"`
}

// CreateClientAPIKey intentionally does not create keys from the public console.
// API keys must be issued by the admin route or a trusted provisioning flow,
// otherwise an anonymous visitor could mint a usable key without recharge.
func (h *APIKeyHandler) CreateClientAPIKey(c *gin.Context) {
	respondError(c, http.StatusForbidden, "public api key creation is disabled; open or recharge a key from Qanlo, or ask an admin to issue one")
}

// GetCurrentAPIKey returns the current API key profile without incrementing usage.
func (h *APIKeyHandler) GetCurrentAPIKey(c *gin.Context) {
	value, ok := c.Get("api_key")
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	key, ok := value.(*database.APIKey)
	if !ok || key == nil {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}

	usage, err := h.repo.GetAPIKeyUsageCount(key.ID, time.Now().UTC().Format("2006-01-02"))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to read api key usage")
		return
	}

	respondOK(c, formatAPIKeyWithUsage(database.APIKeyWithUsage{
		APIKey:     *key,
		TodayUsage: usage,
	}))
}

// CreateAPIKey creates a commercial API key. The raw key is returned once.
func (h *APIKeyHandler) CreateAPIKey(c *gin.Context) {
	var req createAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid json body")
		return
	}

	dailyLimit := h.defaultDailyLimit
	if req.DailyLimit != nil {
		dailyLimit = *req.DailyLimit
	}

	key, rawKey, err := h.repo.CreateAPIKey(database.CreateAPIKeyParams{
		Name:       req.Name,
		Tier:       req.Tier,
		DailyLimit: dailyLimit,
		Notes:      req.Notes,
	})
	if err != nil {
		if errors.Is(err, database.ErrInvalidQueryParam) {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondError(c, http.StatusInternalServerError, "failed to create api key")
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"data": gin.H{
			"id":          key.ID,
			"account_id":  key.AccountID,
			"name":        key.Name,
			"tier":        key.Tier,
			"daily_limit": key.DailyLimit,
			"enabled":     key.Enabled,
			"notes":       key.Notes,
			"key_prefix":  key.KeyPrefix,
			"api_key":     rawKey,
			"notice":      "store this api_key now; it will not be shown again",
		},
	})
}

// ListAPIKeys lists API keys with today's usage.
func (h *APIKeyHandler) ListAPIKeys(c *gin.Context) {
	keys, err := h.repo.ListAPIKeysWithUsage()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to list api keys")
		return
	}

	data := make([]map[string]any, len(keys))
	for i, key := range keys {
		data[i] = formatAPIKeyWithUsage(key)
	}

	respondOK(c, data)
}

// UpdateAPIKey updates customer/key metadata, status, and daily limit.
func (h *APIKeyHandler) UpdateAPIKey(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		respondError(c, http.StatusBadRequest, "invalid api key id")
		return
	}

	var req updateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid json body")
		return
	}

	key, err := h.repo.UpdateAPIKey(id, database.UpdateAPIKeyParams{
		Name:       req.Name,
		Tier:       req.Tier,
		DailyLimit: req.DailyLimit,
		Enabled:    req.Enabled,
		Notes:      req.Notes,
	})
	if errors.Is(err, database.ErrInvalidQueryParam) {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusNotFound, "api key not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to update api key")
		return
	}

	usage, err := h.repo.GetAPIKeyUsageCount(key.ID, time.Now().UTC().Format("2006-01-02"))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to read api key usage")
		return
	}

	respondOK(c, formatAPIKeyWithUsage(database.APIKeyWithUsage{
		APIKey:     *key,
		TodayUsage: usage,
	}))
}

// RevokeAPIKey revokes an API key by ID.
func (h *APIKeyHandler) RevokeAPIKey(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		respondError(c, http.StatusBadRequest, "invalid api key id")
		return
	}

	err = h.repo.RevokeAPIKey(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusNotFound, "api key not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to revoke api key")
		return
	}

	respondOK(c, gin.H{"id": id, "revoked": true})
}

func formatAPIKeyWithUsage(key database.APIKeyWithUsage) map[string]any {
	result := map[string]any{
		"id":           key.ID,
		"account_id":   key.AccountID,
		"name":         key.Name,
		"tier":         key.Tier,
		"daily_limit":  key.DailyLimit,
		"enabled":      key.Enabled,
		"notes":        key.Notes,
		"key_prefix":   key.KeyPrefix,
		"today_usage":  key.TodayUsage,
		"created_at":   key.CreatedAt,
		"updated_at":   key.UpdatedAt,
		"revoked_at":   key.RevokedAt,
		"has_raw_key":  false,
		"quota_policy": "daily_limit=0 means unlimited",
	}
	return result
}
