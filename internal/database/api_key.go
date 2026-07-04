package database

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

var (
	// ErrAPIKeyRequired means the request did not include an API key.
	ErrAPIKeyRequired = errors.New("api key required")
	// ErrInvalidAPIKey means the provided API key is unknown, disabled, or revoked.
	ErrInvalidAPIKey = errors.New("invalid api key")
	// ErrAPIQuotaExceeded means the key has used up its daily quota.
	ErrAPIQuotaExceeded = errors.New("daily api quota exceeded")
	// ErrAPIKeyBlocked means the key is temporarily or permanently blocked by abuse protection.
	ErrAPIKeyBlocked = errors.New("api key blocked")
)

// APIKey is a persisted commercial API key. KeyHash is never exposed.
type APIKey struct {
	ID         int64      `json:"id"`
	AccountID  *int64     `json:"account_id,omitempty"`
	KeyHash    string     `json:"-"`
	KeyPrefix  string     `json:"key_prefix"`
	Name       string     `json:"name"`
	Tier       string     `json:"tier"`
	DailyLimit int        `json:"daily_limit"`
	Enabled    bool       `json:"enabled"`
	Notes      string     `json:"notes,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

// APIKeyUsage stores per-day usage counters.
type APIKeyUsage struct {
	ID           int64     `json:"id"`
	APIKeyID     int64     `json:"api_key_id"`
	UsageDate    string    `json:"usage_date"`
	RequestCount int       `json:"request_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// APIKeyWithUsage is used for admin listing.
type APIKeyWithUsage struct {
	APIKey
	TodayUsage int `json:"today_usage"`
}

// CreateAPIKeyParams holds input for creating a new key.
type CreateAPIKeyParams struct {
	Name       string
	Tier       string
	DailyLimit int
	Notes      string
}

// UpdateAPIKeyParams holds admin-editable API key fields.
type UpdateAPIKeyParams struct {
	Name       *string
	Tier       *string
	DailyLimit *int
	Enabled    *bool
	Notes      *string
}

// CreateAPIKey creates an API key and returns the raw key once.
func (r *Repository) CreateAPIKey(params CreateAPIKeyParams) (*APIKey, string, error) {
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return nil, "", fmt.Errorf("%w: name is required", ErrInvalidQueryParam)
	}

	tier := strings.TrimSpace(params.Tier)
	if tier == "" {
		tier = "free"
	}

	if params.DailyLimit < 0 {
		return nil, "", fmt.Errorf("%w: daily_limit cannot be negative", ErrInvalidQueryParam)
	}

	rawKey, err := generateRawAPIKey()
	if err != nil {
		return nil, "", err
	}

	now := time.Now().UTC()
	key := &APIKey{
		KeyHash:    HashAPIKey(rawKey),
		KeyPrefix:  rawKey[:min(len(rawKey), 18)],
		Name:       name,
		Tier:       tier,
		DailyLimit: params.DailyLimit,
		Enabled:    true,
		Notes:      strings.TrimSpace(params.Notes),
		CreatedAt:  now,
		UpdatedAt:  &now,
	}

	if err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("api_keys").Create(key).Error; err != nil {
			return err
		}
		account, err := createDefaultUserAccountTx(tx, key.ID, key.Name)
		if err != nil {
			return err
		}
		key.AccountID = &account.ID
		return tx.Table("api_keys").Where("id = ?", key.ID).Update("account_id", account.ID).Error
	}); err != nil {
		return nil, "", err
	}

	return key, rawKey, nil
}

// ListAPIKeysWithUsage returns API keys with today's usage counter.
func (r *Repository) ListAPIKeysWithUsage() ([]APIKeyWithUsage, error) {
	today := time.Now().UTC().Format("2006-01-02")
	var keys []APIKeyWithUsage

	err := r.db.Table("api_keys").
		Select("api_keys.*, COALESCE(api_key_usage.request_count, 0) AS today_usage").
		Joins("LEFT JOIN api_key_usage ON api_keys.id = api_key_usage.api_key_id AND api_key_usage.usage_date = ?", today).
		Order("api_keys.id DESC").
		Scan(&keys).Error

	return keys, err
}

// UpdateAPIKey updates admin-editable API key metadata and quota.
func (r *Repository) UpdateAPIKey(id int64, params UpdateAPIKeyParams) (*APIKey, error) {
	if id < 1 {
		return nil, fmt.Errorf("%w: api key id must be positive", ErrInvalidQueryParam)
	}

	updates := map[string]any{}
	if params.Name != nil {
		name := strings.TrimSpace(*params.Name)
		if name == "" {
			return nil, fmt.Errorf("%w: name cannot be empty", ErrInvalidQueryParam)
		}
		updates["name"] = name
	}
	if params.Tier != nil {
		tier := strings.TrimSpace(*params.Tier)
		if tier == "" {
			tier = "free"
		}
		updates["tier"] = tier
	}
	if params.DailyLimit != nil {
		if *params.DailyLimit < 0 {
			return nil, fmt.Errorf("%w: daily_limit cannot be negative", ErrInvalidQueryParam)
		}
		updates["daily_limit"] = *params.DailyLimit
	}
	if params.Enabled != nil {
		updates["enabled"] = *params.Enabled
		if *params.Enabled {
			updates["revoked_at"] = nil
		} else {
			updates["revoked_at"] = time.Now().UTC()
		}
	}
	if params.Notes != nil {
		updates["notes"] = strings.TrimSpace(*params.Notes)
	}
	if len(updates) == 0 {
		var key APIKey
		err := r.db.Table("api_keys").Where("id = ?", id).First(&key).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, gorm.ErrRecordNotFound
		}
		return &key, err
	}
	updates["updated_at"] = time.Now().UTC()

	result := r.db.Table("api_keys").Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	var key APIKey
	if err := r.db.Table("api_keys").Where("id = ?", id).First(&key).Error; err != nil {
		return nil, err
	}
	return &key, nil
}

// RevokeAPIKey disables an API key and records the revocation time.
func (r *Repository) RevokeAPIKey(id int64) error {
	result := r.db.Table("api_keys").
		Where("id = ? AND revoked_at IS NULL", id).
		Updates(map[string]any{
			"enabled":    false,
			"updated_at": time.Now().UTC(),
			"revoked_at": time.Now().UTC(),
		})

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// AuthenticateAndRecordAPIKey validates a raw API key and increments today's usage.
func (r *Repository) AuthenticateAndRecordAPIKey(rawKey string) (*APIKey, int, error) {
	key, err := r.ValidateAPIKey(rawKey)
	if err != nil {
		return key, 0, err
	}

	today := time.Now().UTC().Format("2006-01-02")
	if err := r.incrementAPIKeyUsage(key.ID, today, key.DailyLimit); err != nil {
		if errors.Is(err, ErrAPIQuotaExceeded) {
			usage, _ := r.getAPIKeyUsageCount(key.ID, today)
			return key, usage, err
		}
		return nil, 0, err
	}

	usage, err := r.getAPIKeyUsageCount(key.ID, today)
	if err != nil {
		return nil, 0, err
	}

	return key, usage, nil
}

// RecordAPIKeyUsage increments today's usage for an already validated API key.
func (r *Repository) RecordAPIKeyUsage(key *APIKey) (int, error) {
	if key == nil {
		return 0, ErrAPIKeyRequired
	}

	today := time.Now().UTC().Format("2006-01-02")
	if err := r.incrementAPIKeyUsage(key.ID, today, key.DailyLimit); err != nil {
		if errors.Is(err, ErrAPIQuotaExceeded) {
			usage, _ := r.getAPIKeyUsageCount(key.ID, today)
			return usage, err
		}
		return 0, err
	}

	return r.getAPIKeyUsageCount(key.ID, today)
}

// ValidateAPIKey validates a raw API key without incrementing usage.
func (r *Repository) ValidateAPIKey(rawKey string) (*APIKey, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return nil, ErrAPIKeyRequired
	}

	var key APIKey
	err := r.db.Table("api_keys").
		Where("key_hash = ? AND enabled = ? AND revoked_at IS NULL", HashAPIKey(rawKey), true).
		First(&key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInvalidAPIKey
	}
	if err != nil {
		return nil, err
	}
	block, err := r.FindActiveAbuseBlock(AbuseTargetAPIKey, fmt.Sprint(key.ID))
	if err != nil {
		return nil, err
	}
	if block != nil {
		return &key, ErrAPIKeyBlocked
	}
	return &key, nil
}

// GetAPIKeyUsageCount returns usage count for a key/date pair.
func (r *Repository) GetAPIKeyUsageCount(apiKeyID int64, usageDate string) (int, error) {
	return r.getAPIKeyUsageCount(apiKeyID, usageDate)
}

func (r *Repository) incrementAPIKeyUsage(apiKeyID int64, usageDate string, dailyLimit int) error {
	var result *gorm.DB
	if dailyLimit > 0 {
		result = r.db.Exec(`
			INSERT INTO api_key_usage (api_key_id, usage_date, request_count, created_at, updated_at)
			VALUES (?, ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
			ON CONFLICT(api_key_id, usage_date)
			DO UPDATE SET
				request_count = request_count + 1,
				updated_at = CURRENT_TIMESTAMP
			WHERE request_count < ?
		`, apiKeyID, usageDate, dailyLimit)
	} else {
		result = r.db.Exec(`
			INSERT INTO api_key_usage (api_key_id, usage_date, request_count, created_at, updated_at)
			VALUES (?, ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
			ON CONFLICT(api_key_id, usage_date)
			DO UPDATE SET
				request_count = request_count + 1,
				updated_at = CURRENT_TIMESTAMP
		`, apiKeyID, usageDate)
	}

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrAPIQuotaExceeded
	}
	return nil
}

func (r *Repository) getAPIKeyUsageCount(apiKeyID int64, usageDate string) (int, error) {
	var usage APIKeyUsage
	err := r.db.Table("api_key_usage").
		Where("api_key_id = ? AND usage_date = ?", apiKeyID, usageDate).
		First(&usage).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return usage.RequestCount, nil
}

// HashAPIKey hashes a raw API key for storage and comparison.
func HashAPIKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}

func generateRawAPIKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "cp_live_" + base64.RawURLEncoding.EncodeToString(buf), nil
}
