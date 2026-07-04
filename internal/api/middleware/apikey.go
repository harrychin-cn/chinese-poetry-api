package middleware

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

const (
	apiKeyHeader     = "X-API-Key"
	adminTokenHeader = "X-Admin-Token"
)

// APIKeyAuth protects commercial endpoints with persisted API keys.
func APIKeyAuth(repo *database.Repository) gin.HandlerFunc {
	return APIKeyAuthWithRecharge(repo, "")
}

// APIKeyAuthWithRecharge protects commercial endpoints and can include a recharge URL when quota is exhausted.
func APIKeyAuthWithRecharge(repo *database.Repository, rechargeURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		rawKey := extractAPIKey(c)
		apiKey, usage, err := repo.AuthenticateAndRecordAPIKey(rawKey)
		if err != nil {
			if apiKey != nil {
				setAPIKeyContext(c, apiKey)
				setAPIKeyHeaders(c, apiKey, usage)
			}
			writeAPIKeyError(c, err, rechargeURL)
			c.Abort()
			return
		}

		setAPIKeyContext(c, apiKey)
		c.Set("api_key_billable", true)
		setAPIKeyHeaders(c, apiKey, usage)

		c.Next()
	}
}

// APIKeyAuthNoUsage validates API keys without incrementing usage.
// Billing/provision/status endpoints use this so充值/查状态不会消耗客户每日调用额度。
func APIKeyAuthNoUsage(repo *database.Repository) gin.HandlerFunc {
	return func(c *gin.Context) {
		rawKey := extractAPIKey(c)
		apiKey, err := repo.ValidateAPIKey(rawKey)
		if err != nil {
			writeAPIKeyError(c, err, "")
			c.Abort()
			return
		}

		setAPIKeyContext(c, apiKey)
		c.Set("api_key_billable", false)
		c.Header("X-API-Key-ID", formatInt64(apiKey.ID))
		if apiKey.DailyLimit > 0 {
			c.Header("X-API-Key-Daily-Limit", formatInt(apiKey.DailyLimit))
		}

		c.Next()
	}
}

// AdminAuth protects admin endpoints with a static admin token.
func AdminAuth(adminToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.TrimSpace(adminToken) == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "admin token is not configured"})
			c.Abort()
			return
		}

		provided := strings.TrimSpace(c.GetHeader(adminTokenHeader))
		if provided == "" {
			provided = bearerToken(c.GetHeader("Authorization"))
		}

		if subtle.ConstantTimeCompare([]byte(provided), []byte(adminToken)) != 1 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid admin token"})
			c.Abort()
			return
		}

		c.Next()
	}
}

func writeAPIKeyError(c *gin.Context, err error, rechargeURL string) {
	switch {
	case errors.Is(err, database.ErrAPIKeyRequired):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "api key required"})
	case errors.Is(err, database.ErrInvalidAPIKey):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
	case errors.Is(err, database.ErrAPIKeyBlocked):
		c.JSON(http.StatusForbidden, gin.H{"error": "api key blocked"})
	case errors.Is(err, database.ErrAPIQuotaExceeded):
		body := gin.H{
			"error":             "daily api quota exceeded",
			"recharge_endpoint": "/api/v1/billing/qanlo/recharge-session",
		}
		if strings.TrimSpace(rechargeURL) != "" {
			body["recharge_url"] = strings.TrimSpace(rechargeURL)
		}
		c.JSON(http.StatusTooManyRequests, body)
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "api key authentication failed"})
	}
}

func setAPIKeyContext(c *gin.Context, apiKey *database.APIKey) {
	c.Set("api_key", apiKey)
	c.Set("api_key_id", apiKey.ID)
	c.Set("api_key_name", apiKey.Name)
}

func setAPIKeyHeaders(c *gin.Context, apiKey *database.APIKey, usage int) {
	c.Header("X-API-Key-ID", formatInt64(apiKey.ID))
	c.Header("X-API-Key-Usage-Today", formatInt(usage))
	if apiKey.DailyLimit > 0 {
		c.Header("X-API-Key-Daily-Limit", formatInt(apiKey.DailyLimit))
	}
}

func extractAPIKey(c *gin.Context) string {
	if key := strings.TrimSpace(c.GetHeader(apiKeyHeader)); key != "" {
		return key
	}
	if key := bearerToken(c.GetHeader("Authorization")); key != "" {
		return key
	}
	return ""
}

func bearerToken(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 7 && strings.EqualFold(value[:7], "Bearer ") {
		return strings.TrimSpace(value[7:])
	}
	return ""
}

func formatInt(value int) string {
	return formatInt64(int64(value))
}

func formatInt64(value int64) string {
	if value == 0 {
		return "0"
	}

	var buf [20]byte
	i := len(buf)
	negative := value < 0
	if negative {
		value = -value
	}
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
