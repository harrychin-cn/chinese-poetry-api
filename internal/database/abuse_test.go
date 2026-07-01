package database

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupAbuseTestRepo(t *testing.T) *Repository {
	t.Helper()

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	db := NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	return NewRepository(db)
}

func TestAbuseBlockLifecycle(t *testing.T) {
	repo := setupAbuseTestRepo(t)
	expiresAt := time.Now().UTC().Add(time.Hour)

	block, err := repo.UpsertAbuseBlock(AbuseBlockParams{
		TargetType:  AbuseTargetIP,
		TargetValue: "203.0.113.10",
		Reason:      "load spike",
		ExpiresAt:   &expiresAt,
		CreatedBy:   "admin",
	})
	require.NoError(t, err)
	require.NotZero(t, block.ID)

	active, err := repo.FindActiveAbuseBlock(AbuseTargetIP, "203.0.113.10")
	require.NoError(t, err)
	require.NotNil(t, active)
	assert.Equal(t, "load spike", active.Reason)

	enabled := false
	updated, err := repo.UpdateAbuseBlock(block.ID, UpdateAbuseBlockParams{Enabled: &enabled})
	require.NoError(t, err)
	assert.False(t, updated.Enabled)

	active, err = repo.FindActiveAbuseBlock(AbuseTargetIP, "203.0.113.10")
	require.NoError(t, err)
	assert.Nil(t, active)
}

func TestAbuseBlockRejectsInvalidAPIKeyTarget(t *testing.T) {
	repo := setupAbuseTestRepo(t)

	_, err := repo.UpsertAbuseBlock(AbuseBlockParams{
		TargetType:  AbuseTargetAPIKey,
		TargetValue: "not-an-id",
	})
	require.ErrorIs(t, err, ErrInvalidQueryParam)
}

func TestValidateAPIKeyRejectsBlockedKey(t *testing.T) {
	repo := setupAbuseTestRepo(t)

	key, rawKey, err := repo.CreateAPIKey(CreateAPIKeyParams{Name: "blocked customer", DailyLimit: 10})
	require.NoError(t, err)

	_, err = repo.UpsertAbuseBlock(AbuseBlockParams{
		TargetType:  AbuseTargetAPIKey,
		TargetValue: "1",
		Reason:      "chargeback",
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), key.ID)

	_, err = repo.ValidateAPIKey(rawKey)
	require.ErrorIs(t, err, ErrAPIKeyBlocked)
}
