package database

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	PlagiarismCorpusTypeNetwork   = "network_corpus"
	PlagiarismCorpusTypeDispute   = "dispute_case"
	PlagiarismCorpusTypeReference = "reference_work"

	PlagiarismCorpusStatusEnabled  = "enabled"
	PlagiarismCorpusStatusDisabled = "disabled"

	ManualReviewStatusPending  = "pending"
	ManualReviewStatusApproved = "approved"
	ManualReviewStatusRejected = "rejected"
)

// PlagiarismCorpusSource stores an operator-curated external source for originality checks.
type PlagiarismCorpusSource struct {
	ID             int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	SourceType     string    `gorm:"column:source_type;not null" json:"source_type"`
	Title          string    `gorm:"column:title;not null" json:"title"`
	Author         string    `gorm:"column:author" json:"author,omitempty"`
	SourceURL      string    `gorm:"column:source_url" json:"source_url,omitempty"`
	Content        string    `gorm:"column:content;not null" json:"content"`
	NormalizedHash string    `gorm:"column:normalized_hash;not null" json:"normalized_hash"`
	EmbeddingJSON  string    `gorm:"column:embedding_json" json:"embedding_json,omitempty"`
	Status         string    `gorm:"column:status;not null" json:"status"`
	Notes          string    `gorm:"column:notes" json:"notes,omitempty"`
	CreatedBy      string    `gorm:"column:created_by" json:"created_by,omitempty"`
	CreatedAt      time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (PlagiarismCorpusSource) TableName() string { return "plagiarism_corpus_sources" }

type CreatePlagiarismCorpusSourceParams struct {
	SourceType string
	Title      string
	Author     string
	SourceURL  string
	Content    string
	Status     string
	Notes      string
	CreatedBy  string
}

type ListPlagiarismCorpusSourcesParams struct {
	SourceType string
	Status     string
	Limit      int
}

type ManualReviewDecisionParams struct {
	Reviewer string
	Notes    string
}

type ManualReviewQueueEntry struct {
	Queue  ManualReviewQueueItem `json:"queue"`
	Work   OriginalWork          `json:"work"`
	Report PlagiarismReport      `json:"report"`
}

func (r *Repository) CreatePlagiarismCorpusSource(params CreatePlagiarismCorpusSourceParams) (*PlagiarismCorpusSource, error) {
	title := limitString(strings.TrimSpace(params.Title), 160)
	content := normalizeWorkContent(params.Content)
	if title == "" || content == "" {
		return nil, ErrInvalidQueryParam
	}

	normalized := normalizePlagiarismText(content)
	if normalized == "" {
		return nil, fmt.Errorf("%w: corpus content is empty after normalization", ErrInvalidQueryParam)
	}

	now := time.Now().UTC()
	source := PlagiarismCorpusSource{
		SourceType:     normalizePlagiarismCorpusType(params.SourceType),
		Title:          title,
		Author:         limitString(strings.TrimSpace(params.Author), 120),
		SourceURL:      limitString(strings.TrimSpace(params.SourceURL), 500),
		Content:        content,
		NormalizedHash: hashNormalizedText(normalized),
		EmbeddingJSON:  marshalFloatSlice(plagiarismEmbedding(normalized)),
		Status:         normalizePlagiarismCorpusStatus(params.Status),
		Notes:          limitString(strings.TrimSpace(params.Notes), 1000),
		CreatedBy:      limitString(firstNonEmptyString(params.CreatedBy, "operator"), 120),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := r.db.Create(&source).Error; err != nil {
		return nil, err
	}
	return &source, nil
}

func (r *Repository) ListPlagiarismCorpusSources(params ListPlagiarismCorpusSourcesParams) ([]PlagiarismCorpusSource, error) {
	limit := params.Limit
	if limit < 1 || limit > 200 {
		limit = 50
	}
	q := r.db.Model(&PlagiarismCorpusSource{}).Order("updated_at DESC, id DESC").Limit(limit)
	if status := strings.TrimSpace(params.Status); status != "" && status != "all" {
		q = q.Where("status = ?", normalizePlagiarismCorpusStatus(status))
	}
	if sourceType := strings.TrimSpace(params.SourceType); sourceType != "" && sourceType != "all" {
		q = q.Where("source_type = ?", normalizePlagiarismCorpusType(sourceType))
	}

	var sources []PlagiarismCorpusSource
	if err := q.Find(&sources).Error; err != nil {
		return nil, err
	}
	return sources, nil
}

func (r *Repository) ListManualReviewQueue(status string, limit int) ([]ManualReviewQueueEntry, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	q := r.db.Model(&ManualReviewQueueItem{}).Order("created_at DESC, id DESC").Limit(limit)
	if status = strings.TrimSpace(status); status != "" && status != "all" {
		q = q.Where("status = ?", status)
	}

	var items []ManualReviewQueueItem
	if err := q.Find(&items).Error; err != nil {
		return nil, err
	}

	entries := make([]ManualReviewQueueEntry, 0, len(items))
	for _, item := range items {
		var work OriginalWork
		if err := r.db.Where("id = ?", item.WorkID).First(&work).Error; err != nil {
			return nil, err
		}
		var report PlagiarismReport
		if err := r.db.Where("id = ?", item.ReportID).First(&report).Error; err != nil {
			return nil, err
		}
		entries = append(entries, ManualReviewQueueEntry{Queue: item, Work: work, Report: report})
	}
	return entries, nil
}

func (r *Repository) DecideManualReviewQueue(id int64, approve bool, params ManualReviewDecisionParams) (*ManualReviewQueueEntry, error) {
	if id < 1 {
		return nil, ErrInvalidQueryParam
	}

	now := time.Now().UTC()
	reviewer := limitString(firstNonEmptyString(params.Reviewer, "operator"), 120)
	notes := limitString(strings.TrimSpace(params.Notes), 1000)

	var entry ManualReviewQueueEntry
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var item ManualReviewQueueItem
		if err := tx.Where("id = ?", id).First(&item).Error; err != nil {
			return err
		}

		var report PlagiarismReport
		if err := tx.Where("id = ?", item.ReportID).First(&report).Error; err != nil {
			return err
		}

		var work OriginalWork
		if err := tx.Where("id = ?", item.WorkID).First(&work).Error; err != nil {
			return err
		}

		beforeStatus := work.Status
		if approve {
			item.Status = ManualReviewStatusApproved
			report.ReviewStatus = "manual_approved"
			work.Status = WorkStatusPublished
			work.Visibility = WorkVisibilityPublic
			work.PlagiarismStatus = PlagiarismStatusPassed
			if work.PublishedAt == nil {
				work.PublishedAt = &now
			}
		} else {
			item.Status = ManualReviewStatusRejected
			report.ReviewStatus = "manual_rejected"
			work.Status = WorkStatusReviewRequired
			work.Visibility = WorkVisibilityPrivate
			work.PlagiarismStatus = plagiarismStatusFromRisk(report.RiskLevel)
			work.PublishedAt = nil
		}

		item.Reviewer = reviewer
		item.ReviewNotes = notes
		item.DecidedAt = &now
		item.UpdatedAt = now
		work.UpdatedAt = now

		if err := tx.Save(&item).Error; err != nil {
			return err
		}
		if err := tx.Save(&report).Error; err != nil {
			return err
		}
		if err := tx.Save(&work).Error; err != nil {
			return err
		}
		if beforeStatus != work.Status {
			eventType := "manual_reject"
			if approve {
				eventType = "manual_approve"
			}
			if err := createWorkPublicationEvent(tx, work.ID, work.APIKeyID, beforeStatus, work.Status, work.Visibility, eventType); err != nil {
				return err
			}
		}

		entry = ManualReviewQueueEntry{Queue: item, Work: work, Report: report}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

func normalizePlagiarismCorpusType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", PlagiarismCorpusTypeNetwork:
		return PlagiarismCorpusTypeNetwork
	case PlagiarismCorpusTypeDispute:
		return PlagiarismCorpusTypeDispute
	case PlagiarismCorpusTypeReference:
		return PlagiarismCorpusTypeReference
	default:
		return PlagiarismCorpusTypeNetwork
	}
}

func normalizePlagiarismCorpusStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case PlagiarismCorpusStatusDisabled:
		return PlagiarismCorpusStatusDisabled
	default:
		return PlagiarismCorpusStatusEnabled
	}
}
