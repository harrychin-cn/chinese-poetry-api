package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

// WorkHandler manages user original works.
type WorkHandler struct {
	repo *database.Repository
}

// NewWorkHandler creates a work handler.
func NewWorkHandler(repo *database.Repository) *WorkHandler {
	return &WorkHandler{repo: repo}
}

type createWorkRequest struct {
	Title              string `json:"title"`
	WorkType           string `json:"work_type"`
	Content            string `json:"content"`
	Description        string `json:"description"`
	Visibility         string `json:"visibility"`
	LicenseType        string `json:"license_type"`
	LicenseVersion     string `json:"license_version"`
	OriginalCommitment bool   `json:"original_commitment"`
	LicenseAccepted    bool   `json:"license_accepted"`
	ImagePrompt        string `json:"image_prompt"`
	Publish            bool   `json:"publish"`
	ChangeNote         string `json:"change_note"`
}

type updateWorkRequest struct {
	Title              *string `json:"title"`
	WorkType           *string `json:"work_type"`
	Content            *string `json:"content"`
	Description        *string `json:"description"`
	Visibility         *string `json:"visibility"`
	LicenseType        *string `json:"license_type"`
	LicenseVersion     *string `json:"license_version"`
	OriginalCommitment *bool   `json:"original_commitment"`
	LicenseAccepted    *bool   `json:"license_accepted"`
	ImagePrompt        *string `json:"image_prompt"`
	Publish            *bool   `json:"publish"`
	ChangeNote         string  `json:"change_note"`
}

// Create stores an original work for the current API key.
func (h *WorkHandler) Create(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}

	var req createWorkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid json body")
		return
	}

	work, err := h.repo.CreateOriginalWork(database.CreateOriginalWorkParams{
		APIKeyID:           apiKeyID,
		Title:              req.Title,
		WorkType:           req.WorkType,
		Content:            req.Content,
		Description:        req.Description,
		Visibility:         req.Visibility,
		LicenseType:        req.LicenseType,
		LicenseVersion:     req.LicenseVersion,
		OriginalCommitment: req.OriginalCommitment,
		LicenseAccepted:    req.LicenseAccepted,
		ImagePrompt:        req.ImagePrompt,
		Publish:            req.Publish,
		ChangeNote:         req.ChangeNote,
	})
	if errors.Is(err, database.ErrInvalidQueryParam) {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to create work")
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": formatWork(*work)})
}

// List returns works owned by the current API key.
func (h *WorkHandler) List(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	works, err := h.repo.ListOriginalWorks(apiKeyID, c.DefaultQuery("status", "all"), queryInt(c, "limit", 20))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to list works")
		return
	}
	data := make([]map[string]any, len(works))
	for i, work := range works {
		data[i] = formatWork(work)
	}
	respondOK(c, gin.H{"items": data})
}

// Get returns one owned work.
func (h *WorkHandler) Get(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	id, ok := parseWorkID(c)
	if !ok {
		return
	}
	work, err := h.repo.GetOriginalWork(apiKeyID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusNotFound, "work not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to get work")
		return
	}
	respondOK(c, formatWork(*work))
}

// Update updates an owned work.
func (h *WorkHandler) Update(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	id, ok := parseWorkID(c)
	if !ok {
		return
	}
	var req updateWorkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid json body")
		return
	}
	work, err := h.repo.UpdateOriginalWork(apiKeyID, id, database.UpdateOriginalWorkParams{
		Title:              req.Title,
		WorkType:           req.WorkType,
		Content:            req.Content,
		Description:        req.Description,
		Visibility:         req.Visibility,
		LicenseType:        req.LicenseType,
		LicenseVersion:     req.LicenseVersion,
		OriginalCommitment: req.OriginalCommitment,
		LicenseAccepted:    req.LicenseAccepted,
		ImagePrompt:        req.ImagePrompt,
		Publish:            req.Publish,
		ChangeNote:         req.ChangeNote,
	})
	if errors.Is(err, database.ErrInvalidQueryParam) {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusNotFound, "work not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to update work")
		return
	}
	respondOK(c, formatWork(*work))
}

// Publish marks an owned work public after license confirmation.
func (h *WorkHandler) Publish(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	id, ok := parseWorkID(c)
	if !ok {
		return
	}
	work, err := h.repo.PublishOriginalWork(apiKeyID, id)
	if errors.Is(err, database.ErrInvalidQueryParam) {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusNotFound, "work not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to publish work")
		return
	}
	respondOK(c, formatWork(*work))
}

// Versions returns saved versions for an owned work.
func (h *WorkHandler) Versions(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	id, ok := parseWorkID(c)
	if !ok {
		return
	}
	versions, err := h.repo.ListOriginalWorkVersions(apiKeyID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusNotFound, "work not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to list work versions")
		return
	}
	data := make([]map[string]any, len(versions))
	for i, version := range versions {
		data[i] = formatWorkVersion(version)
	}
	respondOK(c, gin.H{"items": data})
}

// LicenseAcceptances returns license records for an owned work.
func (h *WorkHandler) LicenseAcceptances(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	id, ok := parseWorkID(c)
	if !ok {
		return
	}
	records, err := h.repo.ListWorkLicenseAcceptances(apiKeyID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusNotFound, "work not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to list license records")
		return
	}
	data := make([]map[string]any, len(records))
	for i, record := range records {
		data[i] = formatWorkLicenseAcceptance(record)
	}
	respondOK(c, gin.H{"items": data})
}

// PlagiarismReport returns the latest originality check report for an owned work.
func (h *WorkHandler) PlagiarismReport(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	id, ok := parseWorkID(c)
	if !ok {
		return
	}
	report, err := h.repo.LatestPlagiarismReport(apiKeyID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusNotFound, "plagiarism report not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to get plagiarism report")
		return
	}
	respondOK(c, formatPlagiarismReport(*report))
}

// PublicGet returns a published public work by platform work code.
func (h *WorkHandler) PublicGet(c *gin.Context) {
	work, err := h.repo.GetPublicOriginalWorkByCode(c.Param("code"))
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, database.ErrInvalidQueryParam) {
		respondError(c, http.StatusNotFound, "public work not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to get public work")
		return
	}
	respondOK(c, formatWork(*work))
}

func parseWorkID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		respondError(c, http.StatusBadRequest, "invalid work id")
		return 0, false
	}
	return id, true
}

func formatWork(work database.OriginalWork) map[string]any {
	return map[string]any{
		"id":                  work.ID,
		"work_code":           work.WorkCode,
		"api_key_id":          work.APIKeyID,
		"title":               work.Title,
		"work_type":           work.WorkType,
		"content":             work.Content,
		"content_hash":        work.ContentHash,
		"description":         work.Description,
		"visibility":          work.Visibility,
		"status":              work.Status,
		"license_type":        work.LicenseType,
		"license_version":     work.LicenseVersion,
		"original_commitment": work.OriginalCommitment,
		"license_accepted":    work.LicenseAccepted,
		"plagiarism_status":   work.PlagiarismStatus,
		"image_prompt":        work.ImagePrompt,
		"version":             work.Version,
		"published_at":        work.PublishedAt,
		"created_at":          work.CreatedAt,
		"updated_at":          work.UpdatedAt,
	}
}

func formatWorkVersion(version database.OriginalWorkVersion) map[string]any {
	return map[string]any{
		"id":           version.ID,
		"work_id":      version.WorkID,
		"version":      version.Version,
		"title":        version.Title,
		"content":      version.Content,
		"content_hash": version.ContentHash,
		"change_note":  version.ChangeNote,
		"created_at":   version.CreatedAt,
	}
}

func formatWorkLicenseAcceptance(record database.WorkLicenseAcceptance) map[string]any {
	return map[string]any{
		"id":                  record.ID,
		"work_id":             record.WorkID,
		"api_key_id":          record.APIKeyID,
		"license_type":        record.LicenseType,
		"license_version":     record.LicenseVersion,
		"original_commitment": record.OriginalCommitment,
		"license_accepted":    record.LicenseAccepted,
		"acceptance_text":     record.AcceptanceText,
		"accepted_at":         record.AcceptedAt,
	}
}

func formatPlagiarismReport(value database.PlagiarismReportWithMatches) map[string]any {
	matches := make([]map[string]any, len(value.Matches))
	for i, match := range value.Matches {
		matches[i] = map[string]any{
			"id":            match.ID,
			"source_type":   match.SourceType,
			"source_id":     match.SourceID,
			"source_title":  match.SourceTitle,
			"source_author": match.SourceAuthor,
			"similarity":    match.Similarity,
			"match_type":    match.MatchType,
			"excerpt":       match.Excerpt,
			"created_at":    match.CreatedAt,
		}
	}
	return map[string]any{
		"id":                  value.Report.ID,
		"work_id":             value.Report.WorkID,
		"normalized_hash":     value.Report.NormalizedHash,
		"simhash":             value.Report.SimHash,
		"risk_level":          value.Report.RiskLevel,
		"risk_reason":         value.Report.RiskReason,
		"exact_match_count":   value.Report.ExactMatchCount,
		"similar_match_count": value.Report.SimilarMatchCount,
		"review_status":       value.Report.ReviewStatus,
		"created_at":          value.Report.CreatedAt,
		"matches":             matches,
	}
}
