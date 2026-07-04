package database

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// PublicWorkListParams controls global public-library listing and partner export.
type PublicWorkListParams struct {
	Query        string
	WorkType     string
	AuthorHandle string
	Sort         string
	Limit        int
	Offset       int
}

// PublicAuthorListParams controls global public-author ranking/listing.
type PublicAuthorListParams struct {
	Query  string
	Sort   string
	Limit  int
	Offset int
}

// PublicWorkSummary joins one public work with its public author, tips, and certificate metadata.
type PublicWorkSummary struct {
	Work             OriginalWork
	Author           UserAccount
	TipCount         int64
	TotalTipAmount   int
	CertificateCode  string
	CertificateHash  string
	AnchorStatus     string
	AnchorNetwork    string
	AnchorTxID       string
	LatestActivityAt *time.Time
}

// PublicAuthorSummary joins one public author with global library ranking counters.
type PublicAuthorSummary struct {
	Author            UserAccount
	PublicWorkCount   int64
	TipCount          int64
	TotalTipAmount    int
	LatestPublishedAt *time.Time
}

type publicWorkSummaryRow struct {
	WorkID             int64      `gorm:"column:work_id"`
	WorkCode           string     `gorm:"column:work_code"`
	APIKeyID           int64      `gorm:"column:api_key_id"`
	Title              string     `gorm:"column:title"`
	WorkType           string     `gorm:"column:work_type"`
	Content            string     `gorm:"column:content"`
	ContentHash        string     `gorm:"column:content_hash"`
	Description        string     `gorm:"column:description"`
	Visibility         string     `gorm:"column:visibility"`
	Status             string     `gorm:"column:status"`
	LicenseType        string     `gorm:"column:license_type"`
	LicenseVersion     string     `gorm:"column:license_version"`
	OriginalCommitment bool       `gorm:"column:original_commitment"`
	LicenseAccepted    bool       `gorm:"column:license_accepted"`
	PlagiarismStatus   string     `gorm:"column:plagiarism_status"`
	ImagePrompt        string     `gorm:"column:image_prompt"`
	Version            int        `gorm:"column:version"`
	PublishedAt        *time.Time `gorm:"column:published_at"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at"`

	AuthorID        int64     `gorm:"column:author_id"`
	AuthorHandle    string    `gorm:"column:author_handle"`
	AuthorName      string    `gorm:"column:author_name"`
	AuthorBio       string    `gorm:"column:author_bio"`
	AuthorAvatarURL string    `gorm:"column:author_avatar_url"`
	AuthorWebsite   string    `gorm:"column:author_website_url"`
	AuthorStatus    string    `gorm:"column:author_status"`
	AuthorCreatedAt time.Time `gorm:"column:author_created_at"`
	AuthorUpdatedAt time.Time `gorm:"column:author_updated_at"`

	TipCount         int64  `gorm:"column:tip_count"`
	TotalTipAmount   int    `gorm:"column:total_tip_amount"`
	CertificateCode  string `gorm:"column:certificate_code"`
	CertificateHash  string `gorm:"column:certificate_hash"`
	AnchorStatus     string `gorm:"column:anchor_status"`
	AnchorNetwork    string `gorm:"column:anchor_network"`
	AnchorTxID       string `gorm:"column:anchor_tx_id"`
	LatestActivityAt string `gorm:"column:latest_activity_at"`
}

type publicAuthorSummaryRow struct {
	AuthorID          int64     `gorm:"column:author_id"`
	Handle            string    `gorm:"column:handle"`
	DisplayName       string    `gorm:"column:display_name"`
	Bio               string    `gorm:"column:bio"`
	AvatarURL         string    `gorm:"column:avatar_url"`
	WebsiteURL        string    `gorm:"column:website_url"`
	Status            string    `gorm:"column:status"`
	CreatedAt         time.Time `gorm:"column:created_at"`
	UpdatedAt         time.Time `gorm:"column:updated_at"`
	PublicWorkCount   int64     `gorm:"column:public_work_count"`
	TipCount          int64     `gorm:"column:tip_count"`
	TotalTipAmount    int       `gorm:"column:total_tip_amount"`
	LatestPublishedAt string    `gorm:"column:latest_published_at"`
}

// ListPublicOriginalWorkSummaries returns global public works for discovery pages, rankings, and partner export.
func (r *Repository) ListPublicOriginalWorkSummaries(params PublicWorkListParams) ([]PublicWorkSummary, int64, error) {
	q, err := r.publicWorksBaseQuery(params)
	if err != nil {
		return nil, 0, err
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	rows := []publicWorkSummaryRow{}
	listQ, err := r.publicWorksBaseQuery(params)
	if err != nil {
		return nil, 0, err
	}
	err = listQ.
		Select(publicWorkSummarySelectSQL()).
		Joins(publicWorkTipsJoinSQL()).
		Joins("LEFT JOIN work_certificates ON work_certificates.work_id = original_works.id").
		Order(publicWorkOrderClause(params.Sort)).
		Limit(clampPublicLimit(params.Limit)).
		Offset(clampPublicOffset(params.Offset)).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	items := make([]PublicWorkSummary, len(rows))
	for i, row := range rows {
		items[i] = row.toPublicWorkSummary()
	}
	return items, total, nil
}

// ListPublicAuthorSummaries returns public authors with aggregate work/tip counters.
func (r *Repository) ListPublicAuthorSummaries(params PublicAuthorListParams) ([]PublicAuthorSummary, int64, error) {
	q := r.publicAuthorsBaseQuery(params)

	var total int64
	if err := q.Distinct("user_accounts.id").Count(&total).Error; err != nil {
		return nil, 0, err
	}

	rows := []publicAuthorSummaryRow{}
	err := r.publicAuthorsBaseQuery(params).
		Select(`user_accounts.id AS author_id,
			user_accounts.handle AS handle,
			user_accounts.display_name AS display_name,
			user_accounts.bio AS bio,
			user_accounts.avatar_url AS avatar_url,
			user_accounts.website_url AS website_url,
			user_accounts.status AS status,
			user_accounts.created_at AS created_at,
			user_accounts.updated_at AS updated_at,
			COUNT(DISTINCT original_works.id) AS public_work_count,
			COALESCE(SUM(tip_stats.tip_count), 0) AS tip_count,
			COALESCE(SUM(tip_stats.total_tip_amount), 0) AS total_tip_amount,
			MAX(original_works.published_at) AS latest_published_at`).
		Joins(publicWorkTipsJoinSQL()).
		Group("user_accounts.id").
		Order(publicAuthorOrderClause(params.Sort)).
		Limit(clampPublicLimit(params.Limit)).
		Offset(clampPublicOffset(params.Offset)).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	items := make([]PublicAuthorSummary, len(rows))
	for i, row := range rows {
		items[i] = row.toPublicAuthorSummary()
	}
	return items, total, nil
}

func (r *Repository) publicWorksBaseQuery(params PublicWorkListParams) (*gorm.DB, error) {
	q := r.db.Table("original_works").
		Joins("JOIN api_keys ON api_keys.id = original_works.api_key_id").
		Joins("JOIN user_accounts ON user_accounts.id = api_keys.account_id").
		Where("original_works.status = ? AND original_works.visibility = ? AND user_accounts.status = ?", WorkStatusPublished, WorkVisibilityPublic, "active")

	if query := strings.TrimSpace(params.Query); query != "" {
		like := "%" + limitString(query, 120) + "%"
		q = q.Where("(original_works.title LIKE ? OR original_works.content LIKE ? OR original_works.description LIKE ? OR user_accounts.display_name LIKE ? OR user_accounts.handle LIKE ?)", like, like, like, like, like)
	}
	if workTypeInput := strings.TrimSpace(params.WorkType); workTypeInput != "" && strings.ToLower(workTypeInput) != "all" {
		workType := normalizeWorkType(workTypeInput)
		if workType == "" {
			return nil, fmt.Errorf("%w: unsupported work_type", ErrInvalidQueryParam)
		}
		q = q.Where("original_works.work_type = ?", workType)
	}
	if handleInput := strings.TrimSpace(params.AuthorHandle); handleInput != "" {
		handle, err := normalizeUserHandle(handleInput)
		if err != nil {
			return nil, err
		}
		q = q.Where("user_accounts.handle = ?", handle)
	}
	return q, nil
}

func (r *Repository) publicAuthorsBaseQuery(params PublicAuthorListParams) *gorm.DB {
	q := r.db.Table("user_accounts").
		Joins("JOIN api_keys ON api_keys.account_id = user_accounts.id").
		Joins("JOIN original_works ON original_works.api_key_id = api_keys.id").
		Where("user_accounts.status = ? AND original_works.status = ? AND original_works.visibility = ?", "active", WorkStatusPublished, WorkVisibilityPublic)

	if query := strings.TrimSpace(params.Query); query != "" {
		like := "%" + limitString(query, 120) + "%"
		q = q.Where("(user_accounts.display_name LIKE ? OR user_accounts.handle LIKE ? OR user_accounts.bio LIKE ?)", like, like, like)
	}
	return q
}

func publicWorkSummarySelectSQL() string {
	return `original_works.id AS work_id,
		original_works.work_code AS work_code,
		original_works.api_key_id AS api_key_id,
		original_works.title AS title,
		original_works.work_type AS work_type,
		original_works.content AS content,
		original_works.content_hash AS content_hash,
		original_works.description AS description,
		original_works.visibility AS visibility,
		original_works.status AS status,
		original_works.license_type AS license_type,
		original_works.license_version AS license_version,
		original_works.original_commitment AS original_commitment,
		original_works.license_accepted AS license_accepted,
		original_works.plagiarism_status AS plagiarism_status,
		original_works.image_prompt AS image_prompt,
		original_works.version AS version,
		original_works.published_at AS published_at,
		original_works.created_at AS created_at,
		original_works.updated_at AS updated_at,
		user_accounts.id AS author_id,
		user_accounts.handle AS author_handle,
		user_accounts.display_name AS author_name,
		user_accounts.bio AS author_bio,
		user_accounts.avatar_url AS author_avatar_url,
		user_accounts.website_url AS author_website_url,
		user_accounts.status AS author_status,
		user_accounts.created_at AS author_created_at,
		user_accounts.updated_at AS author_updated_at,
		COALESCE(tip_stats.tip_count, 0) AS tip_count,
		COALESCE(tip_stats.total_tip_amount, 0) AS total_tip_amount,
		COALESCE(work_certificates.certificate_code, '') AS certificate_code,
		COALESCE(work_certificates.certificate_hash, '') AS certificate_hash,
		COALESCE(work_certificates.anchor_status, '') AS anchor_status,
		COALESCE(work_certificates.anchor_network, '') AS anchor_network,
		COALESCE(work_certificates.anchor_tx_id, '') AS anchor_tx_id,
		CASE
			WHEN tip_stats.latest_tip_at IS NOT NULL AND (original_works.published_at IS NULL OR tip_stats.latest_tip_at > original_works.published_at) THEN tip_stats.latest_tip_at
			ELSE original_works.published_at
		END AS latest_activity_at`
}

func publicWorkTipsJoinSQL() string {
	return `LEFT JOIN (
		SELECT work_id,
			COUNT(*) AS tip_count,
			COALESCE(SUM(amount), 0) AS total_tip_amount,
			MAX(created_at) AS latest_tip_at
		FROM work_tips
		WHERE status = 'succeeded'
		GROUP BY work_id
	) tip_stats ON tip_stats.work_id = original_works.id`
}

func publicWorkOrderClause(sort string) string {
	switch strings.ToLower(strings.TrimSpace(sort)) {
	case "tips", "tip", "popular", "hot":
		return "total_tip_amount DESC, tip_count DESC, original_works.published_at DESC, original_works.id DESC"
	case "activity", "active":
		return "latest_activity_at DESC, original_works.published_at DESC, original_works.id DESC"
	case "oldest":
		return "original_works.published_at ASC, original_works.id ASC"
	case "title":
		return "original_works.title ASC, original_works.id ASC"
	default:
		return "original_works.published_at DESC, original_works.id DESC"
	}
}

func publicAuthorOrderClause(sort string) string {
	switch strings.ToLower(strings.TrimSpace(sort)) {
	case "tips", "tip", "popular", "hot":
		return "total_tip_amount DESC, tip_count DESC, public_work_count DESC, latest_published_at DESC, user_accounts.id DESC"
	case "latest", "new":
		return "latest_published_at DESC, user_accounts.id DESC"
	case "name":
		return "user_accounts.display_name ASC, user_accounts.id ASC"
	default:
		return "public_work_count DESC, latest_published_at DESC, user_accounts.id DESC"
	}
}

func clampPublicLimit(limit int) int {
	if limit < 1 || limit > 100 {
		return 20
	}
	return limit
}

func clampPublicOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

func (row publicWorkSummaryRow) toPublicWorkSummary() PublicWorkSummary {
	return PublicWorkSummary{
		Work: OriginalWork{
			ID:                 row.WorkID,
			WorkCode:           row.WorkCode,
			APIKeyID:           row.APIKeyID,
			Title:              row.Title,
			WorkType:           row.WorkType,
			Content:            row.Content,
			ContentHash:        row.ContentHash,
			Description:        row.Description,
			Visibility:         row.Visibility,
			Status:             row.Status,
			LicenseType:        row.LicenseType,
			LicenseVersion:     row.LicenseVersion,
			OriginalCommitment: row.OriginalCommitment,
			LicenseAccepted:    row.LicenseAccepted,
			PlagiarismStatus:   row.PlagiarismStatus,
			ImagePrompt:        row.ImagePrompt,
			Version:            row.Version,
			PublishedAt:        row.PublishedAt,
			CreatedAt:          row.CreatedAt,
			UpdatedAt:          row.UpdatedAt,
		},
		Author: UserAccount{
			ID:          row.AuthorID,
			Handle:      row.AuthorHandle,
			DisplayName: row.AuthorName,
			Bio:         row.AuthorBio,
			AvatarURL:   row.AuthorAvatarURL,
			WebsiteURL:  row.AuthorWebsite,
			Status:      row.AuthorStatus,
			CreatedAt:   row.AuthorCreatedAt,
			UpdatedAt:   row.AuthorUpdatedAt,
		},
		TipCount:         row.TipCount,
		TotalTipAmount:   row.TotalTipAmount,
		CertificateCode:  row.CertificateCode,
		CertificateHash:  row.CertificateHash,
		AnchorStatus:     row.AnchorStatus,
		AnchorNetwork:    row.AnchorNetwork,
		AnchorTxID:       row.AnchorTxID,
		LatestActivityAt: parsePublicSummaryTime(row.LatestActivityAt),
	}
}

func (row publicAuthorSummaryRow) toPublicAuthorSummary() PublicAuthorSummary {
	return PublicAuthorSummary{
		Author: UserAccount{
			ID:          row.AuthorID,
			Handle:      row.Handle,
			DisplayName: row.DisplayName,
			Bio:         row.Bio,
			AvatarURL:   row.AvatarURL,
			WebsiteURL:  row.WebsiteURL,
			Status:      row.Status,
			CreatedAt:   row.CreatedAt,
			UpdatedAt:   row.UpdatedAt,
		},
		PublicWorkCount:   row.PublicWorkCount,
		TipCount:          row.TipCount,
		TotalTipAmount:    row.TotalTipAmount,
		LatestPublishedAt: parsePublicSummaryTime(row.LatestPublishedAt),
	}
}

func parsePublicSummaryTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05.999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return &parsed
		}
	}
	return nil
}
