package database

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateAndAuthenticateAPIKey(t *testing.T) {
	db := setupTestDB(t)
	require.NoError(t, db.migrateAPIKeyTables())
	require.NoError(t, db.migrateAbuseTables())
	repo := NewRepository(db)

	key, rawKey, err := repo.CreateAPIKey(CreateAPIKeyParams{
		Name:       "developer customer",
		Tier:       "developer",
		DailyLimit: 2,
	})
	require.NoError(t, err)
	require.NotEmpty(t, rawKey)
	assert.NotContains(t, key.KeyHash, rawKey)
	assert.Equal(t, "developer customer", key.Name)
	assert.Equal(t, 2, key.DailyLimit)

	authKey, usage, err := repo.AuthenticateAndRecordAPIKey(rawKey)
	require.NoError(t, err)
	assert.Equal(t, key.ID, authKey.ID)
	assert.Equal(t, 1, usage)

	_, usage, err = repo.AuthenticateAndRecordAPIKey(rawKey)
	require.NoError(t, err)
	assert.Equal(t, 2, usage)

	_, usage, err = repo.AuthenticateAndRecordAPIKey(rawKey)
	assert.True(t, errors.Is(err, ErrAPIQuotaExceeded))
	assert.Equal(t, 2, usage)

	keys, err := repo.ListAPIKeysWithUsage()
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, 2, keys[0].TodayUsage)
}

func TestAPIKeyInvalidAndRevoked(t *testing.T) {
	db := setupTestDB(t)
	require.NoError(t, db.migrateAPIKeyTables())
	require.NoError(t, db.migrateAbuseTables())
	repo := NewRepository(db)

	_, _, err := repo.AuthenticateAndRecordAPIKey("")
	assert.True(t, errors.Is(err, ErrAPIKeyRequired))

	_, _, err = repo.AuthenticateAndRecordAPIKey("cp_live_not_real")
	assert.True(t, errors.Is(err, ErrInvalidAPIKey))

	key, rawKey, err := repo.CreateAPIKey(CreateAPIKeyParams{
		Name:       "revoked customer",
		DailyLimit: 100,
	})
	require.NoError(t, err)

	require.NoError(t, repo.RevokeAPIKey(key.ID))

	_, _, err = repo.AuthenticateAndRecordAPIKey(rawKey)
	assert.True(t, errors.Is(err, ErrInvalidAPIKey))
}

func TestUpdateAPIKeyMetadataQuotaAndStatus(t *testing.T) {
	db := setupTestDB(t)
	require.NoError(t, db.migrateAPIKeyTables())
	require.NoError(t, db.migrateAbuseTables())
	repo := NewRepository(db)

	key, rawKey, err := repo.CreateAPIKey(CreateAPIKeyParams{
		Name:       "old customer",
		Tier:       "trial",
		DailyLimit: 1,
		Notes:      "old note",
	})
	require.NoError(t, err)

	name := "new customer"
	tier := "pro"
	limit := 3
	notes := "manual upgrade after recharge"
	updated, err := repo.UpdateAPIKey(key.ID, UpdateAPIKeyParams{
		Name:       &name,
		Tier:       &tier,
		DailyLimit: &limit,
		Notes:      &notes,
	})
	require.NoError(t, err)
	assert.Equal(t, name, updated.Name)
	assert.Equal(t, tier, updated.Tier)
	assert.Equal(t, limit, updated.DailyLimit)
	assert.Equal(t, notes, updated.Notes)
	assert.True(t, updated.Enabled)

	enabled := false
	updated, err = repo.UpdateAPIKey(key.ID, UpdateAPIKeyParams{Enabled: &enabled})
	require.NoError(t, err)
	assert.False(t, updated.Enabled)
	assert.NotNil(t, updated.RevokedAt)

	_, _, err = repo.AuthenticateAndRecordAPIKey(rawKey)
	assert.True(t, errors.Is(err, ErrInvalidAPIKey))

	enabled = true
	updated, err = repo.UpdateAPIKey(key.ID, UpdateAPIKeyParams{Enabled: &enabled})
	require.NoError(t, err)
	assert.True(t, updated.Enabled)
	assert.Nil(t, updated.RevokedAt)

	_, usage, err := repo.AuthenticateAndRecordAPIKey(rawKey)
	require.NoError(t, err)
	assert.Equal(t, 1, usage)
}
