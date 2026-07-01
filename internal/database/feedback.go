package database

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

// FeedbackItem is a customer-submitted feedback record.
type FeedbackItem struct {
	ID         int64     `gorm:"primaryKey;autoIncrement"`
	APIKeyID   int64     `gorm:"column:api_key_id;not null"`
	Type       string    `gorm:"column:type;not null"`
	Subject    string    `gorm:"column:subject"`
	Message    string    `gorm:"column:message;not null"`
	Contact    string    `gorm:"column:contact"`
	Status     string    `gorm:"column:status;not null;default:open"`
	AdminNotes string    `gorm:"column:admin_notes"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
}

// TableName returns the feedback table name.
func (FeedbackItem) TableName() string {
	return "feedback_items"
}

// CreateFeedbackParams contains customer feedback input.
type CreateFeedbackParams struct {
	APIKeyID int64
	Type     string
	Subject  string
	Message  string
	Contact  string
}

// UpdateFeedbackParams contains admin feedback update fields.
type UpdateFeedbackParams struct {
	Status     *string
	AdminNotes *string
}

// CreateFeedback stores customer feedback.
func (r *Repository) CreateFeedback(params CreateFeedbackParams) (*FeedbackItem, error) {
	if params.APIKeyID < 1 {
		return nil, ErrInvalidQueryParam
	}
	message := strings.TrimSpace(params.Message)
	if message == "" {
		return nil, ErrInvalidQueryParam
	}

	feedbackType := normalizeFeedbackType(params.Type)
	item := &FeedbackItem{
		APIKeyID: params.APIKeyID,
		Type:     feedbackType,
		Subject:  limitString(strings.TrimSpace(params.Subject), 200),
		Message:  limitString(message, 2000),
		Contact:  limitString(strings.TrimSpace(params.Contact), 200),
		Status:   "open",
	}
	if err := r.db.Create(item).Error; err != nil {
		return nil, err
	}
	return item, nil
}

// ListFeedback lists customer feedback for admin operations.
func (r *Repository) ListFeedback(status string, apiKeyID *int64, limit int) ([]FeedbackItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	q := r.db.Order("created_at DESC").Limit(limit)
	if status = strings.TrimSpace(status); status != "" && status != "all" {
		q = q.Where("status = ?", status)
	}
	if apiKeyID != nil {
		q = q.Where("api_key_id = ?", *apiKeyID)
	}

	var items []FeedbackItem
	if err := q.Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// UpdateFeedback updates admin status/notes for a feedback item.
func (r *Repository) UpdateFeedback(id int64, params UpdateFeedbackParams) (*FeedbackItem, error) {
	if id < 1 {
		return nil, ErrInvalidQueryParam
	}
	updates := map[string]any{"updated_at": time.Now()}
	if params.Status != nil {
		status := normalizeFeedbackStatus(*params.Status)
		if status == "" {
			return nil, ErrInvalidQueryParam
		}
		updates["status"] = status
	}
	if params.AdminNotes != nil {
		updates["admin_notes"] = limitString(strings.TrimSpace(*params.AdminNotes), 1000)
	}

	result := r.db.Model(&FeedbackItem{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	var item FeedbackItem
	if err := r.db.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		return nil, err
	}
	return &item, nil
}

func normalizeFeedbackType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "bug", "data", "feature", "billing", "other":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "other"
	}
}

func normalizeFeedbackStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "open", "reviewing", "resolved", "closed":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}
