package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

// UsageHandler exposes client and admin usage dashboards.
type UsageHandler struct {
	repo *database.Repository
}

func NewUsageHandler(repo *database.Repository) *UsageHandler {
	return &UsageHandler{repo: repo}
}

// ClientDaily returns daily aggregates for the current API key.
func (h *UsageHandler) ClientDaily(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	h.writeDaily(c, &apiKeyID)
}

// ClientEndpoints returns endpoint aggregates for the current API key.
func (h *UsageHandler) ClientEndpoints(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	h.writeEndpoints(c, &apiKeyID)
}

// ClientQueries returns hot query aggregates for the current API key.
func (h *UsageHandler) ClientQueries(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	h.writeQueries(c, &apiKeyID)
}

// AdminDaily returns daily aggregates for all keys or one api_key_id.
func (h *UsageHandler) AdminDaily(c *gin.Context) {
	apiKeyID, ok := optionalAPIKeyID(c)
	if !ok {
		return
	}
	h.writeDaily(c, apiKeyID)
}

// AdminEndpoints returns endpoint aggregates for all keys or one api_key_id.
func (h *UsageHandler) AdminEndpoints(c *gin.Context) {
	apiKeyID, ok := optionalAPIKeyID(c)
	if !ok {
		return
	}
	h.writeEndpoints(c, apiKeyID)
}

// AdminQueries returns hot query aggregates for all keys or one api_key_id.
func (h *UsageHandler) AdminQueries(c *gin.Context) {
	apiKeyID, ok := optionalAPIKeyID(c)
	if !ok {
		return
	}
	h.writeQueries(c, apiKeyID)
}

func (h *UsageHandler) writeDaily(c *gin.Context, apiKeyID *int64) {
	stats, err := h.repo.ListUsageDailyStats(apiKeyID, queryInt(c, "days", 30))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to read daily usage")
		return
	}
	respondOK(c, gin.H{
		"days":       queryInt(c, "days", 30),
		"api_key_id": apiKeyID,
		"items":      stats,
	})
}

func (h *UsageHandler) writeEndpoints(c *gin.Context, apiKeyID *int64) {
	stats, err := h.repo.ListUsageEndpointStats(apiKeyID, queryInt(c, "days", 30), queryInt(c, "limit", 20))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to read endpoint usage")
		return
	}
	respondOK(c, gin.H{
		"days":       queryInt(c, "days", 30),
		"limit":      queryInt(c, "limit", 20),
		"api_key_id": apiKeyID,
		"items":      stats,
	})
}

func (h *UsageHandler) writeQueries(c *gin.Context, apiKeyID *int64) {
	stats, err := h.repo.ListUsageQueryStats(apiKeyID, queryInt(c, "days", 30), queryInt(c, "limit", 20))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to read query usage")
		return
	}
	respondOK(c, gin.H{
		"days":       queryInt(c, "days", 30),
		"limit":      queryInt(c, "limit", 20),
		"api_key_id": apiKeyID,
		"items":      stats,
	})
}

func currentAPIKeyID(c *gin.Context) (int64, bool) {
	value, ok := c.Get("api_key_id")
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	default:
		return 0, false
	}
}

func optionalAPIKeyID(c *gin.Context) (*int64, bool) {
	value := c.Query("api_key_id")
	if value == "" {
		return nil, true
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 1 {
		respondError(c, http.StatusBadRequest, "invalid api_key_id")
		return nil, false
	}
	return &id, true
}

func queryInt(c *gin.Context, key string, fallback int) int {
	value := c.Query(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
