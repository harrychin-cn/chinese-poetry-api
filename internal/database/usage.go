package database

import (
	"strings"
	"time"
)

// APIRequestLog stores one API request audit row for commercial operations.
type APIRequestLog struct {
	ID             int64     `json:"id"`
	APIKeyID       *int64    `json:"api_key_id,omitempty"`
	UsageDate      string    `json:"usage_date"`
	Method         string    `json:"method"`
	Path           string    `json:"path"`
	Endpoint       string    `json:"endpoint"`
	StatusCode     int       `json:"status_code"`
	LatencyMs      int64     `json:"latency_ms"`
	Billable       bool      `json:"billable"`
	ErrorClass     string    `json:"error_class,omitempty"`
	QueryText      string    `json:"query_text,omitempty"`
	QuerySignature string    `json:"query_signature,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// RecordAPIRequestParams is the safe input for request audit logging.
type RecordAPIRequestParams struct {
	APIKeyID       *int64
	Method         string
	Path           string
	Endpoint       string
	StatusCode     int
	LatencyMs      int64
	Billable       bool
	ErrorClass     string
	QueryText      string
	QuerySignature string
	CreatedAt      time.Time
}

// UsageDailyStat is a per-day usage aggregate.
type UsageDailyStat struct {
	UsageDate           string  `json:"usage_date"`
	TotalRequests       int     `json:"total_requests"`
	BillableRequests    int     `json:"billable_requests"`
	SuccessRequests     int     `json:"success_requests"`
	ClientErrorRequests int     `json:"client_error_requests"`
	ServerErrorRequests int     `json:"server_error_requests"`
	AvgLatencyMs        float64 `json:"avg_latency_ms"`
	MaxLatencyMs        int64   `json:"max_latency_ms"`
	ErrorRate           float64 `json:"error_rate"`
}

// UsageEndpointStat is an aggregate grouped by method and endpoint.
type UsageEndpointStat struct {
	Method           string  `json:"method"`
	Endpoint         string  `json:"endpoint"`
	TotalRequests    int     `json:"total_requests"`
	BillableRequests int     `json:"billable_requests"`
	ErrorRequests    int     `json:"error_requests"`
	AvgLatencyMs     float64 `json:"avg_latency_ms"`
	MaxLatencyMs     int64   `json:"max_latency_ms"`
	ErrorRate        float64 `json:"error_rate"`
}

// UsageQueryStat is an aggregate for hot search/recall query signatures.
type UsageQueryStat struct {
	Endpoint       string  `json:"endpoint"`
	QueryText      string  `json:"query_text"`
	QuerySignature string  `json:"query_signature"`
	TotalRequests  int     `json:"total_requests"`
	ErrorRequests  int     `json:"error_requests"`
	LastSeenAt     string  `json:"last_seen_at"`
	ErrorRate      float64 `json:"error_rate"`
}

// RecordAPIRequest writes one request audit row. It is intentionally best-effort
// at the middleware layer, but repository callers still receive the DB error.
func (r *Repository) RecordAPIRequest(params RecordAPIRequestParams) error {
	createdAt := params.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	statusCode := params.StatusCode
	if statusCode == 0 {
		statusCode = 200
	}
	latencyMs := params.LatencyMs
	if latencyMs < 0 {
		latencyMs = 0
	}

	log := APIRequestLog{
		APIKeyID:       params.APIKeyID,
		UsageDate:      createdAt.UTC().Format("2006-01-02"),
		Method:         strings.ToUpper(strings.TrimSpace(params.Method)),
		Path:           limitString(strings.TrimSpace(params.Path), 300),
		Endpoint:       limitString(strings.TrimSpace(params.Endpoint), 300),
		StatusCode:     statusCode,
		LatencyMs:      latencyMs,
		Billable:       params.Billable,
		ErrorClass:     limitString(strings.TrimSpace(params.ErrorClass), 80),
		QueryText:      limitString(strings.TrimSpace(params.QueryText), 300),
		QuerySignature: limitString(strings.TrimSpace(params.QuerySignature), 500),
		CreatedAt:      createdAt,
	}
	if log.Method == "" {
		log.Method = "GET"
	}
	if log.Endpoint == "" {
		log.Endpoint = log.Path
	}
	if log.Path == "" {
		log.Path = log.Endpoint
	}

	return r.db.Table("api_request_logs").Create(&log).Error
}

// ListUsageDailyStats returns daily usage stats for all keys or one key.
func (r *Repository) ListUsageDailyStats(apiKeyID *int64, days int) ([]UsageDailyStat, error) {
	days = clampUsageDays(days)
	since := time.Now().UTC().AddDate(0, 0, -days+1).Format("2006-01-02")

	query := r.db.Table("api_request_logs").
		Select(`
			usage_date,
			COUNT(*) AS total_requests,
			COALESCE(SUM(CASE WHEN billable = 1 THEN 1 ELSE 0 END), 0) AS billable_requests,
			COALESCE(SUM(CASE WHEN status_code < 400 THEN 1 ELSE 0 END), 0) AS success_requests,
			COALESCE(SUM(CASE WHEN status_code >= 400 AND status_code < 500 THEN 1 ELSE 0 END), 0) AS client_error_requests,
			COALESCE(SUM(CASE WHEN status_code >= 500 THEN 1 ELSE 0 END), 0) AS server_error_requests,
			COALESCE(AVG(latency_ms), 0) AS avg_latency_ms,
			COALESCE(MAX(latency_ms), 0) AS max_latency_ms
		`).
		Where("usage_date >= ?", since)
	if apiKeyID != nil {
		query = query.Where("api_key_id = ?", *apiKeyID)
	}

	var stats []UsageDailyStat
	err := query.Group("usage_date").Order("usage_date DESC").Scan(&stats).Error
	addDailyErrorRates(stats)
	return stats, err
}

// ListUsageEndpointStats returns endpoint usage/error/latency aggregates.
func (r *Repository) ListUsageEndpointStats(apiKeyID *int64, days, limit int) ([]UsageEndpointStat, error) {
	days = clampUsageDays(days)
	limit = clampUsageLimit(limit)
	since := time.Now().UTC().AddDate(0, 0, -days+1).Format("2006-01-02")

	query := r.db.Table("api_request_logs").
		Select(`
			method,
			endpoint,
			COUNT(*) AS total_requests,
			COALESCE(SUM(CASE WHEN billable = 1 THEN 1 ELSE 0 END), 0) AS billable_requests,
			COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), 0) AS error_requests,
			COALESCE(AVG(latency_ms), 0) AS avg_latency_ms,
			COALESCE(MAX(latency_ms), 0) AS max_latency_ms
		`).
		Where("usage_date >= ?", since)
	if apiKeyID != nil {
		query = query.Where("api_key_id = ?", *apiKeyID)
	}

	var stats []UsageEndpointStat
	err := query.Group("method, endpoint").
		Order("total_requests DESC, error_requests DESC, endpoint ASC").
		Limit(limit).
		Scan(&stats).Error
	addEndpointErrorRates(stats)
	return stats, err
}

// ListUsageQueryStats returns hot query signatures for search and recall endpoints.
func (r *Repository) ListUsageQueryStats(apiKeyID *int64, days, limit int) ([]UsageQueryStat, error) {
	days = clampUsageDays(days)
	limit = clampUsageLimit(limit)
	since := time.Now().UTC().AddDate(0, 0, -days+1).Format("2006-01-02")

	query := r.db.Table("api_request_logs").
		Select(`
			endpoint,
			query_text,
			query_signature,
			COUNT(*) AS total_requests,
			COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), 0) AS error_requests,
			MAX(created_at) AS last_seen_at
		`).
		Where("usage_date >= ? AND query_signature <> ''", since)
	if apiKeyID != nil {
		query = query.Where("api_key_id = ?", *apiKeyID)
	}

	var stats []UsageQueryStat
	err := query.Group("endpoint, query_text, query_signature").
		Order("total_requests DESC, last_seen_at DESC").
		Limit(limit).
		Scan(&stats).Error
	addQueryErrorRates(stats)
	return stats, err
}

func addDailyErrorRates(stats []UsageDailyStat) {
	for i := range stats {
		errors := stats[i].ClientErrorRequests + stats[i].ServerErrorRequests
		if stats[i].TotalRequests > 0 {
			stats[i].ErrorRate = float64(errors) / float64(stats[i].TotalRequests)
		}
	}
}

func addEndpointErrorRates(stats []UsageEndpointStat) {
	for i := range stats {
		if stats[i].TotalRequests > 0 {
			stats[i].ErrorRate = float64(stats[i].ErrorRequests) / float64(stats[i].TotalRequests)
		}
	}
}

func addQueryErrorRates(stats []UsageQueryStat) {
	for i := range stats {
		if stats[i].TotalRequests > 0 {
			stats[i].ErrorRate = float64(stats[i].ErrorRequests) / float64(stats[i].TotalRequests)
		}
	}
}

func clampUsageDays(days int) int {
	if days < 1 {
		return 30
	}
	if days > 366 {
		return 366
	}
	return days
}

func clampUsageLimit(limit int) int {
	if limit < 1 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func limitString(value string, maxLen int) string {
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	for i := range value {
		if i >= maxLen {
			return value[:i]
		}
	}
	return value
}
