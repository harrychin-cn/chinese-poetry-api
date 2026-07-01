package database

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	// AbuseTargetIP blocks a client IP before route handlers run.
	AbuseTargetIP = "ip"
	// AbuseTargetAPIKey blocks a persisted local API key ID during API key auth.
	AbuseTargetAPIKey = "api_key"
)

// AbuseBlock is an operator-managed or auto-created blocklist row.
type AbuseBlock struct {
	ID          int64      `json:"id"`
	TargetType  string     `json:"target_type"`
	TargetValue string     `json:"target_value"`
	Reason      string     `json:"reason"`
	Enabled     bool       `json:"enabled"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	CreatedBy   string     `json:"created_by"`
	Notes       string     `json:"notes,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// AbuseBlockParams holds create/upsert values for a blocklist row.
type AbuseBlockParams struct {
	TargetType  string
	TargetValue string
	Reason      string
	Enabled     *bool
	ExpiresAt   *time.Time
	CreatedBy   string
	Notes       string
}

// AbuseBlockFilter controls admin listing.
type AbuseBlockFilter struct {
	TargetType string
	ActiveOnly bool
	Limit      int
}

// UpdateAbuseBlockParams holds admin-editable block fields.
type UpdateAbuseBlockParams struct {
	Reason         *string
	Enabled        *bool
	ExpiresAt      *time.Time
	ClearExpiresAt bool
	CreatedBy      *string
	Notes          *string
}

// UpsertAbuseBlock creates or updates a block row for one target.
func (r *Repository) UpsertAbuseBlock(params AbuseBlockParams) (*AbuseBlock, error) {
	targetType, targetValue, err := normalizeAbuseTarget(params.TargetType, params.TargetValue)
	if err != nil {
		return nil, err
	}

	enabled := true
	if params.Enabled != nil {
		enabled = *params.Enabled
	}
	now := time.Now().UTC()
	reason := strings.TrimSpace(params.Reason)
	if reason == "" {
		reason = "manual"
	}
	createdBy := strings.TrimSpace(params.CreatedBy)
	if createdBy == "" {
		createdBy = "operator"
	}

	var block AbuseBlock
	err = r.db.Table("abuse_blocks").
		Where("target_type = ? AND target_value = ?", targetType, targetValue).
		First(&block).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		block = AbuseBlock{
			TargetType:  targetType,
			TargetValue: targetValue,
			Reason:      reason,
			Enabled:     enabled,
			ExpiresAt:   params.ExpiresAt,
			CreatedBy:   createdBy,
			Notes:       strings.TrimSpace(params.Notes),
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := r.db.Table("abuse_blocks").Create(&block).Error; err != nil {
			return nil, err
		}
		return &block, nil
	}
	if err != nil {
		return nil, err
	}

	updates := map[string]any{
		"reason":     reason,
		"enabled":    enabled,
		"expires_at": params.ExpiresAt,
		"created_by": createdBy,
		"notes":      strings.TrimSpace(params.Notes),
		"updated_at": now,
	}
	if err := r.db.Table("abuse_blocks").Where("id = ?", block.ID).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := r.db.Table("abuse_blocks").Where("id = ?", block.ID).First(&block).Error; err != nil {
		return nil, err
	}
	return &block, nil
}

// ListAbuseBlocks returns recent blocklist rows for admin dashboards.
func (r *Repository) ListAbuseBlocks(filter AbuseBlockFilter) ([]AbuseBlock, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	query := r.db.Table("abuse_blocks")
	if strings.TrimSpace(filter.TargetType) != "" {
		targetType, err := normalizeAbuseTargetType(filter.TargetType)
		if err != nil {
			return nil, err
		}
		query = query.Where("target_type = ?", targetType)
	}
	if filter.ActiveOnly {
		now := time.Now().UTC()
		query = query.Where("enabled = ? AND (expires_at IS NULL OR expires_at > ?)", true, now)
	}

	var blocks []AbuseBlock
	err := query.Order("id DESC").Limit(limit).Find(&blocks).Error
	return blocks, err
}

// UpdateAbuseBlock updates one blocklist row by ID.
func (r *Repository) UpdateAbuseBlock(id int64, params UpdateAbuseBlockParams) (*AbuseBlock, error) {
	if id < 1 {
		return nil, fmt.Errorf("%w: block id must be positive", ErrInvalidQueryParam)
	}

	updates := map[string]any{}
	if params.Reason != nil {
		reason := strings.TrimSpace(*params.Reason)
		if reason == "" {
			reason = "manual"
		}
		updates["reason"] = reason
	}
	if params.Enabled != nil {
		updates["enabled"] = *params.Enabled
	}
	if params.ExpiresAt != nil {
		updates["expires_at"] = params.ExpiresAt
	} else if params.ClearExpiresAt {
		updates["expires_at"] = nil
	}
	if params.CreatedBy != nil {
		createdBy := strings.TrimSpace(*params.CreatedBy)
		if createdBy == "" {
			createdBy = "operator"
		}
		updates["created_by"] = createdBy
	}
	if params.Notes != nil {
		updates["notes"] = strings.TrimSpace(*params.Notes)
	}

	if len(updates) > 0 {
		updates["updated_at"] = time.Now().UTC()
		result := r.db.Table("abuse_blocks").Where("id = ?", id).Updates(updates)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			return nil, gorm.ErrRecordNotFound
		}
	}

	var block AbuseBlock
	err := r.db.Table("abuse_blocks").Where("id = ?", id).First(&block).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, gorm.ErrRecordNotFound
	}
	return &block, err
}

// FindActiveAbuseBlock returns a currently enabled, unexpired block or nil.
func (r *Repository) FindActiveAbuseBlock(targetType, targetValue string) (*AbuseBlock, error) {
	targetType, targetValue, err := normalizeAbuseTarget(targetType, targetValue)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	var block AbuseBlock
	err = r.db.Table("abuse_blocks").
		Where("target_type = ? AND target_value = ? AND enabled = ? AND (expires_at IS NULL OR expires_at > ?)",
			targetType, targetValue, true, now).
		First(&block).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &block, nil
}

func normalizeAbuseTarget(targetType, targetValue string) (string, string, error) {
	normalizedType, err := normalizeAbuseTargetType(targetType)
	if err != nil {
		return "", "", err
	}
	normalizedValue := strings.TrimSpace(targetValue)
	if normalizedValue == "" {
		return "", "", fmt.Errorf("%w: target_value is required", ErrInvalidQueryParam)
	}
	if normalizedType == AbuseTargetAPIKey {
		id, err := strconv.ParseInt(normalizedValue, 10, 64)
		if err != nil || id < 1 {
			return "", "", fmt.Errorf("%w: api_key target_value must be a positive id", ErrInvalidQueryParam)
		}
		normalizedValue = strconv.FormatInt(id, 10)
	}
	return normalizedType, normalizedValue, nil
}

func normalizeAbuseTargetType(targetType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(targetType)) {
	case AbuseTargetIP:
		return AbuseTargetIP, nil
	case AbuseTargetAPIKey, "api-key", "key":
		return AbuseTargetAPIKey, nil
	default:
		return "", fmt.Errorf("%w: target_type must be ip or api_key", ErrInvalidQueryParam)
	}
}
