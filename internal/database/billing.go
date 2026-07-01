package database

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

var (
	// ErrQanloBindingNotFound means the local API key has no Qanlo binding yet.
	ErrQanloBindingNotFound = errors.New("qanlo binding not found")
	// ErrInvalidQanloState means the Qanlo callback state is missing, unknown, or expired.
	ErrInvalidQanloState = errors.New("invalid qanlo callback state")
)

// QanloBinding stores the Qanlo Agent Key binding mirror for one local API key.
// The raw Qanlo key is never stored; only hash and prefix/masked values are kept.
type QanloBinding struct {
	ID                int64      `json:"id"`
	APIKeyID          int64      `json:"api_key_id"`
	Status            string     `json:"status"`
	ExternalUserID    string     `json:"external_user_id"`
	ExternalDeviceID  string     `json:"external_device_id"`
	QanloKeyHash      string     `json:"-"`
	QanloKeyPrefix    string     `json:"qanlo_key_prefix"`
	QanloBaseURL      string     `json:"qanlo_base_url"`
	CallbackState     string     `json:"callback_state,omitempty"`
	CallbackExpiresAt *time.Time `json:"callback_expires_at,omitempty"`
	LastSyncedAt      *time.Time `json:"last_synced_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// QanloBindingSessionParams is used when starting a provision/recharge browser flow.
type QanloBindingSessionParams struct {
	APIKeyID          int64
	ExternalUserID    string
	ExternalDeviceID  string
	CallbackState     string
	CallbackExpiresAt time.Time
}

// QanloCallbackParams is the sanitized callback data received from Qanlo.
type QanloCallbackParams struct {
	CallbackState string
	RawQanloKey   string
	QanloBaseURL  string
	RawQuery      string
	EventType     string
}

// GetQanloBindingByAPIKeyID returns a Qanlo binding by local API key id.
func (r *Repository) GetQanloBindingByAPIKeyID(apiKeyID int64) (*QanloBinding, error) {
	var binding QanloBinding
	err := r.db.Table("api_key_qanlo_bindings").
		Where("api_key_id = ?", apiKeyID).
		First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrQanloBindingNotFound
	}
	if err != nil {
		return nil, err
	}
	return &binding, nil
}

// GetQanloBindingByCallbackState returns a non-expired binding by callback state.
func (r *Repository) GetQanloBindingByCallbackState(state string) (*QanloBinding, error) {
	state = strings.TrimSpace(state)
	if state == "" {
		return nil, ErrInvalidQanloState
	}

	var binding QanloBinding
	err := r.db.Table("api_key_qanlo_bindings").
		Where("callback_state = ? AND callback_expires_at > ?", state, time.Now().UTC()).
		First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInvalidQanloState
	}
	if err != nil {
		return nil, err
	}
	return &binding, nil
}

// UpsertQanloBindingSession creates or refreshes a provision/recharge session.
func (r *Repository) UpsertQanloBindingSession(params QanloBindingSessionParams) (*QanloBinding, error) {
	if params.APIKeyID < 1 {
		return nil, fmt.Errorf("%w: api_key_id must be positive", ErrInvalidQueryParam)
	}
	if strings.TrimSpace(params.ExternalUserID) == "" || strings.TrimSpace(params.ExternalDeviceID) == "" {
		return nil, fmt.Errorf("%w: external identity is required", ErrInvalidQueryParam)
	}
	if strings.TrimSpace(params.CallbackState) == "" {
		return nil, fmt.Errorf("%w: callback_state is required", ErrInvalidQueryParam)
	}

	now := time.Now().UTC()
	result := r.db.Exec(`
		INSERT INTO api_key_qanlo_bindings (
			api_key_id, status, external_user_id, external_device_id,
			callback_state, callback_expires_at, created_at, updated_at
		)
		VALUES (?, 'pending', ?, ?, ?, ?, ?, ?)
		ON CONFLICT(api_key_id)
		DO UPDATE SET
			status = CASE
				WHEN api_key_qanlo_bindings.status = 'linked' THEN api_key_qanlo_bindings.status
				ELSE 'pending'
			END,
			external_user_id = excluded.external_user_id,
			external_device_id = excluded.external_device_id,
			callback_state = excluded.callback_state,
			callback_expires_at = excluded.callback_expires_at,
			updated_at = excluded.updated_at
	`, params.APIKeyID, params.ExternalUserID, params.ExternalDeviceID, strings.TrimSpace(params.CallbackState), params.CallbackExpiresAt.UTC(), now, now)
	if result.Error != nil {
		return nil, result.Error
	}

	return r.GetQanloBindingByAPIKeyID(params.APIKeyID)
}

// SaveQanloCallback validates a callback state and stores the Qanlo binding mirror.
func (r *Repository) SaveQanloCallback(params QanloCallbackParams) (*QanloBinding, error) {
	state := strings.TrimSpace(params.CallbackState)
	rawKey := strings.TrimSpace(params.RawQanloKey)
	if state == "" || rawKey == "" {
		return nil, ErrInvalidQanloState
	}

	now := time.Now().UTC()
	var binding QanloBinding
	err := r.db.Table("api_key_qanlo_bindings").
		Where("callback_state = ? AND callback_expires_at > ?", state, now).
		First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInvalidQanloState
	}
	if err != nil {
		return nil, err
	}

	keyPrefix := MaskSecret(rawKey)
	err = r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			UPDATE api_key_qanlo_bindings
			SET status = 'linked',
				qanlo_key_hash = ?,
				qanlo_key_prefix = ?,
				qanlo_base_url = ?,
				last_synced_at = ?,
				updated_at = ?
			WHERE id = ?
		`, HashAPIKey(rawKey), keyPrefix, strings.TrimSpace(params.QanloBaseURL), now, now, binding.ID).Error; err != nil {
			return err
		}

		eventType := strings.TrimSpace(params.EventType)
		if eventType == "" {
			eventType = "callback"
		}
		return tx.Exec(`
			INSERT INTO qanlo_callback_events (
				api_key_id, callback_state, event_type, key_prefix, qanlo_base_url, raw_query, created_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(callback_state) DO NOTHING
		`, binding.APIKeyID, state, eventType, keyPrefix, strings.TrimSpace(params.QanloBaseURL), strings.TrimSpace(params.RawQuery), now).Error
	})
	if err != nil {
		return nil, err
	}

	return r.GetQanloBindingByAPIKeyID(binding.APIKeyID)
}

// RecordQanloReturn records a Qanlo browser return even when no new key is present,
// for example after a recharge-only flow.
func (r *Repository) RecordQanloReturn(callbackState, eventType, rawQuery string) (*QanloBinding, error) {
	binding, err := r.GetQanloBindingByCallbackState(callbackState)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		eventType = "return"
	}

	err = r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			UPDATE api_key_qanlo_bindings
			SET last_synced_at = ?,
				updated_at = ?
			WHERE id = ?
		`, now, now, binding.ID).Error; err != nil {
			return err
		}

		return tx.Exec(`
			INSERT INTO qanlo_callback_events (
				api_key_id, callback_state, event_type, key_prefix, qanlo_base_url, raw_query, created_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(callback_state) DO NOTHING
		`, binding.APIKeyID, strings.TrimSpace(callbackState), eventType, binding.QanloKeyPrefix, binding.QanloBaseURL, strings.TrimSpace(rawQuery), now).Error
	})
	if err != nil {
		return nil, err
	}

	return r.GetQanloBindingByAPIKeyID(binding.APIKeyID)
}

// MaskSecret returns a display-safe secret mask. It never returns the full value.
func MaskSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 10 {
		return value[:min(len(value), 4)] + "***"
	}
	return value[:6] + "..." + value[len(value)-4:]
}
