package middleware

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

// AbuseBlocklist blocks operator-managed IP targets before route handlers run.
// API key blocks are enforced in Repository.ValidateAPIKey, after the raw key is
// safely matched to a persisted key ID.
func AbuseBlocklist(repo *database.Repository) gin.HandlerFunc {
	return func(c *gin.Context) {
		if repo == nil {
			c.Next()
			return
		}

		block, err := repo.FindActiveAbuseBlock(database.AbuseTargetIP, c.ClientIP())
		if err == nil && block != nil {
			c.JSON(http.StatusForbidden, gin.H{
				"error":        "request blocked",
				"target_type":  block.TargetType,
				"target_value": block.TargetValue,
				"reason":       block.Reason,
				"expires_at":   block.ExpiresAt,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// AbuseDetector auto-blocks repeated 401/429 failures in a short window.
type AbuseDetector struct {
	repo      *database.Repository
	limit     int
	window    time.Duration
	blockFor  time.Duration
	mu        sync.Mutex
	failures  map[string][]time.Time
	blockedAt map[string]time.Time
}

// NewAbuseDetector creates an in-memory detector. It is intentionally simple:
// persisted blocks survive restarts; only the rolling counters are in memory.
func NewAbuseDetector(repo *database.Repository, limit int, window, blockFor time.Duration) *AbuseDetector {
	if limit <= 0 {
		limit = 20
	}
	if window <= 0 {
		window = time.Minute
	}
	if blockFor <= 0 {
		blockFor = time.Hour
	}
	return &AbuseDetector{
		repo:      repo,
		limit:     limit,
		window:    window,
		blockFor:  blockFor,
		failures:  make(map[string][]time.Time),
		blockedAt: make(map[string]time.Time),
	}
}

// Middleware records failed or rate-limited requests after downstream handlers.
func (d *AbuseDetector) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if d == nil || d.repo == nil || !isAbuseSignal(c.Writer.Status()) {
			return
		}
		targetType, targetValue := abuseTargetFromContext(c)
		if targetValue == "" {
			return
		}
		d.recordFailure(targetType, targetValue)
	}
}

func isAbuseSignal(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusTooManyRequests
}

func abuseTargetFromContext(c *gin.Context) (string, string) {
	if value, exists := c.Get("api_key_id"); exists {
		return database.AbuseTargetAPIKey, fmt.Sprint(value)
	}
	return database.AbuseTargetIP, c.ClientIP()
}

func (d *AbuseDetector) recordFailure(targetType, targetValue string) {
	now := time.Now().UTC()
	key := targetType + ":" + targetValue

	d.mu.Lock()
	events := d.failures[key]
	pruned := events[:0]
	for _, at := range events {
		if now.Sub(at) <= d.window {
			pruned = append(pruned, at)
		}
	}
	pruned = append(pruned, now)
	d.failures[key] = pruned
	shouldBlock := len(pruned) >= d.limit
	lastBlock := d.blockedAt[key]
	if shouldBlock {
		d.failures[key] = nil
		d.blockedAt[key] = now
	}
	d.mu.Unlock()

	if !shouldBlock || (!lastBlock.IsZero() && now.Sub(lastBlock) < d.window) {
		return
	}

	expiresAt := now.Add(d.blockFor)
	_, _ = d.repo.UpsertAbuseBlock(database.AbuseBlockParams{
		TargetType:  targetType,
		TargetValue: targetValue,
		Reason:      fmt.Sprintf("auto blocked after %d authentication/rate-limit failures within %s", d.limit, d.window),
		Enabled:     boolPtr(true),
		ExpiresAt:   &expiresAt,
		CreatedBy:   "auto",
		Notes:       "created by abuse detector",
	})
}

func boolPtr(value bool) *bool {
	return &value
}
