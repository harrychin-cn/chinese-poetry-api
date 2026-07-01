package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// RateLimiter holds rate limiting configuration
type RateLimiter struct {
	limiters   map[string]*rate.Limiter
	lastSeen   map[string]time.Time
	mu         sync.RWMutex
	rps        rate.Limit
	burst      int
	ttl        time.Duration
	lastPruned time.Time
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(rps float64, burst int) *RateLimiter {
	return &RateLimiter{
		limiters:   make(map[string]*rate.Limiter),
		lastSeen:   make(map[string]time.Time),
		rps:        rate.Limit(rps),
		burst:      burst,
		ttl:        10 * time.Minute,
		lastPruned: time.Now(),
	}
}

// getLimiter returns a rate limiter for the given key.
func (rl *RateLimiter) getLimiter(key string) *rate.Limiter {
	now := time.Now()
	rl.pruneStale(now)

	rl.mu.RLock()
	limiter, exists := rl.limiters[key]
	rl.mu.RUnlock()

	if exists {
		rl.mu.Lock()
		rl.lastSeen[key] = now
		rl.mu.Unlock()
		return limiter
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check after acquiring write lock
	if limiter, exists := rl.limiters[key]; exists {
		rl.lastSeen[key] = now
		return limiter
	}

	limiter = rate.NewLimiter(rl.rps, rl.burst)
	rl.limiters[key] = limiter
	rl.lastSeen[key] = now

	return limiter
}

func (rl *RateLimiter) pruneStale(now time.Time) {
	rl.mu.RLock()
	shouldPrune := now.Sub(rl.lastPruned) >= time.Minute
	rl.mu.RUnlock()
	if !shouldPrune {
		return
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()
	if now.Sub(rl.lastPruned) < time.Minute {
		return
	}
	for key, seenAt := range rl.lastSeen {
		if now.Sub(seenAt) > rl.ttl {
			delete(rl.lastSeen, key)
			delete(rl.limiters, key)
		}
	}
	rl.lastPruned = now
}

// Middleware returns a Gin middleware function for rate limiting
func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return rl.MiddlewareByKey("ip", func(c *gin.Context) string {
		return c.ClientIP()
	})
}

// APIKeyMiddleware rate limits requests by authenticated API key ID.
// It must run after API key auth middleware has populated api_key_id.
func (rl *RateLimiter) APIKeyMiddleware() gin.HandlerFunc {
	return rl.MiddlewareByKey("api_key", func(c *gin.Context) string {
		if value, exists := c.Get("api_key_id"); exists {
			return "api_key:" + fmt.Sprint(value)
		}
		return "ip:" + c.ClientIP()
	})
}

// APIKeyTokenMiddleware rate limits by API key token before authentication.
// The token is hashed before it is used as the in-memory limiter key, so the
// raw API key is never retained in the limiter map. Put this before API key
// auth if short-cycle 429 responses should not consume daily quota.
func (rl *RateLimiter) APIKeyTokenMiddleware() gin.HandlerFunc {
	return rl.MiddlewareByKey("api_key", func(c *gin.Context) string {
		rawKey := extractAPIKey(c)
		if rawKey == "" {
			return "ip:" + c.ClientIP()
		}
		sum := sha256.Sum256([]byte(rawKey))
		return "api_key_token:" + hex.EncodeToString(sum[:])
	})
}

// MiddlewareByKey returns a Gin middleware that limits by a caller-provided key.
func (rl *RateLimiter) MiddlewareByKey(scope string, keyFunc func(*gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := keyFunc(c)
		if key == "" {
			key = c.ClientIP()
		}
		limiter := rl.getLimiter(key)

		if !limiter.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "rate limit exceeded",
				"message": "too many requests, please try again later",
				"scope":   scope,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
