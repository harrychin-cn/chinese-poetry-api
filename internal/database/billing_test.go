package database

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQanloBindingSessionAndCallback(t *testing.T) {
	db := setupTestDB(t)
	require.NoError(t, db.migrateAPIKeyTables())
	require.NoError(t, db.migrateBillingTables())
	repo := NewRepository(db)

	key, _, err := repo.CreateAPIKey(CreateAPIKeyParams{
		Name:       "billing customer",
		DailyLimit: 10,
	})
	require.NoError(t, err)

	binding, err := repo.UpsertQanloBindingSession(QanloBindingSessionParams{
		APIKeyID:          key.ID,
		ExternalUserID:    "poetry-user-1",
		ExternalDeviceID:  "poetry-device-1",
		CallbackState:     "state-1",
		CallbackExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	require.NoError(t, err)
	assert.Equal(t, key.ID, binding.APIKeyID)
	assert.Equal(t, "pending", binding.Status)
	assert.Equal(t, "poetry-user-1", binding.ExternalUserID)

	linked, err := repo.SaveQanloCallback(QanloCallbackParams{
		CallbackState:      "state-1",
		QanloKeyHash:       HashAPIKey("sk-qanlo-secret"),
		QanloKeyPrefix:     MaskSecret("sk-qanlo-secret"),
		QanloKeyCiphertext: "encrypted",
		QanloBaseURL:       "https://qanlo.com/v1",
		RawQuery:           "state=state-1&key=sk-qanlo-secret",
		EventType:          "callback",
	})
	require.NoError(t, err)
	assert.Equal(t, "linked", linked.Status)
	assert.True(t, linked.QanloKeyHash != "" && linked.QanloKeyHash != "sk-qanlo-secret")
	assert.Equal(t, "sk-qan...cret", linked.QanloKeyPrefix)
	assert.Equal(t, "https://qanlo.com/v1", linked.QanloBaseURL)

	var eventCount int64
	require.NoError(t, repo.db.Table("qanlo_callback_events").Where("callback_state = ?", "state-1").Count(&eventCount).Error)
	assert.Equal(t, int64(1), eventCount)
}

func TestQanloCallbackRejectsExpiredState(t *testing.T) {
	db := setupTestDB(t)
	require.NoError(t, db.migrateAPIKeyTables())
	require.NoError(t, db.migrateBillingTables())
	repo := NewRepository(db)

	key, _, err := repo.CreateAPIKey(CreateAPIKeyParams{Name: "expired customer", DailyLimit: 10})
	require.NoError(t, err)

	_, err = repo.UpsertQanloBindingSession(QanloBindingSessionParams{
		APIKeyID:          key.ID,
		ExternalUserID:    "poetry-user-1",
		ExternalDeviceID:  "poetry-device-1",
		CallbackState:     "expired-state",
		CallbackExpiresAt: time.Now().UTC().Add(-time.Hour),
	})
	require.NoError(t, err)

	_, err = repo.SaveQanloCallback(QanloCallbackParams{
		CallbackState:      "expired-state",
		QanloKeyHash:       HashAPIKey("sk-qanlo-secret"),
		QanloKeyPrefix:     MaskSecret("sk-qanlo-secret"),
		QanloKeyCiphertext: "encrypted",
	})
	assert.True(t, errors.Is(err, ErrInvalidQanloState))
}

func TestRecordQanloReturn(t *testing.T) {
	db := setupTestDB(t)
	require.NoError(t, db.migrateAPIKeyTables())
	require.NoError(t, db.migrateBillingTables())
	repo := NewRepository(db)

	key, _, err := repo.CreateAPIKey(CreateAPIKeyParams{Name: "return customer", DailyLimit: 10})
	require.NoError(t, err)

	_, err = repo.UpsertQanloBindingSession(QanloBindingSessionParams{
		APIKeyID:          key.ID,
		ExternalUserID:    "poetry-user-1",
		ExternalDeviceID:  "poetry-device-1",
		CallbackState:     "return-state",
		CallbackExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	require.NoError(t, err)

	binding, err := repo.RecordQanloReturn("return-state", "recharge_return", "state=return-state")
	require.NoError(t, err)
	assert.Equal(t, key.ID, binding.APIKeyID)
	assert.NotNil(t, binding.LastSyncedAt)

	var eventCount int64
	require.NoError(t, repo.db.Table("qanlo_callback_events").Where("callback_state = ? AND event_type = ?", "return-state", "recharge_return").Count(&eventCount).Error)
	assert.Equal(t, int64(1), eventCount)
}
