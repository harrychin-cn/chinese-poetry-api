package middleware

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

// RequestAudit records API request metadata for operation dashboards.
// It runs after handlers so route-specific auth middleware can attach api_key_id.
func RequestAudit(repo *database.Repository) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		if repo == nil || !strings.HasPrefix(c.Request.URL.Path, "/api/v1/") {
			return
		}

		apiKeyID, _ := getInt64Context(c, "api_key_id")
		billable, _ := c.Get("api_key_billable")
		queryText, querySignature := requestQuerySignature(c)
		endpoint := c.FullPath()
		if endpoint == "" {
			endpoint = c.Request.URL.Path
		}

		_ = repo.RecordAPIRequest(database.RecordAPIRequestParams{
			APIKeyID:       apiKeyID,
			Method:         c.Request.Method,
			Path:           c.Request.URL.Path,
			Endpoint:       endpoint,
			StatusCode:     responseStatus(c),
			LatencyMs:      time.Since(start).Milliseconds(),
			Billable:       billable == true,
			ErrorClass:     responseErrorClass(c),
			QueryText:      queryText,
			QuerySignature: querySignature,
			CreatedAt:      time.Now().UTC(),
		})
	}
}

func getInt64Context(c *gin.Context, key string) (*int64, bool) {
	value, ok := c.Get(key)
	if !ok {
		return nil, false
	}
	switch typed := value.(type) {
	case int64:
		return &typed, true
	case int:
		converted := int64(typed)
		return &converted, true
	default:
		return nil, false
	}
}

func responseStatus(c *gin.Context) int {
	status := c.Writer.Status()
	if status == 0 {
		return http.StatusOK
	}
	return status
}

func responseErrorClass(c *gin.Context) string {
	status := responseStatus(c)
	switch {
	case status >= 500:
		return "server_error"
	case status >= 400:
		return "client_error"
	default:
		return ""
	}
}

func requestQuerySignature(c *gin.Context) (string, string) {
	keys := []string{
		"q",
		"keyword",
		"author",
		"author_id",
		"dynasty",
		"dynasty_id",
		"type",
		"type_id",
		"tag",
		"tag_category",
		"scenario",
		"search_in",
		"lines",
		"chars_per_line",
		"sort",
	}
	values := make([]string, 0, len(keys))
	query := c.Request.URL.Query()
	for _, key := range keys {
		rawValues, ok := query[key]
		if !ok {
			continue
		}
		cleanValues := make([]string, 0, len(rawValues))
		for _, value := range rawValues {
			value = strings.Join(strings.Fields(value), " ")
			if value != "" {
				cleanValues = append(cleanValues, value)
			}
		}
		if len(cleanValues) == 0 {
			continue
		}
		sort.Strings(cleanValues)
		values = append(values, key+"="+strings.Join(cleanValues, "|"))
	}
	if len(values) == 0 {
		return "", ""
	}
	signature := strings.Join(values, "&")
	endpoint := c.FullPath()
	if endpoint == "" {
		endpoint = c.Request.URL.Path
	}
	return signature, endpoint + "?" + signature
}
