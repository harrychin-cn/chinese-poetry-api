package database

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	WorkStatusDraft          = "draft"
	WorkStatusPublished      = "published"
	WorkStatusReviewRequired = "review_required"

	WorkVisibilityPrivate = "private"
	WorkVisibilityPublic  = "public"

	DefaultWorkLicenseType    = "cc0-like"
	DefaultWorkLicenseVersion = "v0.1"
)

// OriginalWork is a user-created poem/ci/qu/fu record.
type OriginalWork struct {
	ID                 int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	WorkCode           string     `gorm:"column:work_code" json:"work_code"`
	APIKeyID           int64      `gorm:"column:api_key_id;not null" json:"api_key_id"`
	Title              string     `gorm:"column:title;not null" json:"title"`
	WorkType           string     `gorm:"column:work_type;not null" json:"work_type"`
	Content            string     `gorm:"column:content;not null" json:"content"`
	ContentHash        string     `gorm:"column:content_hash;not null" json:"content_hash"`
	Description        string     `gorm:"column:description" json:"description,omitempty"`
	Visibility         string     `gorm:"column:visibility;not null" json:"visibility"`
	Status             string     `gorm:"column:status;not null" json:"status"`
	LicenseType        string     `gorm:"column:license_type;not null" json:"license_type"`
	LicenseVersion     string     `gorm:"column:license_version;not null" json:"license_version"`
	OriginalCommitment bool       `gorm:"column:original_commitment;not null" json:"original_commitment"`
	LicenseAccepted    bool       `gorm:"column:license_accepted;not null" json:"license_accepted"`
	PlagiarismStatus   string     `gorm:"column:plagiarism_status;not null" json:"plagiarism_status"`
	ImagePrompt        string     `gorm:"column:image_prompt" json:"image_prompt,omitempty"`
	Version            int        `gorm:"column:version;not null" json:"version"`
	PublishedAt        *time.Time `gorm:"column:published_at" json:"published_at,omitempty"`
	CreatedAt          time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at" json:"updated_at"`
}

func (OriginalWork) TableName() string { return "original_works" }

// OriginalWorkVersion records every saved content version.
type OriginalWorkVersion struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	WorkID      int64     `gorm:"column:work_id;not null" json:"work_id"`
	Version     int       `gorm:"column:version;not null" json:"version"`
	Title       string    `gorm:"column:title;not null" json:"title"`
	Content     string    `gorm:"column:content;not null" json:"content"`
	ContentHash string    `gorm:"column:content_hash;not null" json:"content_hash"`
	ChangeNote  string    `gorm:"column:change_note" json:"change_note,omitempty"`
	CreatedAt   time.Time `gorm:"column:created_at" json:"created_at"`
}

func (OriginalWorkVersion) TableName() string { return "original_work_versions" }

// WorkLicenseAcceptance records the author commitment and open-license acceptance.
type WorkLicenseAcceptance struct {
	ID                 int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	WorkID             int64     `gorm:"column:work_id;not null" json:"work_id"`
	APIKeyID           int64     `gorm:"column:api_key_id;not null" json:"api_key_id"`
	LicenseType        string    `gorm:"column:license_type;not null" json:"license_type"`
	LicenseVersion     string    `gorm:"column:license_version;not null" json:"license_version"`
	OriginalCommitment bool      `gorm:"column:original_commitment;not null" json:"original_commitment"`
	LicenseAccepted    bool      `gorm:"column:license_accepted;not null" json:"license_accepted"`
	AcceptanceText     string    `gorm:"column:acceptance_text" json:"acceptance_text,omitempty"`
	AcceptedAt         time.Time `gorm:"column:accepted_at" json:"accepted_at"`
}

func (WorkLicenseAcceptance) TableName() string { return "work_license_acceptances" }

// WorkPublicationEvent records publish/unpublish transitions.
type WorkPublicationEvent struct {
	ID         int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	WorkID     int64     `gorm:"column:work_id;not null" json:"work_id"`
	APIKeyID   int64     `gorm:"column:api_key_id;not null" json:"api_key_id"`
	FromStatus string    `gorm:"column:from_status" json:"from_status,omitempty"`
	ToStatus   string    `gorm:"column:to_status;not null" json:"to_status"`
	Visibility string    `gorm:"column:visibility;not null" json:"visibility"`
	EventType  string    `gorm:"column:event_type;not null" json:"event_type"`
	CreatedAt  time.Time `gorm:"column:created_at" json:"created_at"`
}

func (WorkPublicationEvent) TableName() string { return "work_publication_events" }

type CreateOriginalWorkParams struct {
	APIKeyID           int64
	Title              string
	WorkType           string
	Content            string
	Description        string
	Visibility         string
	LicenseType        string
	LicenseVersion     string
	OriginalCommitment bool
	LicenseAccepted    bool
	ImagePrompt        string
	Publish            bool
	ChangeNote         string
}

type UpdateOriginalWorkParams struct {
	Title              *string
	WorkType           *string
	Content            *string
	Description        *string
	Visibility         *string
	LicenseType        *string
	LicenseVersion     *string
	OriginalCommitment *bool
	LicenseAccepted    *bool
	ImagePrompt        *string
	Publish            *bool
	ChangeNote         string
}

// CreateOriginalWork creates a draft or published original work and records version/license evidence.
func (r *Repository) CreateOriginalWork(params CreateOriginalWorkParams) (*OriginalWork, error) {
	work, err := buildOriginalWork(params)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	work.CreatedAt = now
	work.UpdatedAt = now

	if params.Publish && (!work.OriginalCommitment || !work.LicenseAccepted) {
		return nil, fmt.Errorf("%w: publishing requires original_commitment and license_accepted", ErrInvalidQueryParam)
	}

	err = r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(work).Error; err != nil {
			return err
		}
		work.WorkCode = formatWorkCode(now, work.ID)
		if err := tx.Model(&OriginalWork{}).Where("id = ?", work.ID).Update("work_code", work.WorkCode).Error; err != nil {
			return err
		}
		if err := createWorkVersion(tx, work, params.ChangeNote); err != nil {
			return err
		}
		if work.OriginalCommitment || work.LicenseAccepted {
			if err := createWorkLicenseAcceptance(tx, work); err != nil {
				return err
			}
		}

		if params.Publish {
			check, err := r.runPlagiarismCheckTx(tx, work)
			if err != nil {
				return err
			}
			if plagiarismBlocksPublish(check.report.RiskLevel) {
				work.Status = WorkStatusReviewRequired
				work.Visibility = WorkVisibilityPrivate
				work.PublishedAt = nil
				if err := tx.Save(work).Error; err != nil {
					return err
				}
				if err := createWorkPublicationEvent(tx, work.ID, work.APIKeyID, WorkStatusDraft, WorkStatusReviewRequired, work.Visibility, "review_required"); err != nil {
					return err
				}
				return nil
			}

			work.Status = WorkStatusPublished
			work.Visibility = WorkVisibilityPublic
			work.PublishedAt = &now
			if err := tx.Save(work).Error; err != nil {
				return err
			}
			if err := createWorkPublicationEvent(tx, work.ID, work.APIKeyID, WorkStatusDraft, WorkStatusPublished, work.Visibility, "publish"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return work, nil
}

// UpdateOriginalWork updates an owned work and writes a new version when title/content changes.
func (r *Repository) UpdateOriginalWork(apiKeyID, id int64, params UpdateOriginalWorkParams) (*OriginalWork, error) {
	if apiKeyID < 1 || id < 1 {
		return nil, ErrInvalidQueryParam
	}

	var updated OriginalWork
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var current OriginalWork
		if err := tx.Where("id = ? AND api_key_id = ?", id, apiKeyID).First(&current).Error; err != nil {
			return err
		}

		beforeStatus := current.Status
		titleChanged := false
		contentChanged := false
		wantsPublish := params.Publish != nil && *params.Publish

		if params.Title != nil {
			title := normalizeWorkTitle(*params.Title)
			if title == "" {
				return fmt.Errorf("%w: title is required", ErrInvalidQueryParam)
			}
			titleChanged = title != current.Title
			current.Title = title
		}
		if params.WorkType != nil {
			workType := normalizeWorkType(*params.WorkType)
			if workType == "" {
				return fmt.Errorf("%w: unsupported work_type", ErrInvalidQueryParam)
			}
			current.WorkType = workType
		}
		if params.Content != nil {
			content := normalizeWorkContent(*params.Content)
			if content == "" {
				return fmt.Errorf("%w: content is required", ErrInvalidQueryParam)
			}
			hash := hashWorkContent(content)
			contentChanged = hash != current.ContentHash
			current.Content = content
			current.ContentHash = hash
		}
		if params.Description != nil {
			current.Description = limitString(strings.TrimSpace(*params.Description), 1000)
		}
		if params.Visibility != nil {
			visibility := normalizeWorkVisibility(*params.Visibility)
			if visibility == "" {
				return fmt.Errorf("%w: unsupported visibility", ErrInvalidQueryParam)
			}
			current.Visibility = visibility
		}
		if params.LicenseType != nil {
			current.LicenseType = normalizeLicenseType(*params.LicenseType)
		}
		if params.LicenseVersion != nil {
			current.LicenseVersion = normalizeLicenseVersion(*params.LicenseVersion)
		}
		if params.OriginalCommitment != nil {
			current.OriginalCommitment = *params.OriginalCommitment
		}
		if params.LicenseAccepted != nil {
			current.LicenseAccepted = *params.LicenseAccepted
		}
		if params.ImagePrompt != nil {
			current.ImagePrompt = limitString(strings.TrimSpace(*params.ImagePrompt), 3000)
		}
		if wantsPublish {
			if !current.OriginalCommitment || !current.LicenseAccepted {
				return fmt.Errorf("%w: publishing requires original_commitment and license_accepted", ErrInvalidQueryParam)
			}
		}

		if titleChanged || contentChanged {
			current.Version++
			current.PlagiarismStatus = PlagiarismStatusPending
		}
		current.UpdatedAt = time.Now().UTC()

		needsPlagiarismCheck := wantsPublish || (contentChanged && beforeStatus == WorkStatusPublished)
		if needsPlagiarismCheck {
			check, err := r.runPlagiarismCheckTx(tx, &current)
			if err != nil {
				return err
			}
			if plagiarismBlocksPublish(check.report.RiskLevel) {
				current.Status = WorkStatusReviewRequired
				current.Visibility = WorkVisibilityPrivate
				current.PublishedAt = nil
			} else if wantsPublish {
				now := time.Now().UTC()
				current.Status = WorkStatusPublished
				current.Visibility = WorkVisibilityPublic
				if current.PublishedAt == nil {
					current.PublishedAt = &now
				}
			}
		}

		if err := tx.Save(&current).Error; err != nil {
			return err
		}
		if titleChanged || contentChanged {
			if err := createWorkVersion(tx, &current, params.ChangeNote); err != nil {
				return err
			}
		}
		if (params.OriginalCommitment != nil || params.LicenseAccepted != nil || params.LicenseType != nil || params.LicenseVersion != nil) &&
			(current.OriginalCommitment || current.LicenseAccepted) {
			if err := createWorkLicenseAcceptance(tx, &current); err != nil {
				return err
			}
		}
		if beforeStatus != current.Status {
			eventType := "publish"
			if current.Status == WorkStatusReviewRequired {
				eventType = "review_required"
			}
			if err := createWorkPublicationEvent(tx, current.ID, current.APIKeyID, beforeStatus, current.Status, current.Visibility, eventType); err != nil {
				return err
			}
		}

		updated = current
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (r *Repository) PublishOriginalWork(apiKeyID, id int64) (*OriginalWork, error) {
	yes := true
	return r.UpdateOriginalWork(apiKeyID, id, UpdateOriginalWorkParams{Publish: &yes})
}

func (r *Repository) ListOriginalWorks(apiKeyID int64, status string, limit int) ([]OriginalWork, error) {
	if apiKeyID < 1 {
		return nil, ErrInvalidQueryParam
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	q := r.db.Where("api_key_id = ?", apiKeyID).Order("updated_at DESC, id DESC").Limit(limit)
	if status = strings.TrimSpace(status); status != "" && status != "all" {
		q = q.Where("status = ?", status)
	}
	var works []OriginalWork
	if err := q.Find(&works).Error; err != nil {
		return nil, err
	}
	return works, nil
}

func (r *Repository) GetOriginalWork(apiKeyID, id int64) (*OriginalWork, error) {
	if apiKeyID < 1 || id < 1 {
		return nil, ErrInvalidQueryParam
	}
	var work OriginalWork
	if err := r.db.Where("id = ? AND api_key_id = ?", id, apiKeyID).First(&work).Error; err != nil {
		return nil, err
	}
	return &work, nil
}

func (r *Repository) GetPublicOriginalWorkByCode(code string) (*OriginalWork, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil, ErrInvalidQueryParam
	}
	var work OriginalWork
	if err := r.db.Where("work_code = ? AND status = ? AND visibility = ?", code, WorkStatusPublished, WorkVisibilityPublic).First(&work).Error; err != nil {
		return nil, err
	}
	return &work, nil
}

func (r *Repository) ListOriginalWorkVersions(apiKeyID, workID int64) ([]OriginalWorkVersion, error) {
	if _, err := r.GetOriginalWork(apiKeyID, workID); err != nil {
		return nil, err
	}
	var versions []OriginalWorkVersion
	if err := r.db.Where("work_id = ?", workID).Order("version DESC").Find(&versions).Error; err != nil {
		return nil, err
	}
	return versions, nil
}

func (r *Repository) ListWorkLicenseAcceptances(apiKeyID, workID int64) ([]WorkLicenseAcceptance, error) {
	if _, err := r.GetOriginalWork(apiKeyID, workID); err != nil {
		return nil, err
	}
	var records []WorkLicenseAcceptance
	if err := r.db.Where("work_id = ?", workID).Order("accepted_at DESC").Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

func buildOriginalWork(params CreateOriginalWorkParams) (*OriginalWork, error) {
	if params.APIKeyID < 1 {
		return nil, ErrInvalidQueryParam
	}
	title := normalizeWorkTitle(params.Title)
	if title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrInvalidQueryParam)
	}
	content := normalizeWorkContent(params.Content)
	if content == "" {
		return nil, fmt.Errorf("%w: content is required", ErrInvalidQueryParam)
	}
	workType := normalizeWorkType(params.WorkType)
	if workType == "" {
		return nil, fmt.Errorf("%w: unsupported work_type", ErrInvalidQueryParam)
	}
	visibility := normalizeWorkVisibility(params.Visibility)
	if visibility == "" {
		return nil, fmt.Errorf("%w: unsupported visibility", ErrInvalidQueryParam)
	}

	return &OriginalWork{
		APIKeyID:           params.APIKeyID,
		Title:              title,
		WorkType:           workType,
		Content:            content,
		ContentHash:        hashWorkContent(content),
		Description:        limitString(strings.TrimSpace(params.Description), 1000),
		Visibility:         visibility,
		Status:             WorkStatusDraft,
		LicenseType:        normalizeLicenseType(params.LicenseType),
		LicenseVersion:     normalizeLicenseVersion(params.LicenseVersion),
		OriginalCommitment: params.OriginalCommitment,
		LicenseAccepted:    params.LicenseAccepted,
		PlagiarismStatus:   "pending",
		ImagePrompt:        limitString(strings.TrimSpace(params.ImagePrompt), 3000),
		Version:            1,
	}, nil
}

func createWorkVersion(tx *gorm.DB, work *OriginalWork, changeNote string) error {
	version := OriginalWorkVersion{
		WorkID:      work.ID,
		Version:     work.Version,
		Title:       work.Title,
		Content:     work.Content,
		ContentHash: work.ContentHash,
		ChangeNote:  limitString(strings.TrimSpace(changeNote), 500),
	}
	return tx.Create(&version).Error
}

func createWorkLicenseAcceptance(tx *gorm.DB, work *OriginalWork) error {
	record := WorkLicenseAcceptance{
		WorkID:             work.ID,
		APIKeyID:           work.APIKeyID,
		LicenseType:        work.LicenseType,
		LicenseVersion:     work.LicenseVersion,
		OriginalCommitment: work.OriginalCommitment,
		LicenseAccepted:    work.LicenseAccepted,
		AcceptanceText:     "I confirm this is my original work or I have legal authorization, and I accept the platform open-license terms.",
	}
	return tx.Create(&record).Error
}

func createWorkPublicationEvent(tx *gorm.DB, workID, apiKeyID int64, fromStatus, toStatus, visibility, eventType string) error {
	event := WorkPublicationEvent{
		WorkID:     workID,
		APIKeyID:   apiKeyID,
		FromStatus: fromStatus,
		ToStatus:   toStatus,
		Visibility: visibility,
		EventType:  eventType,
	}
	return tx.Create(&event).Error
}

func normalizeWorkTitle(value string) string {
	return limitString(strings.TrimSpace(value), 120)
}

func normalizeWorkContent(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return limitString(strings.TrimSpace(value), 10000)
}

func normalizeWorkType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "poem":
		return "poem"
	case "ci", "qu", "fu", "modern_poem", "lyric":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func normalizeWorkVisibility(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", WorkVisibilityPrivate:
		return WorkVisibilityPrivate
	case WorkVisibilityPublic:
		return WorkVisibilityPublic
	default:
		return ""
	}
}

func normalizeLicenseType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return DefaultWorkLicenseType
	}
	return limitString(value, 80)
}

func normalizeLicenseVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return DefaultWorkLicenseVersion
	}
	return limitString(value, 40)
}

func hashWorkContent(content string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(content)))
	return hex.EncodeToString(sum[:])
}

func formatWorkCode(t time.Time, id int64) string {
	if t.IsZero() {
		t = time.Now().UTC()
	}
	return fmt.Sprintf("PCQF-%04d-%08d", t.Year(), id)
}

func IsOriginalWorkNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
