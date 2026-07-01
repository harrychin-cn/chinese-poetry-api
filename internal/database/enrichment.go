package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	EnrichmentStatusPending    = "pending"
	EnrichmentStatusAccepted   = "accepted"
	EnrichmentStatusRejected   = "rejected"
	EnrichmentStatusRolledBack = "rolled_back"
	EnrichmentStatusDraft      = "draft"
)

// PoemKnowledge stores AI/manual enriched explanation fields for one poem.
type PoemKnowledge struct {
	ID             int64     `json:"id"`
	PoemID         int64     `json:"poem_id"`
	Summary        string    `json:"summary"`
	Translation    string    `json:"translation"`
	Annotation     string    `json:"annotation"`
	Recommendation string    `json:"recommendation"`
	QualityStatus  string    `json:"quality_status"`
	Source         string    `json:"source"`
	Reviewer       string    `json:"reviewer,omitempty"`
	ReviewNotes    string    `json:"review_notes,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// PoemEmbedding stores a future vector-search row. The vector is kept as JSON
// for the SQLite MVP; a later production edition can migrate it to sqlite-vec or
// an external vector database without changing the public knowledge API.
type PoemEmbedding struct {
	ID          int64     `json:"id"`
	PoemID      int64     `json:"poem_id"`
	Provider    string    `json:"provider"`
	Model       string    `json:"model"`
	Dimension   int       `json:"dimension"`
	VectorJSON  string    `json:"-"`
	ContentHash string    `json:"content_hash"`
	CreatedAt   time.Time `json:"created_at"`
}

// EnrichmentJob tracks a batch AI data-enrichment run.
type EnrichmentJob struct {
	ID             int64      `json:"id"`
	Status         string     `json:"status"`
	Scope          string     `json:"scope"`
	TotalCount     int        `json:"total_count"`
	ProcessedCount int        `json:"processed_count"`
	AcceptedCount  int        `json:"accepted_count"`
	RejectedCount  int        `json:"rejected_count"`
	ErrorCount     int        `json:"error_count"`
	ConfigJSON     string     `json:"config_json,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// EnrichmentReviewItem is one pending/accepted/rejected enrichment candidate.
type EnrichmentReviewItem struct {
	ID                    int64     `json:"id"`
	JobID                 *int64    `json:"job_id,omitempty"`
	PoemID                int64     `json:"poem_id"`
	Status                string    `json:"status"`
	ProposedTagsJSON      string    `json:"-"`
	ProposedKnowledgeJSON string    `json:"-"`
	AppliedTagIDsJSON     string    `json:"-"`
	PreviousKnowledgeJSON string    `json:"-"`
	Reviewer              string    `json:"reviewer,omitempty"`
	ReviewNotes           string    `json:"review_notes,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type CreateEnrichmentJobParams struct {
	Scope      string
	TotalCount int
	Config     map[string]any
}

type ProposedKnowledgeInput struct {
	Summary        string `json:"summary"`
	Translation    string `json:"translation"`
	Annotation     string `json:"annotation"`
	Recommendation string `json:"recommendation"`
	Source         string `json:"source"`
}

type CreateReviewItemParams struct {
	JobID             *int64
	PoemID            int64
	ProposedTags      []TagInput
	ProposedKnowledge ProposedKnowledgeInput
}

type ReviewDecisionParams struct {
	Reviewer string
	Notes    string
}

// EnrichmentRollbackResult reports how much accepted enrichment data was removed.
type EnrichmentRollbackResult struct {
	RunID         string `json:"run_id,omitempty"`
	JobID         *int64 `json:"job_id,omitempty"`
	PoemID        *int64 `json:"poem_id,omitempty"`
	ReviewItems   int    `json:"review_items"`
	PoemsAffected int    `json:"poems_affected"`
	TagsRemoved   int64  `json:"tags_removed"`
	KnowledgeRows int64  `json:"knowledge_rows_removed"`
	Reviewer      string `json:"reviewer,omitempty"`
	Notes         string `json:"notes,omitempty"`
}

// EnrichmentReviewNoteCount groups common manual review notes for operations reports.
type EnrichmentReviewNoteCount struct {
	Note  string `json:"note"`
	Count int    `json:"count"`
}

// EnrichmentRunSummary is an operations snapshot for one enrichment run.
type EnrichmentRunSummary struct {
	RunID             string                      `json:"run_id"`
	JobIDs            []int64                     `json:"job_ids"`
	TotalItems        int                         `json:"total_items"`
	PendingCount      int                         `json:"pending_count"`
	AcceptedCount     int                         `json:"accepted_count"`
	RejectedCount     int                         `json:"rejected_count"`
	RolledBackCount   int                         `json:"rolled_back_count"`
	ReviewedCount     int                         `json:"reviewed_count"`
	PassRate          float64                     `json:"pass_rate"`
	RejectedNoteTop10 []EnrichmentReviewNoteCount `json:"rejected_note_top10"`
}

// CreateEnrichmentJob creates a batch enrichment job record.
func (r *Repository) CreateEnrichmentJob(params CreateEnrichmentJobParams) (*EnrichmentJob, error) {
	scope := strings.TrimSpace(params.Scope)
	if scope == "" {
		scope = "sample"
	}
	if params.TotalCount < 0 {
		return nil, fmt.Errorf("%w: total_count cannot be negative", ErrInvalidQueryParam)
	}

	configJSON := ""
	if params.Config != nil {
		encoded, err := json.Marshal(params.Config)
		if err != nil {
			return nil, err
		}
		configJSON = string(encoded)
	}

	now := time.Now().UTC()
	job := &EnrichmentJob{
		Status:     "pending",
		Scope:      scope,
		TotalCount: params.TotalCount,
		ConfigJSON: configJSON,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := r.db.Table("enrichment_jobs").Create(job).Error; err != nil {
		return nil, err
	}
	return job, nil
}

// ListEnrichmentJobs lists recent jobs for the operation console.
func (r *Repository) ListEnrichmentJobs(limit int) ([]EnrichmentJob, error) {
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var jobs []EnrichmentJob
	err := r.db.Table("enrichment_jobs").Order("id DESC").Limit(limit).Find(&jobs).Error
	return jobs, err
}

// CreateEnrichmentReviewItem stores one AI-generated candidate for manual review.
func (r *Repository) CreateEnrichmentReviewItem(params CreateReviewItemParams) (*EnrichmentReviewItem, error) {
	if params.PoemID < 1 {
		return nil, fmt.Errorf("%w: poem_id must be positive", ErrInvalidQueryParam)
	}

	var poemExists int64
	if err := r.db.Table(r.poemsTable()).Where("id = ?", params.PoemID).Count(&poemExists).Error; err != nil {
		return nil, err
	}
	if poemExists == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	if params.JobID != nil {
		var jobExists int64
		if err := r.db.Table("enrichment_jobs").Where("id = ?", *params.JobID).Count(&jobExists).Error; err != nil {
			return nil, err
		}
		if jobExists == 0 {
			return nil, gorm.ErrRecordNotFound
		}
	}

	tagsJSON, err := json.Marshal(params.ProposedTags)
	if err != nil {
		return nil, err
	}
	knowledgeJSON, err := json.Marshal(params.ProposedKnowledge)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	item := &EnrichmentReviewItem{
		JobID:                 params.JobID,
		PoemID:                params.PoemID,
		Status:                EnrichmentStatusPending,
		ProposedTagsJSON:      string(tagsJSON),
		ProposedKnowledgeJSON: string(knowledgeJSON),
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if err := r.db.Table("enrichment_review_items").Create(item).Error; err != nil {
		return nil, err
	}
	return item, nil
}

// CorrectEnrichmentReviewItem updates an AI candidate after manual correction.
// Corrected candidates stay pending until the reviewer explicitly accepts them.
func (r *Repository) CorrectEnrichmentReviewItem(id int64, proposedTags []TagInput, proposedKnowledge ProposedKnowledgeInput, params ReviewDecisionParams) (*EnrichmentReviewItem, error) {
	if id < 1 {
		return nil, fmt.Errorf("%w: review item id must be positive", ErrInvalidQueryParam)
	}

	tagsJSON, err := json.Marshal(proposedTags)
	if err != nil {
		return nil, err
	}
	knowledgeJSON, err := json.Marshal(proposedKnowledge)
	if err != nil {
		return nil, err
	}

	var corrected EnrichmentReviewItem
	err = r.db.Transaction(func(tx *gorm.DB) error {
		txRepo := &Repository{db: &DB{DB: tx}, lang: r.lang}
		item, err := txRepo.getReviewItemForUpdate(id)
		if err != nil {
			return err
		}
		if item.Status != EnrichmentStatusPending {
			return fmt.Errorf("%w: only pending review items can be corrected", ErrInvalidQueryParam)
		}

		if err := tx.Table("enrichment_review_items").Where("id = ?", id).Updates(map[string]any{
			"proposed_tags_json":      string(tagsJSON),
			"proposed_knowledge_json": string(knowledgeJSON),
			"reviewer":                strings.TrimSpace(params.Reviewer),
			"review_notes":            strings.TrimSpace(params.Notes),
			"updated_at":              time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		updated, err := txRepo.getReviewItemForUpdate(id)
		if err != nil {
			return err
		}
		corrected = *updated
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &corrected, nil
}

// ListEnrichmentReviewItems lists review candidates by status.
func (r *Repository) ListEnrichmentReviewItems(status string, limit int) ([]EnrichmentReviewItem, error) {
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = EnrichmentStatusPending
	}

	var items []EnrichmentReviewItem
	err := r.db.Table("enrichment_review_items").Where("status = ?", status).Order("id ASC").Limit(limit).Find(&items).Error
	return items, err
}

// ListEnrichmentReviewItemsForRun lists review candidates for one run_id.
func (r *Repository) ListEnrichmentReviewItemsForRun(runID, status string, limit int) ([]EnrichmentReviewItem, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("%w: run_id is required", ErrInvalidQueryParam)
	}
	if limit < 1 {
		limit = 30
	}
	if limit > 1000 {
		limit = 1000
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = EnrichmentStatusPending
	}

	jobs, err := r.findEnrichmentJobsByRunID(runID)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	jobIDs := make([]int64, 0, len(jobs))
	for _, job := range jobs {
		jobIDs = append(jobIDs, job.ID)
	}

	var items []EnrichmentReviewItem
	err = r.db.Table("enrichment_review_items").
		Where("job_id IN ? AND status = ?", jobIDs, status).
		Order("id ASC").
		Limit(limit).
		Find(&items).Error
	return items, err
}

// GetEnrichmentReviewItem returns one review candidate by id.
func (r *Repository) GetEnrichmentReviewItem(id int64) (*EnrichmentReviewItem, error) {
	if id < 1 {
		return nil, fmt.Errorf("%w: review item id must be positive", ErrInvalidQueryParam)
	}
	return r.getReviewItemForUpdate(id)
}

// AcceptEnrichmentReviewItem applies one review candidate to tags/knowledge.
func (r *Repository) AcceptEnrichmentReviewItem(id int64, params ReviewDecisionParams) (*EnrichmentReviewItem, error) {
	if id < 1 {
		return nil, fmt.Errorf("%w: review item id must be positive", ErrInvalidQueryParam)
	}

	var accepted EnrichmentReviewItem
	err := r.db.Transaction(func(tx *gorm.DB) error {
		txRepo := &Repository{db: &DB{DB: tx}, lang: r.lang}
		item, err := txRepo.getReviewItemForUpdate(id)
		if err != nil {
			return err
		}
		if item.Status != EnrichmentStatusPending {
			return fmt.Errorf("%w: review item is not pending", ErrInvalidQueryParam)
		}

		var tags []TagInput
		if strings.TrimSpace(item.ProposedTagsJSON) != "" {
			if err := json.Unmarshal([]byte(item.ProposedTagsJSON), &tags); err != nil {
				return err
			}
		}
		if len(tags) > 0 {
			existingPoemTags, err := txRepo.ListTagsByPoemIDs([]int64{item.PoemID})
			if err != nil {
				return err
			}
			preExistingAssignment := make(map[int64]bool, len(existingPoemTags[item.PoemID]))
			for _, tag := range existingPoemTags[item.PoemID] {
				preExistingAssignment[tag.ID] = true
			}

			appliedTags, err := txRepo.AssignTagsToPoem(item.PoemID, tags)
			if err != nil {
				return err
			}
			appliedTagIDs := make([]int64, 0, len(appliedTags))
			for _, tag := range appliedTags {
				if !preExistingAssignment[tag.ID] {
					appliedTagIDs = append(appliedTagIDs, tag.ID)
				}
			}
			encoded, err := json.Marshal(appliedTagIDs)
			if err != nil {
				return err
			}
			item.AppliedTagIDsJSON = string(encoded)
		}

		var knowledge ProposedKnowledgeInput
		if strings.TrimSpace(item.ProposedKnowledgeJSON) != "" {
			if err := json.Unmarshal([]byte(item.ProposedKnowledgeJSON), &knowledge); err != nil {
				return err
			}
			if previous, err := txRepo.GetPoemKnowledge(item.PoemID); err == nil && previous != nil {
				encoded, err := json.Marshal(previous)
				if err != nil {
					return err
				}
				item.PreviousKnowledgeJSON = string(encoded)
			} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err := txRepo.UpsertPoemKnowledge(item.PoemID, knowledge, EnrichmentStatusAccepted, params.Reviewer, params.Notes); err != nil {
				return err
			}
		}

		updates := map[string]any{
			"status":                  EnrichmentStatusAccepted,
			"applied_tag_ids_json":    item.AppliedTagIDsJSON,
			"previous_knowledge_json": item.PreviousKnowledgeJSON,
			"reviewer":                strings.TrimSpace(params.Reviewer),
			"review_notes":            strings.TrimSpace(params.Notes),
			"updated_at":              time.Now().UTC(),
		}
		if err := tx.Table("enrichment_review_items").Where("id = ?", id).Updates(updates).Error; err != nil {
			return err
		}

		if item.JobID != nil {
			if err := txRepo.recountEnrichmentJob(*item.JobID); err != nil {
				return err
			}
		}
		updated, err := txRepo.getReviewItemForUpdate(id)
		if err != nil {
			return err
		}
		accepted = *updated
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &accepted, nil
}

// RejectEnrichmentReviewItem marks one candidate rejected without applying it.
func (r *Repository) RejectEnrichmentReviewItem(id int64, params ReviewDecisionParams) (*EnrichmentReviewItem, error) {
	if id < 1 {
		return nil, fmt.Errorf("%w: review item id must be positive", ErrInvalidQueryParam)
	}

	var item EnrichmentReviewItem
	err := r.db.Transaction(func(tx *gorm.DB) error {
		txRepo := &Repository{db: &DB{DB: tx}, lang: r.lang}
		current, err := txRepo.getReviewItemForUpdate(id)
		if err != nil {
			return err
		}
		if current.Status != EnrichmentStatusPending {
			return fmt.Errorf("%w: review item is not pending", ErrInvalidQueryParam)
		}
		if err := tx.Table("enrichment_review_items").Where("id = ?", id).Updates(map[string]any{
			"status":       EnrichmentStatusRejected,
			"reviewer":     strings.TrimSpace(params.Reviewer),
			"review_notes": strings.TrimSpace(params.Notes),
			"updated_at":   time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		if current.JobID != nil {
			if err := txRepo.recountEnrichmentJob(*current.JobID); err != nil {
				return err
			}
		}
		updated, err := txRepo.getReviewItemForUpdate(id)
		if err != nil {
			return err
		}
		item = *updated
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// GetEnrichmentRunSummary summarizes review progress and rejection reasons for a run.
func (r *Repository) GetEnrichmentRunSummary(runID string) (*EnrichmentRunSummary, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("%w: run_id is required", ErrInvalidQueryParam)
	}

	jobs, err := r.findEnrichmentJobsByRunID(runID)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	jobIDs := make([]int64, 0, len(jobs))
	for _, job := range jobs {
		jobIDs = append(jobIDs, job.ID)
	}

	var counts []struct {
		Status string
		Count  int
	}
	if err := r.db.Table("enrichment_review_items").
		Select("status, COUNT(*) AS count").
		Where("job_id IN ?", jobIDs).
		Group("status").
		Scan(&counts).Error; err != nil {
		return nil, err
	}

	summary := &EnrichmentRunSummary{
		RunID:             runID,
		JobIDs:            jobIDs,
		RejectedNoteTop10: []EnrichmentReviewNoteCount{},
	}
	for _, row := range counts {
		summary.TotalItems += row.Count
		switch row.Status {
		case EnrichmentStatusPending:
			summary.PendingCount = row.Count
		case EnrichmentStatusAccepted:
			summary.AcceptedCount = row.Count
		case EnrichmentStatusRejected:
			summary.RejectedCount = row.Count
		case EnrichmentStatusRolledBack:
			summary.RolledBackCount = row.Count
		}
	}
	summary.ReviewedCount = summary.AcceptedCount + summary.RejectedCount
	if summary.ReviewedCount > 0 {
		summary.PassRate = float64(summary.AcceptedCount) / float64(summary.ReviewedCount)
	}

	var noteRows []struct {
		ReviewNotes string
		Count       int
	}
	if err := r.db.Table("enrichment_review_items").
		Select("review_notes, COUNT(*) AS count").
		Where("job_id IN ? AND status = ? AND TRIM(COALESCE(review_notes, '')) <> ''", jobIDs, EnrichmentStatusRejected).
		Group("review_notes").
		Order("count DESC").
		Limit(10).
		Scan(&noteRows).Error; err != nil {
		return nil, err
	}
	for _, row := range noteRows {
		summary.RejectedNoteTop10 = append(summary.RejectedNoteTop10, EnrichmentReviewNoteCount{
			Note:  row.ReviewNotes,
			Count: row.Count,
		})
	}

	return summary, nil
}

// RollbackEnrichmentJob removes published knowledge and tag assignments created
// from accepted review items in one run. The run can be referenced by job scope
// or by config_json.run_id.
func (r *Repository) RollbackEnrichmentJob(runID string, params ReviewDecisionParams) (*EnrichmentRollbackResult, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("%w: run_id is required", ErrInvalidQueryParam)
	}

	jobs, err := r.findEnrichmentJobsByRunID(runID)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	result := &EnrichmentRollbackResult{
		RunID:    runID,
		Reviewer: strings.TrimSpace(params.Reviewer),
		Notes:    strings.TrimSpace(params.Notes),
	}
	for _, job := range jobs {
		partial, err := r.rollbackAcceptedReviewItems("job_id = ?", []any{job.ID}, params)
		if err != nil {
			return nil, err
		}
		result.ReviewItems += partial.ReviewItems
		result.PoemsAffected += partial.PoemsAffected
		result.TagsRemoved += partial.TagsRemoved
		result.KnowledgeRows += partial.KnowledgeRows
	}
	return result, nil
}

func (r *Repository) findEnrichmentJobsByRunID(runID string) ([]EnrichmentJob, error) {
	var jobs []EnrichmentJob
	err := r.db.Table("enrichment_jobs").
		Where("scope = ? OR config_json LIKE ?", runID, "%\"run_id\":\""+runID+"\"%").
		Order("id ASC").
		Find(&jobs).Error
	return jobs, err
}

// RollbackPoemEnrichment removes published knowledge and accepted AI tag
// assignments for one poem.
func (r *Repository) RollbackPoemEnrichment(poemID int64, params ReviewDecisionParams) (*EnrichmentRollbackResult, error) {
	if poemID < 1 {
		return nil, fmt.Errorf("%w: poem_id must be positive", ErrInvalidQueryParam)
	}
	result, err := r.rollbackAcceptedReviewItems("poem_id = ?", []any{poemID}, params)
	if err != nil {
		return nil, err
	}
	result.PoemID = &poemID
	result.Reviewer = strings.TrimSpace(params.Reviewer)
	result.Notes = strings.TrimSpace(params.Notes)
	return result, nil
}

// UpsertPoemKnowledge writes the accepted knowledge fields for one poem.
func (r *Repository) UpsertPoemKnowledge(poemID int64, input ProposedKnowledgeInput, qualityStatus, reviewer, notes string) error {
	if poemID < 1 {
		return fmt.Errorf("%w: poem_id must be positive", ErrInvalidQueryParam)
	}
	qualityStatus = strings.TrimSpace(qualityStatus)
	if qualityStatus == "" {
		qualityStatus = EnrichmentStatusDraft
	}
	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = "ai"
	}

	now := time.Now().UTC()
	return r.db.Exec(`
		INSERT INTO poem_knowledge (
			poem_id, summary, translation, annotation, recommendation,
			quality_status, source, reviewer, review_notes, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(poem_id)
		DO UPDATE SET
			summary = excluded.summary,
			translation = excluded.translation,
			annotation = excluded.annotation,
			recommendation = excluded.recommendation,
			quality_status = excluded.quality_status,
			source = excluded.source,
			reviewer = excluded.reviewer,
			review_notes = excluded.review_notes,
			updated_at = excluded.updated_at
	`, poemID, strings.TrimSpace(input.Summary), strings.TrimSpace(input.Translation), strings.TrimSpace(input.Annotation), strings.TrimSpace(input.Recommendation), qualityStatus, source, strings.TrimSpace(reviewer), strings.TrimSpace(notes), now, now).Error
}

// GetPoemKnowledge returns accepted/draft knowledge by poem id.
func (r *Repository) GetPoemKnowledge(poemID int64) (*PoemKnowledge, error) {
	var knowledge PoemKnowledge
	err := r.db.Table("poem_knowledge").Where("poem_id = ?", poemID).First(&knowledge).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, gorm.ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	return &knowledge, nil
}

// ListPoemKnowledgeByPoemIDs returns stored enrichment fields grouped by poem ID.
func (r *Repository) ListPoemKnowledgeByPoemIDs(poemIDs []int64) (map[int64]PoemKnowledge, error) {
	result := make(map[int64]PoemKnowledge, len(poemIDs))
	if len(poemIDs) == 0 {
		return result, nil
	}

	var rows []PoemKnowledge
	if err := r.db.Table("poem_knowledge").Where("poem_id IN ?", poemIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.PoemID] = row
	}
	return result, nil
}

// UpsertPoemEmbedding stores a vector row for future semantic recall.
func (r *Repository) UpsertPoemEmbedding(input PoemEmbedding) error {
	if input.PoemID < 1 || strings.TrimSpace(input.Provider) == "" || strings.TrimSpace(input.Model) == "" || input.Dimension < 1 || strings.TrimSpace(input.VectorJSON) == "" {
		return fmt.Errorf("%w: invalid embedding", ErrInvalidQueryParam)
	}
	now := time.Now().UTC()
	return r.db.Exec(`
		INSERT INTO poem_embeddings (poem_id, provider, model, dimension, vector_json, content_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(poem_id, provider, model)
		DO UPDATE SET
			dimension = excluded.dimension,
			vector_json = excluded.vector_json,
			content_hash = excluded.content_hash,
			created_at = excluded.created_at
	`, input.PoemID, strings.TrimSpace(input.Provider), strings.TrimSpace(input.Model), input.Dimension, strings.TrimSpace(input.VectorJSON), strings.TrimSpace(input.ContentHash), now).Error
}

func (r *Repository) rollbackAcceptedReviewItems(where string, args []any, params ReviewDecisionParams) (*EnrichmentRollbackResult, error) {
	result := &EnrichmentRollbackResult{
		Reviewer: strings.TrimSpace(params.Reviewer),
		Notes:    strings.TrimSpace(params.Notes),
	}

	err := r.db.Transaction(func(tx *gorm.DB) error {
		txRepo := &Repository{db: &DB{DB: tx}, lang: r.lang}

		var items []EnrichmentReviewItem
		query := tx.Table("enrichment_review_items").Where("status = ?", EnrichmentStatusAccepted)
		if strings.TrimSpace(where) != "" {
			query = query.Where(where, args...)
		}
		if err := query.Find(&items).Error; err != nil {
			return err
		}

		poems := make(map[int64]bool)
		jobs := make(map[int64]bool)
		for _, item := range items {
			poems[item.PoemID] = true
			if item.JobID != nil {
				jobs[*item.JobID] = true
			}

			var tags []TagInput
			if strings.TrimSpace(item.ProposedTagsJSON) != "" {
				if err := json.Unmarshal([]byte(item.ProposedTagsJSON), &tags); err != nil {
					return err
				}
			}
			removed, err := txRepo.deletePoemTagAssignments(item.PoemID, tags, item.AppliedTagIDsJSON)
			if err != nil {
				return err
			}
			result.TagsRemoved += removed

			if err := tx.Table("enrichment_review_items").Where("id = ?", item.ID).Updates(map[string]any{
				"status":       EnrichmentStatusRolledBack,
				"reviewer":     strings.TrimSpace(params.Reviewer),
				"review_notes": strings.TrimSpace(params.Notes),
				"updated_at":   time.Now().UTC(),
			}).Error; err != nil {
				return err
			}
			result.ReviewItems++
		}

		for poemID := range poems {
			restored := false
			for _, item := range items {
				if item.PoemID != poemID || strings.TrimSpace(item.PreviousKnowledgeJSON) == "" {
					continue
				}
				var previous PoemKnowledge
				if err := json.Unmarshal([]byte(item.PreviousKnowledgeJSON), &previous); err != nil {
					return err
				}
				if err := txRepo.restorePoemKnowledge(previous); err != nil {
					return err
				}
				result.KnowledgeRows++
				restored = true
				break
			}
			if !restored {
				deleted := tx.Table("poem_knowledge").Where("poem_id = ?", poemID).Delete(&PoemKnowledge{})
				if deleted.Error != nil {
					return deleted.Error
				}
				result.KnowledgeRows += deleted.RowsAffected
			}
			result.PoemsAffected++
		}

		for jobID := range jobs {
			if err := txRepo.recountEnrichmentJob(jobID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) deletePoemTagAssignments(poemID int64, tags []TagInput, appliedTagIDsJSON string) (int64, error) {
	var tagIDs []int64
	if strings.TrimSpace(appliedTagIDsJSON) != "" {
		if err := json.Unmarshal([]byte(appliedTagIDsJSON), &tagIDs); err != nil {
			return 0, err
		}
	}

	if len(tagIDs) == 0 && len(tags) == 0 {
		return 0, nil
	}

	removed := int64(0)
	for _, tagID := range tagIDs {
		if tagID < 1 {
			continue
		}
		deleted := r.db.Table("poem_tags").Where("poem_id = ? AND tag_id = ?", poemID, tagID).Delete(&PoemTag{})
		if deleted.Error != nil {
			return removed, deleted.Error
		}
		removed += deleted.RowsAffected
	}
	if len(tagIDs) > 0 {
		return removed, nil
	}

	for _, tagInput := range tags {
		name := strings.TrimSpace(tagInput.Name)
		if name == "" {
			continue
		}
		category := strings.TrimSpace(tagInput.Category)
		var tag Tag
		query := r.db.Table("tags").Where("name = ?", name)
		if category != "" {
			query = query.Where("category = ?", category)
		}
		err := query.First(&tag).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return removed, err
		}
		deleted := r.db.Table("poem_tags").Where("poem_id = ? AND tag_id = ?", poemID, tag.ID).Delete(&PoemTag{})
		if deleted.Error != nil {
			return removed, deleted.Error
		}
		removed += deleted.RowsAffected
	}
	return removed, nil
}

func (r *Repository) restorePoemKnowledge(previous PoemKnowledge) error {
	if previous.PoemID < 1 {
		return nil
	}
	return r.db.Exec(`
		INSERT INTO poem_knowledge (
			poem_id, summary, translation, annotation, recommendation,
			quality_status, source, reviewer, review_notes, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(poem_id)
		DO UPDATE SET
			summary = excluded.summary,
			translation = excluded.translation,
			annotation = excluded.annotation,
			recommendation = excluded.recommendation,
			quality_status = excluded.quality_status,
			source = excluded.source,
			reviewer = excluded.reviewer,
			review_notes = excluded.review_notes,
			updated_at = excluded.updated_at
	`, previous.PoemID, previous.Summary, previous.Translation, previous.Annotation, previous.Recommendation, previous.QualityStatus, previous.Source, previous.Reviewer, previous.ReviewNotes, previous.CreatedAt, time.Now().UTC()).Error
}

func (r *Repository) getReviewItemForUpdate(id int64) (*EnrichmentReviewItem, error) {
	var item EnrichmentReviewItem
	err := r.db.Table("enrichment_review_items").Where("id = ?", id).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, gorm.ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) recountEnrichmentJob(jobID int64) error {
	var counts []struct {
		Status string
		Count  int
	}
	if err := r.db.Table("enrichment_review_items").Select("status, COUNT(*) AS count").Where("job_id = ?", jobID).Group("status").Scan(&counts).Error; err != nil {
		return err
	}

	updates := map[string]any{
		"processed_count": 0,
		"accepted_count":  0,
		"rejected_count":  0,
		"updated_at":      time.Now().UTC(),
	}
	processed := 0
	for _, row := range counts {
		switch row.Status {
		case EnrichmentStatusAccepted:
			updates["accepted_count"] = row.Count
			processed += row.Count
		case EnrichmentStatusRejected:
			updates["rejected_count"] = row.Count
			processed += row.Count
		}
	}
	updates["processed_count"] = processed
	return r.db.Table("enrichment_jobs").Where("id = ?", jobID).Updates(updates).Error
}
