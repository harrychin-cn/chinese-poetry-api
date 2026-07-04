package database

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	MediaAssetTypeImage       = "image"
	MediaAssetSourceGenerated = "generated"

	ImageJobStatusPending     = "pending"
	ImageJobStatusPromptReady = "prompt_ready"
	ImageJobStatusSucceeded   = "succeeded"
	ImageJobStatusFailed      = "failed"
)

// MediaAsset stores generated or uploaded media linked to an original work.
type MediaAsset struct {
	ID              int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	WorkID          int64     `gorm:"column:work_id;not null" json:"work_id"`
	APIKeyID        int64     `gorm:"column:api_key_id;not null" json:"api_key_id"`
	AssetType       string    `gorm:"column:asset_type;not null" json:"asset_type"`
	Source          string    `gorm:"column:source;not null" json:"source"`
	URL             string    `gorm:"column:url" json:"url,omitempty"`
	B64JSON         string    `gorm:"column:b64_json" json:"b64_json,omitempty"`
	MimeType        string    `gorm:"column:mime_type" json:"mime_type,omitempty"`
	Model           string    `gorm:"column:model" json:"model,omitempty"`
	Size            string    `gorm:"column:size" json:"size,omitempty"`
	Quality         string    `gorm:"column:quality" json:"quality,omitempty"`
	OutputFormat    string    `gorm:"column:output_format" json:"output_format,omitempty"`
	Prompt          string    `gorm:"column:prompt" json:"prompt,omitempty"`
	RevisedPrompt   string    `gorm:"column:revised_prompt" json:"revised_prompt,omitempty"`
	StorageProvider string    `gorm:"column:storage_provider" json:"storage_provider,omitempty"`
	StorageKey      string    `gorm:"column:storage_key" json:"storage_key,omitempty"`
	FilePath        string    `gorm:"column:file_path" json:"file_path,omitempty"`
	ByteSize        int64     `gorm:"column:byte_size;not null" json:"byte_size"`
	ChecksumSHA256  string    `gorm:"column:checksum_sha256" json:"checksum_sha256,omitempty"`
	CreditCost      int       `gorm:"column:credit_cost;not null" json:"credit_cost"`
	Visibility      string    `gorm:"column:visibility;not null" json:"visibility"`
	CreatedAt       time.Time `gorm:"column:created_at" json:"created_at"`
}

func (MediaAsset) TableName() string { return "media_assets" }

// ImageGenerationJob records prompt preparation and upstream image-generation state.
type ImageGenerationJob struct {
	ID           int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	WorkID       int64     `gorm:"column:work_id;not null" json:"work_id"`
	APIKeyID     int64     `gorm:"column:api_key_id;not null" json:"api_key_id"`
	Status       string    `gorm:"column:status;not null" json:"status"`
	Prompt       string    `gorm:"column:prompt;not null" json:"prompt"`
	Style        string    `gorm:"column:style" json:"style,omitempty"`
	Size         string    `gorm:"column:size" json:"size,omitempty"`
	Model        string    `gorm:"column:model" json:"model,omitempty"`
	Quality      string    `gorm:"column:quality" json:"quality,omitempty"`
	OutputFormat string    `gorm:"column:output_format" json:"output_format,omitempty"`
	ErrorMessage string    `gorm:"column:error_message" json:"error_message,omitempty"`
	MediaAssetID *int64    `gorm:"column:media_asset_id" json:"media_asset_id,omitempty"`
	CreatedAt    time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (ImageGenerationJob) TableName() string { return "image_generation_jobs" }

// ImagePrompt stores reusable prompts prepared from work content.
type ImagePrompt struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	WorkID    int64     `gorm:"column:work_id;not null" json:"work_id"`
	APIKeyID  int64     `gorm:"column:api_key_id;not null" json:"api_key_id"`
	Prompt    string    `gorm:"column:prompt;not null" json:"prompt"`
	Source    string    `gorm:"column:source;not null" json:"source"`
	Style     string    `gorm:"column:style" json:"style,omitempty"`
	Size      string    `gorm:"column:size" json:"size,omitempty"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`
}

func (ImagePrompt) TableName() string { return "image_prompts" }

type CreateImagePromptParams struct {
	WorkID   int64
	APIKeyID int64
	Prompt   string
	Source   string
	Style    string
	Size     string
}

type CreateImageGenerationJobParams struct {
	WorkID       int64
	APIKeyID     int64
	Status       string
	Prompt       string
	Style        string
	Size         string
	Model        string
	Quality      string
	OutputFormat string
}

type CreateMediaAssetParams struct {
	WorkID          int64
	APIKeyID        int64
	AssetType       string
	Source          string
	URL             string
	B64JSON         string
	MimeType        string
	Model           string
	Size            string
	Quality         string
	OutputFormat    string
	Prompt          string
	RevisedPrompt   string
	StorageProvider string
	StorageKey      string
	FilePath        string
	ByteSize        int64
	ChecksumSHA256  string
	CreditCost      int
	Visibility      string
}

type FindCachedWorkImageAssetParams struct {
	Model        string
	Size         string
	Quality      string
	OutputFormat string
	Prompt       string
}

func (r *Repository) CreateImagePrompt(params CreateImagePromptParams) (*ImagePrompt, error) {
	if _, err := r.GetOriginalWork(params.APIKeyID, params.WorkID); err != nil {
		return nil, err
	}
	prompt := limitString(strings.TrimSpace(params.Prompt), 3000)
	if prompt == "" {
		return nil, fmt.Errorf("%w: prompt is required", ErrInvalidQueryParam)
	}
	source := strings.TrimSpace(params.Source)
	if source == "" {
		source = "work"
	}
	item := ImagePrompt{
		WorkID:   params.WorkID,
		APIKeyID: params.APIKeyID,
		Prompt:   prompt,
		Source:   limitString(source, 40),
		Style:    limitString(strings.TrimSpace(params.Style), 80),
		Size:     limitString(strings.TrimSpace(params.Size), 40),
	}
	if err := r.db.Create(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) CreateImageGenerationJob(params CreateImageGenerationJobParams) (*ImageGenerationJob, error) {
	if _, err := r.GetOriginalWork(params.APIKeyID, params.WorkID); err != nil {
		return nil, err
	}
	prompt := limitString(strings.TrimSpace(params.Prompt), 3000)
	if prompt == "" {
		return nil, fmt.Errorf("%w: prompt is required", ErrInvalidQueryParam)
	}
	status := normalizeImageJobStatus(params.Status)
	job := ImageGenerationJob{
		WorkID:       params.WorkID,
		APIKeyID:     params.APIKeyID,
		Status:       status,
		Prompt:       prompt,
		Style:        limitString(strings.TrimSpace(params.Style), 80),
		Size:         limitString(strings.TrimSpace(params.Size), 40),
		Model:        limitString(strings.TrimSpace(params.Model), 80),
		Quality:      limitString(strings.TrimSpace(params.Quality), 40),
		OutputFormat: limitString(strings.TrimSpace(params.OutputFormat), 40),
	}
	if err := r.db.Create(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *Repository) CreateMediaAsset(params CreateMediaAssetParams) (*MediaAsset, error) {
	if _, err := r.GetOriginalWork(params.APIKeyID, params.WorkID); err != nil {
		return nil, err
	}
	assetType := strings.TrimSpace(params.AssetType)
	if assetType == "" {
		assetType = MediaAssetTypeImage
	}
	source := strings.TrimSpace(params.Source)
	if source == "" {
		source = MediaAssetSourceGenerated
	}
	visibility := normalizeWorkVisibility(params.Visibility)
	if visibility == "" {
		visibility = WorkVisibilityPrivate
	}
	asset := MediaAsset{
		WorkID:          params.WorkID,
		APIKeyID:        params.APIKeyID,
		AssetType:       limitString(assetType, 40),
		Source:          limitString(source, 40),
		URL:             strings.TrimSpace(params.URL),
		B64JSON:         strings.TrimSpace(params.B64JSON),
		MimeType:        limitString(strings.TrimSpace(params.MimeType), 80),
		Model:           limitString(strings.TrimSpace(params.Model), 80),
		Size:            limitString(strings.TrimSpace(params.Size), 40),
		Quality:         limitString(strings.TrimSpace(params.Quality), 40),
		OutputFormat:    limitString(strings.TrimSpace(params.OutputFormat), 40),
		Prompt:          limitString(strings.TrimSpace(params.Prompt), 3000),
		RevisedPrompt:   limitString(strings.TrimSpace(params.RevisedPrompt), 3000),
		StorageProvider: limitString(strings.TrimSpace(params.StorageProvider), 40),
		StorageKey:      limitString(strings.TrimSpace(params.StorageKey), 500),
		FilePath:        limitString(strings.TrimSpace(params.FilePath), 1000),
		ByteSize:        params.ByteSize,
		ChecksumSHA256:  limitString(strings.TrimSpace(params.ChecksumSHA256), 80),
		CreditCost:      max(params.CreditCost, 0),
		Visibility:      visibility,
	}
	if asset.URL == "" && asset.B64JSON == "" {
		return nil, fmt.Errorf("%w: media asset url or b64_json is required", ErrInvalidQueryParam)
	}
	if err := r.db.Create(&asset).Error; err != nil {
		return nil, err
	}
	return &asset, nil
}

func (r *Repository) FindCachedWorkImageAsset(apiKeyID, workID int64, params FindCachedWorkImageAssetParams) (*MediaAsset, error) {
	if _, err := r.GetOriginalWork(apiKeyID, workID); err != nil {
		return nil, err
	}
	prompt := limitString(strings.TrimSpace(params.Prompt), 3000)
	if prompt == "" {
		return nil, fmt.Errorf("%w: prompt is required", ErrInvalidQueryParam)
	}
	var asset MediaAsset
	err := r.db.Where(
		`work_id = ? AND api_key_id = ? AND asset_type = ? AND source = ? AND model = ? AND size = ? AND quality = ? AND output_format = ? AND prompt = ? AND (url <> '' OR b64_json <> '')`,
		workID,
		apiKeyID,
		MediaAssetTypeImage,
		MediaAssetSourceGenerated,
		limitString(strings.TrimSpace(params.Model), 80),
		limitString(strings.TrimSpace(params.Size), 40),
		limitString(strings.TrimSpace(params.Quality), 40),
		limitString(strings.TrimSpace(params.OutputFormat), 40),
		prompt,
	).
		Order("created_at DESC, id DESC").
		First(&asset).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &asset, nil
}

func (r *Repository) CompleteImageGenerationJob(jobID int64, mediaAssetID *int64) (*ImageGenerationJob, error) {
	if jobID < 1 {
		return nil, ErrInvalidQueryParam
	}
	updates := map[string]any{
		"status":        ImageJobStatusSucceeded,
		"error_message": "",
		"updated_at":    time.Now().UTC(),
	}
	if mediaAssetID != nil {
		updates["media_asset_id"] = *mediaAssetID
	}
	return r.updateImageGenerationJob(jobID, updates)
}

func (r *Repository) MarkImageGenerationJobPromptReady(jobID int64) (*ImageGenerationJob, error) {
	if jobID < 1 {
		return nil, ErrInvalidQueryParam
	}
	return r.updateImageGenerationJob(jobID, map[string]any{
		"status":     ImageJobStatusPromptReady,
		"updated_at": time.Now().UTC(),
	})
}

func (r *Repository) FailImageGenerationJob(jobID int64, message string) (*ImageGenerationJob, error) {
	if jobID < 1 {
		return nil, ErrInvalidQueryParam
	}
	return r.updateImageGenerationJob(jobID, map[string]any{
		"status":        ImageJobStatusFailed,
		"error_message": limitString(strings.TrimSpace(message), 500),
		"updated_at":    time.Now().UTC(),
	})
}

func (r *Repository) updateImageGenerationJob(jobID int64, updates map[string]any) (*ImageGenerationJob, error) {
	result := r.db.Model(&ImageGenerationJob{}).Where("id = ?", jobID).Updates(updates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var job ImageGenerationJob
	if err := r.db.Where("id = ?", jobID).First(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *Repository) ListWorkMediaAssets(apiKeyID, workID int64, assetType string, limit int) ([]MediaAsset, error) {
	if _, err := r.GetOriginalWork(apiKeyID, workID); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	q := r.db.Where("work_id = ? AND api_key_id = ?", workID, apiKeyID).Order("created_at DESC, id DESC").Limit(limit)
	if assetType = strings.TrimSpace(assetType); assetType != "" && assetType != "all" {
		q = q.Where("asset_type = ?", assetType)
	}
	var items []MediaAsset
	if err := q.Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) ListWorkImageJobs(apiKeyID, workID int64, limit int) ([]ImageGenerationJob, error) {
	if _, err := r.GetOriginalWork(apiKeyID, workID); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	var items []ImageGenerationJob
	if err := r.db.Where("work_id = ? AND api_key_id = ?", workID, apiKeyID).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func normalizeImageJobStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case ImageJobStatusPromptReady:
		return ImageJobStatusPromptReady
	case ImageJobStatusSucceeded:
		return ImageJobStatusSucceeded
	case ImageJobStatusFailed:
		return ImageJobStatusFailed
	default:
		return ImageJobStatusPending
	}
}
