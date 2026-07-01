package database

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUsageRequestLogAggregates(t *testing.T) {
	db := setupTestDB(t)
	require.NoError(t, db.migrateAPIKeyTables())
	require.NoError(t, db.migrateRequestLogTables())
	repo := NewRepository(db)

	key, _, err := repo.CreateAPIKey(CreateAPIKeyParams{Name: "usage customer", DailyLimit: 10})
	require.NoError(t, err)

	now := time.Now().UTC()
	require.NoError(t, repo.RecordAPIRequest(RecordAPIRequestParams{
		APIKeyID:       &key.ID,
		Method:         "GET",
		Path:           "/api/v1/poems/query",
		Endpoint:       "/api/v1/poems/query",
		StatusCode:     200,
		LatencyMs:      12,
		Billable:       true,
		QueryText:      "q=明月",
		QuerySignature: "/api/v1/poems/query?q=明月",
		CreatedAt:      now,
	}))
	require.NoError(t, repo.RecordAPIRequest(RecordAPIRequestParams{
		APIKeyID:       &key.ID,
		Method:         "GET",
		Path:           "/api/v1/poems/query",
		Endpoint:       "/api/v1/poems/query",
		StatusCode:     500,
		LatencyMs:      30,
		Billable:       true,
		ErrorClass:     "server_error",
		QueryText:      "q=明月",
		QuerySignature: "/api/v1/poems/query?q=明月",
		CreatedAt:      now,
	}))
	require.NoError(t, repo.RecordAPIRequest(RecordAPIRequestParams{
		APIKeyID:   &key.ID,
		Method:     "GET",
		Path:       "/api/v1/usage/daily",
		Endpoint:   "/api/v1/usage/daily",
		StatusCode: 200,
		LatencyMs:  3,
		Billable:   false,
		CreatedAt:  now,
	}))

	daily, err := repo.ListUsageDailyStats(&key.ID, 30)
	require.NoError(t, err)
	require.Len(t, daily, 1)
	assert.Equal(t, 3, daily[0].TotalRequests)
	assert.Equal(t, 2, daily[0].BillableRequests)
	assert.Equal(t, 1, daily[0].ServerErrorRequests)
	assert.InDelta(t, 1.0/3.0, daily[0].ErrorRate, 0.001)

	endpoints, err := repo.ListUsageEndpointStats(&key.ID, 30, 10)
	require.NoError(t, err)
	require.Len(t, endpoints, 2)
	assert.Equal(t, "/api/v1/poems/query", endpoints[0].Endpoint)
	assert.Equal(t, 2, endpoints[0].TotalRequests)
	assert.Equal(t, 2, endpoints[0].BillableRequests)
	assert.Equal(t, 1, endpoints[0].ErrorRequests)

	queries, err := repo.ListUsageQueryStats(&key.ID, 30, 10)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "q=明月", queries[0].QueryText)
	assert.Equal(t, 2, queries[0].TotalRequests)
}
