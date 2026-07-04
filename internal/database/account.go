package database

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

var (
	// ErrUserHandleTaken means the requested public account handle is already used.
	ErrUserHandleTaken = errors.New("user handle already exists")
)

// UserAccount is the public author/account profile attached to one or more API keys.
// MVP auth stays API-Key based: the key is the credential, the account is the public author identity.
type UserAccount struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Handle      string    `gorm:"column:handle;not null;uniqueIndex" json:"handle"`
	DisplayName string    `gorm:"column:display_name;not null" json:"display_name"`
	Email       string    `gorm:"column:email" json:"email,omitempty"`
	Bio         string    `gorm:"column:bio" json:"bio,omitempty"`
	AvatarURL   string    `gorm:"column:avatar_url" json:"avatar_url,omitempty"`
	WebsiteURL  string    `gorm:"column:website_url" json:"website_url,omitempty"`
	Status      string    `gorm:"column:status;not null" json:"status"`
	CreatedAt   time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (UserAccount) TableName() string { return "user_accounts" }

// UpdateUserAccountParams contains profile fields users can edit from the console.
type UpdateUserAccountParams struct {
	Handle      *string
	DisplayName *string
	Email       *string
	Bio         *string
	AvatarURL   *string
	WebsiteURL  *string
}

// GetOrCreateUserAccountForAPIKey returns the account bound to an API key.
// Existing keys from older deployments are lazily backfilled with a default public handle.
func (r *Repository) GetOrCreateUserAccountForAPIKey(apiKeyID int64) (*UserAccount, error) {
	if apiKeyID < 1 {
		return nil, ErrInvalidQueryParam
	}

	var account UserAccount
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var key APIKey
		if err := tx.Table("api_keys").Where("id = ?", apiKeyID).First(&key).Error; err != nil {
			return err
		}
		if key.AccountID != nil && *key.AccountID > 0 {
			if err := tx.Table("user_accounts").Where("id = ?", *key.AccountID).First(&account).Error; err == nil {
				return nil
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}

		created, err := createDefaultUserAccountTx(tx, apiKeyID, key.Name)
		if err != nil {
			return err
		}
		account = *created
		return tx.Table("api_keys").Where("id = ?", apiKeyID).Update("account_id", account.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &account, nil
}

// UpdateUserAccountForAPIKey edits the public profile for the current API key's account.
func (r *Repository) UpdateUserAccountForAPIKey(apiKeyID int64, params UpdateUserAccountParams) (*UserAccount, error) {
	account, err := r.GetOrCreateUserAccountForAPIKey(apiKeyID)
	if err != nil {
		return nil, err
	}

	updates := map[string]any{}
	if params.Handle != nil {
		handle, err := normalizeUserHandle(*params.Handle)
		if err != nil {
			return nil, err
		}
		if handle != account.Handle {
			var count int64
			if err := r.db.Table("user_accounts").Where("handle = ? AND id <> ?", handle, account.ID).Count(&count).Error; err != nil {
				return nil, err
			}
			if count > 0 {
				return nil, ErrUserHandleTaken
			}
			updates["handle"] = handle
		}
	}
	if params.DisplayName != nil {
		displayName := limitString(strings.TrimSpace(*params.DisplayName), 80)
		if displayName == "" {
			return nil, fmt.Errorf("%w: display_name is required", ErrInvalidQueryParam)
		}
		updates["display_name"] = displayName
	}
	if params.Email != nil {
		updates["email"] = limitString(strings.TrimSpace(*params.Email), 160)
	}
	if params.Bio != nil {
		updates["bio"] = limitString(strings.TrimSpace(*params.Bio), 500)
	}
	if params.AvatarURL != nil {
		updates["avatar_url"] = limitString(strings.TrimSpace(*params.AvatarURL), 300)
	}
	if params.WebsiteURL != nil {
		updates["website_url"] = limitString(strings.TrimSpace(*params.WebsiteURL), 300)
	}
	if len(updates) == 0 {
		return account, nil
	}
	updates["updated_at"] = time.Now().UTC()

	if err := r.db.Table("user_accounts").Where("id = ?", account.ID).Updates(updates).Error; err != nil {
		return nil, err
	}
	return r.GetUserAccountByID(account.ID)
}

// GetUserAccountByID loads one account.
func (r *Repository) GetUserAccountByID(id int64) (*UserAccount, error) {
	if id < 1 {
		return nil, ErrInvalidQueryParam
	}
	var account UserAccount
	if err := r.db.Table("user_accounts").Where("id = ?", id).First(&account).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

// GetPublicUserAccountByHandle loads one public account profile.
func (r *Repository) GetPublicUserAccountByHandle(handle string) (*UserAccount, error) {
	handle, err := normalizeUserHandle(handle)
	if err != nil {
		return nil, err
	}
	var account UserAccount
	if err := r.db.Table("user_accounts").Where("handle = ? AND status = ?", handle, "active").First(&account).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

// CountPublicOriginalWorksByAccount counts published public works for an account.
func (r *Repository) CountPublicOriginalWorksByAccount(accountID int64) (int64, error) {
	if accountID < 1 {
		return 0, ErrInvalidQueryParam
	}
	var count int64
	err := r.db.Table("original_works").
		Joins("JOIN api_keys ON api_keys.id = original_works.api_key_id").
		Where("api_keys.account_id = ? AND original_works.status = ? AND original_works.visibility = ?", accountID, WorkStatusPublished, WorkVisibilityPublic).
		Count(&count).Error
	return count, err
}

// ListPublicOriginalWorksByAccount returns public works for a profile page.
func (r *Repository) ListPublicOriginalWorksByAccount(accountID int64, limit int) ([]OriginalWork, error) {
	if accountID < 1 {
		return nil, ErrInvalidQueryParam
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	var works []OriginalWork
	err := r.db.Table("original_works").
		Select("original_works.*").
		Joins("JOIN api_keys ON api_keys.id = original_works.api_key_id").
		Where("api_keys.account_id = ? AND original_works.status = ? AND original_works.visibility = ?", accountID, WorkStatusPublished, WorkVisibilityPublic).
		Order("original_works.published_at DESC, original_works.id DESC").
		Limit(limit).
		Find(&works).Error
	return works, err
}

func createDefaultUserAccountTx(tx *gorm.DB, apiKeyID int64, keyName string) (*UserAccount, error) {
	displayName := limitString(strings.TrimSpace(keyName), 80)
	if displayName == "" {
		displayName = fmt.Sprintf("\u4f5c\u8005%d", apiKeyID)
	}
	account := &UserAccount{
		Handle:      fmt.Sprintf("u%06d", apiKeyID),
		DisplayName: displayName,
		Status:      "active",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := tx.Table("user_accounts").Create(account).Error; err != nil {
		return nil, err
	}
	return account, nil
}

func normalizeUserHandle(value string) (string, error) {
	handle := strings.ToLower(strings.TrimSpace(value))
	if len(handle) < 2 || len(handle) > 40 {
		return "", fmt.Errorf("%w: handle must be 2-40 characters", ErrInvalidQueryParam)
	}
	for _, r := range handle {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return "", fmt.Errorf("%w: handle only supports letters, numbers, hyphen and underscore", ErrInvalidQueryParam)
	}
	return handle, nil
}
