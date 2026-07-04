package database

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

const ReverseCreationModelLocal = "stage5-reverse-local"

// ReverseCreationJob records text/image/story-to-work draft generation state.
type ReverseCreationJob struct {
	ID               int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	APIKeyID         int64     `gorm:"column:api_key_id;not null" json:"api_key_id"`
	WorkID           *int64    `gorm:"column:work_id" json:"work_id,omitempty"`
	Status           string    `gorm:"column:status;not null" json:"status"`
	SourceType       string    `gorm:"column:source_type;not null" json:"source_type"`
	SourceText       string    `gorm:"column:source_text" json:"source_text,omitempty"`
	ImageURL         string    `gorm:"column:image_url" json:"image_url,omitempty"`
	WorkType         string    `gorm:"column:work_type;not null" json:"work_type"`
	Style            string    `gorm:"column:style" json:"style,omitempty"`
	Prompt           string    `gorm:"column:prompt;not null" json:"prompt"`
	GeneratedTitle   string    `gorm:"column:generated_title" json:"generated_title,omitempty"`
	GeneratedContent string    `gorm:"column:generated_content" json:"generated_content,omitempty"`
	ErrorMessage     string    `gorm:"column:error_message" json:"error_message,omitempty"`
	Model            string    `gorm:"column:model" json:"model,omitempty"`
	CreatedAt        time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt        time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (ReverseCreationJob) TableName() string { return "reverse_creation_jobs" }

type CreateReverseCreationJobParams struct {
	APIKeyID         int64
	WorkID           *int64
	Status           string
	SourceType       string
	SourceText       string
	ImageURL         string
	WorkType         string
	Style            string
	Prompt           string
	GeneratedTitle   string
	GeneratedContent string
	Model            string
}

func (r *Repository) CreateReverseCreationJob(params CreateReverseCreationJobParams) (*ReverseCreationJob, error) {
	if params.APIKeyID < 1 {
		return nil, ErrInvalidQueryParam
	}
	if params.WorkID != nil {
		if _, err := r.GetOriginalWork(params.APIKeyID, *params.WorkID); err != nil {
			return nil, err
		}
	}
	prompt := limitString(strings.TrimSpace(params.Prompt), 3000)
	if prompt == "" {
		return nil, fmt.Errorf("%w: prompt is required", ErrInvalidQueryParam)
	}
	sourceType := normalizeReverseCreationSourceType(params.SourceType)
	workType := normalizeWorkType(params.WorkType)
	if workType == "" {
		return nil, fmt.Errorf("%w: unsupported work_type", ErrInvalidQueryParam)
	}
	model := strings.TrimSpace(params.Model)
	if model == "" {
		model = ReverseCreationModelLocal
	}
	job := ReverseCreationJob{
		APIKeyID:         params.APIKeyID,
		WorkID:           params.WorkID,
		Status:           normalizeGenerationJobStatus(params.Status),
		SourceType:       sourceType,
		SourceText:       limitString(strings.TrimSpace(params.SourceText), 3000),
		ImageURL:         limitString(strings.TrimSpace(params.ImageURL), 1000),
		WorkType:         workType,
		Style:            limitString(strings.TrimSpace(params.Style), 120),
		Prompt:           prompt,
		GeneratedTitle:   limitString(strings.TrimSpace(params.GeneratedTitle), 120),
		GeneratedContent: limitString(strings.TrimSpace(params.GeneratedContent), 10000),
		Model:            limitString(model, 80),
	}
	if err := r.db.Create(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *Repository) CompleteReverseCreationJob(jobID int64, workID *int64, title, content string) (*ReverseCreationJob, error) {
	if jobID < 1 {
		return nil, ErrInvalidQueryParam
	}
	updates := map[string]any{
		"status":            ImageJobStatusSucceeded,
		"generated_title":   limitString(strings.TrimSpace(title), 120),
		"generated_content": limitString(strings.TrimSpace(content), 10000),
		"error_message":     "",
		"updated_at":        time.Now().UTC(),
	}
	if workID != nil {
		updates["work_id"] = *workID
	}
	return r.updateReverseCreationJob(jobID, updates)
}

func (r *Repository) FailReverseCreationJob(jobID int64, message string) (*ReverseCreationJob, error) {
	if jobID < 1 {
		return nil, ErrInvalidQueryParam
	}
	return r.updateReverseCreationJob(jobID, map[string]any{
		"status":        ImageJobStatusFailed,
		"error_message": limitString(strings.TrimSpace(message), 500),
		"updated_at":    time.Now().UTC(),
	})
}

func (r *Repository) updateReverseCreationJob(jobID int64, updates map[string]any) (*ReverseCreationJob, error) {
	result := r.db.Model(&ReverseCreationJob{}).Where("id = ?", jobID).Updates(updates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var job ReverseCreationJob
	if err := r.db.Where("id = ?", jobID).First(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *Repository) ListReverseCreationJobs(apiKeyID int64, limit int) ([]ReverseCreationJob, error) {
	if apiKeyID < 1 {
		return nil, ErrInvalidQueryParam
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	var items []ReverseCreationJob
	if err := r.db.Where("api_key_id = ?", apiKeyID).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func normalizeReverseCreationSourceType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "image", "mood", "prompt":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "story"
	}
}
