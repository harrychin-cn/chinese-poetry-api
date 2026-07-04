package handler

import (
	"errors"
	"net/http"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

// ReverseCreationHandler creates draft works from a story, image note, or mood description.
type ReverseCreationHandler struct {
	repo *database.Repository
}

// NewReverseCreationHandler creates a reverse-creation handler.
func NewReverseCreationHandler(repo *database.Repository) *ReverseCreationHandler {
	return &ReverseCreationHandler{repo: repo}
}

type reverseCreateRequest struct {
	SourceType string `json:"source_type"`
	SourceText string `json:"source_text"`
	ImageURL   string `json:"image_url"`
	WorkType   string `json:"work_type"`
	Style      string `json:"style"`
	Title      string `json:"title"`
	Save       bool   `json:"save"`
	DryRun     bool   `json:"dry_run"`
}

// Create generates a local reverse-creation draft and optionally saves it as a private work.
func (h *ReverseCreationHandler) Create(c *gin.Context) {
	apiKey, ok := apiKeyFromContext(c)
	if !ok || apiKey == nil {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}

	var req reverseCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid reverse creation request")
		return
	}

	sourceType := normalizeReverseSourceType(req.SourceType)
	sourceText := trimRunes(strings.TrimSpace(req.SourceText), 3000)
	imageURL := trimRunes(strings.TrimSpace(req.ImageURL), 1000)
	if sourceText == "" && imageURL == "" {
		respondError(c, http.StatusBadRequest, "source_text or image_url is required")
		return
	}

	workType := normalizeReverseWorkType(req.WorkType)
	if workType == "" {
		respondError(c, http.StatusBadRequest, "unsupported work_type")
		return
	}
	style := trimRunes(strings.TrimSpace(req.Style), 120)
	sourceForDraft := sourceText
	if sourceForDraft == "" {
		sourceForDraft = imageURL
	}
	title := normalizeReverseDraftTitle(req.Title, workType, sourceForDraft)
	content := buildReverseDraftContent(workType, title, sourceForDraft, style)
	prompt := buildReverseCreationPrompt(sourceType, sourceText, imageURL, workType, style, title)

	status := database.ImageJobStatusPending
	if req.DryRun {
		status = database.ImageJobStatusPromptReady
	}
	job, err := h.repo.CreateReverseCreationJob(database.CreateReverseCreationJobParams{
		APIKeyID:         apiKey.ID,
		Status:           status,
		SourceType:       sourceType,
		SourceText:       sourceText,
		ImageURL:         imageURL,
		WorkType:         workType,
		Style:            style,
		Prompt:           prompt,
		GeneratedTitle:   title,
		GeneratedContent: content,
		Model:            database.ReverseCreationModelLocal,
	})
	if errors.Is(err, database.ErrInvalidQueryParam) {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to create reverse creation job")
		return
	}

	if req.DryRun {
		respondOK(c, gin.H{
			"dry_run":     true,
			"save":        false,
			"title":       title,
			"content":     content,
			"work_type":   workType,
			"source_type": sourceType,
			"style":       style,
			"prompt":      prompt,
			"job":         formatReverseCreationJob(*job),
		})
		return
	}

	var saved *database.OriginalWork
	var workID *int64
	if req.Save {
		saved, err = h.repo.CreateOriginalWork(database.CreateOriginalWorkParams{
			APIKeyID:    apiKey.ID,
			Title:       title,
			WorkType:    workType,
			Content:     content,
			Description: "Reverse creation draft. Review and edit before publishing.",
			Visibility:  database.WorkVisibilityPrivate,
			Publish:     false,
			ChangeNote:  "reverse creation draft",
		})
		if err != nil {
			_, _ = h.repo.FailReverseCreationJob(job.ID, err.Error())
			if errors.Is(err, database.ErrInvalidQueryParam) {
				respondError(c, http.StatusBadRequest, err.Error())
				return
			}
			respondError(c, http.StatusInternalServerError, "failed to save reverse created work")
			return
		}
		id := saved.ID
		workID = &id
	}

	job, err = h.repo.CompleteReverseCreationJob(job.ID, workID, title, content)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to complete reverse creation job")
		return
	}

	data := gin.H{
		"dry_run":     false,
		"save":        req.Save,
		"title":       title,
		"content":     content,
		"work_type":   workType,
		"source_type": sourceType,
		"style":       style,
		"prompt":      prompt,
		"job":         formatReverseCreationJob(*job),
	}
	if saved != nil {
		data["work"] = formatWork(*saved)
	}
	respondOK(c, data)
}

// ListJobs returns recent reverse-creation jobs for the current API key.
func (h *ReverseCreationHandler) ListJobs(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	items, err := h.repo.ListReverseCreationJobs(apiKeyID, queryInt(c, "limit", 20))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to list reverse creation jobs")
		return
	}
	data := make([]map[string]any, len(items))
	for i, item := range items {
		data[i] = formatReverseCreationJob(item)
	}
	respondOK(c, gin.H{"items": data})
}

func normalizeReverseSourceType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "image", "mood", "prompt":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "story"
	}
}

func normalizeReverseWorkType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "poem":
		return "poem"
	case "ci", "qu", "fu", "modern_poem", "lyric":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func normalizeReverseDraftTitle(value, workType, source string) string {
	title := trimRunes(strings.TrimSpace(value), 120)
	if title != "" {
		return title
	}
	phrase := reverseDraftPhrase(source)
	if phrase != "" {
		return trimRunes(phrase+"\u4e4b"+reverseWorkTypeName(workType), 120)
	}
	return "\u9006\u5411\u521b\u4f5c\u00b7" + reverseWorkTypeName(workType)
}

func reverseWorkTypeName(workType string) string {
	switch workType {
	case "ci":
		return "\u8bcd"
	case "qu":
		return "\u66f2"
	case "fu":
		return "\u8d4b"
	case "modern_poem":
		return "\u73b0\u4ee3\u8bd7"
	case "lyric":
		return "\u6b4c\u8bcd"
	default:
		return "\u8bd7"
	}
}

func reverseDraftPhrase(source string) string {
	source = strings.TrimSpace(source)
	var cjk []rune
	for _, r := range source {
		if isCJKRune(r) {
			cjk = append(cjk, r)
			if len(cjk) >= 6 {
				break
			}
		}
	}
	if len(cjk) > 0 {
		return string(cjk)
	}
	parts := strings.FieldsFunc(source, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r))
	})
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			return trimRunes(part, 20)
		}
	}
	return "\u65b0\u610f"
}

func isCJKRune(r rune) bool {
	return (r >= '\u4e00' && r <= '\u9fff') ||
		(r >= '\u3400' && r <= '\u4dbf') ||
		(r >= '\uf900' && r <= '\ufaff')
}

func buildReverseDraftContent(workType, title, source, style string) string {
	scene := trimRunes(reverseDraftPhrase(source), 8)
	if scene == "" {
		scene = "\u6e05\u666f"
	}
	switch workType {
	case "ci":
		return strings.Join([]string{
			"\u3010\u6d63\u6eaa\u6c99\u00b7" + title + "\u3011",
			scene + "\u5165\u68a6\u8fd1\u9ec4\u660f\uff0c",
			"\u4e00\u70b9\u5fae\u5149\u843d\u65e7\u75d5\u3002",
			"\u6e05\u98ce\u4e0d\u8bed\u81ea\u6210\u6587\u3002",
			"\u4e91\u5916\u5c71\u6df1\u7559\u8fdc\u610f\uff0c",
			"\u706f\u524d\u4eba\u9759\u542c\u65b0\u6625\u3002",
			"\u534a\u7a97\u8bd7\u6c14\u6ee1\u82d4\u7eb9\u3002",
		}, "\n")
	case "qu":
		return strings.Join([]string{
			"\u3010\u8d8a\u8c03\u00b7\u5c0f\u6843\u7ea2\u3011" + title,
			scene + "\uff0c\u6e05\u98ce\u8fc7\u6d45\u6c99\uff0c\u4e00\u5f84\u82b1\u9634\u4e0b\u3002",
			"\u8c01\u628a\u6545\u4e8b\u5199\u6210\u971e\uff1f\u843d\u7b14\u5904\uff0c\u6c5f\u5929\u4e0d\u8bf4\u8bdd\u3002",
			"\u7559\u5f97\u65b0\u58f0\u5165\u9152\u5bb6\u3002",
		}, "\n")
	case "fu":
		return strings.Join([]string{
			"\u592b" + scene + "\uff0c\u8d77\u4e8e\u5fae\u5149\u4e4b\u9645\uff0c\u6210\u4e8e\u5fc3\u58f0\u4e4b\u95f4\u3002",
			"\u89c2\u5176\u52bf\uff0c\u5219\u4e91\u5f00\u5cf0\u8fdc\uff1b\u542c\u5176\u610f\uff0c\u5219\u98ce\u8fc7\u679d\u95f2\u3002",
			"\u6545\u4ee5\u5b57\u62fe\u5f71\uff0c\u4ee5\u97f5\u7559\u771f\uff0c\u4f7f\u4e00\u65f6\u6240\u89c1\uff0c\u5316\u4e3a\u957f\u65e5\u53ef\u541f\u3002",
		}, "\n")
	case "modern_poem":
		return strings.Join([]string{
			scene,
			"\u50cf\u4e00\u5c01\u6ca1\u6709\u5bc4\u51fa\u7684\u4fe1",
			"\u5728\u98ce\u91cc\u88ab\u6253\u5f00",
			"\u6211\u628a\u5b83\u6298\u6210\u4e00\u884c\u8bd7",
			"\u7559\u7ed9\u4eca\u591c\u7684\u706f\u706b",
		}, "\n")
	case "lyric":
		return strings.Join([]string{
			scene + "\u5728\u98ce\u91cc\u8f7b\u8f7b\u54cd",
			"\u50cf\u4f60\u8d70\u8fc7\u65e7\u65f6\u5149",
			"\u6211\u628a\u6545\u4e8b\u5531\u6210\u6708\u8272",
			"\u5531\u5230\u5929\u660e\u8fd8\u6709\u9999",
		}, "\n")
	default:
		return strings.Join([]string{
			scene + "\u5165\u8fdc\u5c71",
			"\u6e05\u98ce\u62c2\u6545\u5173",
			"\u4e00\u5ff5\u968f\u4e91\u8d77",
			"\u65b0\u8bd7\u843d\u7b14\u95f4",
		}, "\n")
	}
}

func buildReverseCreationPrompt(sourceType, sourceText, imageURL, workType, style, title string) string {
	parts := []string{
		"Task: reverse-create a Chinese literary draft from user source.",
		"Source type: " + sourceType,
		"Output work_type: " + workType,
		"Draft title: " + title,
		"Requirements: return a concise Chinese draft, keep it editable, and do not publish automatically.",
	}
	if style != "" {
		parts = append(parts, "Style: "+style)
	}
	if sourceText != "" {
		parts = append(parts, "Source text:\n"+sourceText)
	}
	if imageURL != "" {
		parts = append(parts, "Image URL or note:\n"+imageURL)
	}
	return trimRunes(strings.Join(parts, "\n"), 3000)
}

func formatReverseCreationJob(job database.ReverseCreationJob) map[string]any {
	return map[string]any{
		"id":                job.ID,
		"api_key_id":        job.APIKeyID,
		"work_id":           job.WorkID,
		"status":            job.Status,
		"source_type":       job.SourceType,
		"source_text":       job.SourceText,
		"image_url":         job.ImageURL,
		"work_type":         job.WorkType,
		"style":             job.Style,
		"prompt":            job.Prompt,
		"generated_title":   job.GeneratedTitle,
		"generated_content": job.GeneratedContent,
		"error_message":     job.ErrorMessage,
		"model":             job.Model,
		"created_at":        job.CreatedAt,
		"updated_at":        job.UpdatedAt,
	}
}
