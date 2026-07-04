package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/palemoky/chinese-poetry-api/internal/config"
	"github.com/palemoky/chinese-poetry-api/internal/database"
)

// WorkImageHandler connects original works with generated image assets.
type WorkImageHandler struct {
	repo   *database.Repository
	cfg    config.ImageConfig
	client *http.Client
}

// NewWorkImageHandler creates a work-image handler.
func NewWorkImageHandler(repo *database.Repository, cfg config.ImageConfig) *WorkImageHandler {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	return &WorkImageHandler{
		repo: repo,
		cfg:  cfg,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

type generateWorkImageRequest struct {
	Prompt          string `json:"prompt"`
	Size            string `json:"size"`
	Style           string `json:"style"`
	ImageAPIKey     string `json:"image_api_key,omitempty"`
	DryRun          bool   `json:"dry_run"`
	ForceRegenerate bool   `json:"force_regenerate"`
}

// ListMediaAssets returns generated media assets for an owned work.
func (h *WorkImageHandler) ListMediaAssets(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	id, ok := parseWorkID(c)
	if !ok {
		return
	}
	items, err := h.repo.ListWorkMediaAssets(apiKeyID, id, c.DefaultQuery("asset_type", "all"), queryInt(c, "limit", 20))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusNotFound, "work not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to list media assets")
		return
	}
	data := make([]map[string]any, len(items))
	for i, item := range items {
		data[i] = formatMediaAsset(item)
	}
	respondOK(c, gin.H{"items": data})
}

// ListImageJobs returns prompt/image-generation jobs for an owned work.
func (h *WorkImageHandler) ListImageJobs(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	id, ok := parseWorkID(c)
	if !ok {
		return
	}
	items, err := h.repo.ListWorkImageJobs(apiKeyID, id, queryInt(c, "limit", 20))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusNotFound, "work not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to list image jobs")
		return
	}
	data := make([]map[string]any, len(items))
	for i, item := range items {
		data[i] = formatImageGenerationJob(item)
	}
	respondOK(c, gin.H{"items": data})
}

// Generate prepares a work-aware prompt and optionally calls the image gateway.
func (h *WorkImageHandler) Generate(c *gin.Context) {
	apiKey, ok := apiKeyFromContext(c)
	if !ok || apiKey == nil {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	id, ok := parseWorkID(c)
	if !ok {
		return
	}

	var req generateWorkImageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid image request")
		return
	}

	work, err := h.repo.GetOriginalWork(apiKey.ID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusNotFound, "work not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to get work")
		return
	}

	size := normalizeImageSize(req.Size)
	style := normalizeWorkImageStyle(req.Style)
	prompt := buildWorkImagePrompt(*work, style, req.Prompt)
	if prompt == "" {
		respondError(c, http.StatusBadRequest, "prompt is required")
		return
	}
	if len([]rune(prompt)) > 3000 {
		prompt = trimRunes(prompt, 3000)
	}

	promptRecord, err := h.repo.CreateImagePrompt(database.CreateImagePromptParams{
		WorkID:   work.ID,
		APIKeyID: apiKey.ID,
		Prompt:   prompt,
		Source:   "work_generate",
		Style:    style,
		Size:     size,
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to save image prompt")
		return
	}

	model := strings.TrimSpace(h.cfg.Model)
	quality := normalizeImageQuality(h.cfg.Quality)
	outputFormat := normalizeOutputFormat(h.cfg.OutputFormat)

	if !req.DryRun && !req.ForceRegenerate {
		cachedAsset, err := h.repo.FindCachedWorkImageAsset(apiKey.ID, work.ID, database.FindCachedWorkImageAssetParams{
			Model:        model,
			Size:         size,
			Quality:      quality,
			OutputFormat: outputFormat,
			Prompt:       prompt,
		})
		if err != nil {
			respondError(c, http.StatusInternalServerError, "failed to check cached image asset")
			return
		}
		if cachedAsset != nil {
			job, err := h.repo.CreateImageGenerationJob(database.CreateImageGenerationJobParams{
				WorkID:       work.ID,
				APIKeyID:     apiKey.ID,
				Status:       database.ImageJobStatusPending,
				Prompt:       prompt,
				Style:        style,
				Size:         size,
				Model:        model,
				Quality:      quality,
				OutputFormat: outputFormat,
			})
			if err != nil {
				respondError(c, http.StatusInternalServerError, "failed to create cached image job")
				return
			}
			assetID := cachedAsset.ID
			completedJob, err := h.repo.CompleteImageGenerationJob(job.ID, &assetID)
			if err != nil {
				respondError(c, http.StatusInternalServerError, "failed to complete cached image job")
				return
			}
			respondOK(c, gin.H{
				"cached":      true,
				"image_url":   mediaAssetImageURL(*cachedAsset),
				"b64_json":    cachedAsset.B64JSON,
				"credit_cost": 0,
				"prompt":      formatImagePrompt(*promptRecord),
				"job":         formatImageGenerationJob(*completedJob),
				"asset":       formatMediaAsset(*cachedAsset),
			})
			return
		}
	}

	initialStatus := database.ImageJobStatusPending
	if req.DryRun {
		initialStatus = database.ImageJobStatusPromptReady
	}
	job, err := h.repo.CreateImageGenerationJob(database.CreateImageGenerationJobParams{
		WorkID:       work.ID,
		APIKeyID:     apiKey.ID,
		Status:       initialStatus,
		Prompt:       prompt,
		Style:        style,
		Size:         size,
		Model:        model,
		Quality:      quality,
		OutputFormat: outputFormat,
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to create image job")
		return
	}

	if req.DryRun {
		wallet, _ := h.repo.GetOrCreateCreditWallet(apiKey.ID, imageInitialCredits(h.cfg))
		respondOK(c, gin.H{
			"dry_run":     true,
			"credit_cost": 0,
			"credits":     formatCreditWallet(wallet),
			"prompt":      formatImagePrompt(*promptRecord),
			"job":         formatImageGenerationJob(*job),
		})
		return
	}

	imageGatewayKey := strings.TrimSpace(h.cfg.APIKey)
	requestImageKey := strings.TrimSpace(c.GetHeader("X-Image-API-Key"))
	if requestImageKey == "" {
		requestImageKey = strings.TrimSpace(req.ImageAPIKey)
	}
	if requestImageKey != "" {
		imageGatewayKey = requestImageKey
	}
	if imageGatewayKey == "" {
		failedJob, _ := h.repo.FailImageGenerationJob(job.ID, "image_config_missing")
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "image generation is not configured",
			"code":    "image_config_missing",
			"message": "服务端未配置 IMAGE_API_KEY，且本次请求未提供 X-Image-API-Key；本次不会消耗生图额度。",
			"data": gin.H{
				"prompt": formatImagePrompt(*promptRecord),
				"job":    formatNullableImageGenerationJob(failedJob, *job),
			},
		})
		return
	}

	todayUsage, err := h.repo.GetAPIKeyUsageCount(apiKey.ID, time.Now().UTC().Format("2006-01-02"))
	if err != nil {
		_, _ = h.repo.FailImageGenerationJob(job.ID, "failed to read image usage")
		respondError(c, http.StatusInternalServerError, "failed to read image usage")
		return
	}
	if apiKey.DailyLimit > 0 && todayUsage >= apiKey.DailyLimit {
		failedJob, _ := h.repo.FailImageGenerationJob(job.ID, "daily api quota exceeded")
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":             "daily api quota exceeded",
			"recharge_endpoint": "/api/v1/billing/qanlo/recharge-session",
			"data": gin.H{
				"prompt": formatImagePrompt(*promptRecord),
				"job":    formatNullableImageGenerationJob(failedJob, *job),
			},
		})
		return
	}

	creditCost := imageCreditCost(h.cfg)
	wallet, err := h.repo.EnsureCreditsAvailable(apiKey.ID, creditCost, imageInitialCredits(h.cfg))
	if err != nil {
		failedJob, _ := h.repo.FailImageGenerationJob(job.ID, "insufficient image credits")
		if errors.Is(err, database.ErrInsufficientCredits) {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error":             "insufficient image credits",
				"recharge_endpoint": "/api/v1/billing/qanlo/recharge-session",
				"data": gin.H{
					"credit_cost": creditCost,
					"credits":     formatCreditWallet(wallet),
					"prompt":      formatImagePrompt(*promptRecord),
					"job":         formatNullableImageGenerationJob(failedJob, *job),
				},
			})
			return
		}
		respondError(c, http.StatusInternalServerError, "failed to read image credits")
		return
	}

	upstreamReq := openAIImageRequest{
		Model:          model,
		Prompt:         prompt,
		Size:           size,
		N:              1,
		Quality:        quality,
		OutputFormat:   outputFormat,
		ResponseFormat: "b64_json",
	}
	body, err := json.Marshal(upstreamReq)
	if err != nil {
		_, _ = h.repo.FailImageGenerationJob(job.ID, "failed to build image request")
		respondError(c, http.StatusInternalServerError, "failed to build image request")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.client.Timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, imageGenerationsURL(h.cfg.BaseURL), bytes.NewReader(body))
	if err != nil {
		_, _ = h.repo.FailImageGenerationJob(job.ID, "failed to build upstream request")
		respondError(c, http.StatusInternalServerError, "failed to build upstream request")
		return
	}
	httpReq.Header.Set("Authorization", "Bearer "+imageGatewayKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	res, err := h.client.Do(httpReq)
	if err != nil {
		failedJob, _ := h.repo.FailImageGenerationJob(job.ID, err.Error())
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "image gateway request failed",
			"message": err.Error(),
			"data": gin.H{
				"prompt": formatImagePrompt(*promptRecord),
				"job":    formatNullableImageGenerationJob(failedJob, *job),
			},
		})
		return
	}
	defer res.Body.Close()

	resBody, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		_, _ = h.repo.FailImageGenerationJob(job.ID, "failed to read image gateway response")
		respondError(c, http.StatusBadGateway, "failed to read image gateway response")
		return
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		message := safeUpstreamMessage(resBody)
		failedJob, _ := h.repo.FailImageGenerationJob(job.ID, message)
		c.JSON(http.StatusBadGateway, gin.H{
			"error":           "image gateway returned error",
			"upstream_status": res.StatusCode,
			"message":         message,
			"data": gin.H{
				"prompt": formatImagePrompt(*promptRecord),
				"job":    formatNullableImageGenerationJob(failedJob, *job),
			},
		})
		return
	}

	var upstream openAIImagesResponse
	if err := json.Unmarshal(resBody, &upstream); err != nil {
		_, _ = h.repo.FailImageGenerationJob(job.ID, "invalid image gateway response")
		respondError(c, http.StatusBadGateway, "invalid image gateway response")
		return
	}
	if len(upstream.Data) == 0 {
		_, _ = h.repo.FailImageGenerationJob(job.ID, "image gateway returned no image")
		respondError(c, http.StatusBadGateway, "image gateway returned no image")
		return
	}

	item := upstream.Data[0]
	finalFormat := normalizeOutputFormat(firstImageNonEmpty(item.OutputFormat, upstreamReq.OutputFormat, "png"))
	imageURL := strings.TrimSpace(item.URL)
	b64 := strings.TrimSpace(item.B64JSON)
	if imageURL == "" && b64 != "" {
		imageURL = "data:image/" + finalFormat + ";base64," + b64
	}
	if imageURL == "" {
		_, _ = h.repo.FailImageGenerationJob(job.ID, "image gateway returned empty image")
		respondError(c, http.StatusBadGateway, "image gateway returned empty image")
		return
	}

	assetURL := imageURL
	assetB64 := b64
	var stored *storedImageAsset
	if b64 != "" {
		stored, err = storeWorkImageB64Asset(h.cfg, apiKey.ID, work.ID, finalFormat, b64)
		if err != nil {
			_, _ = h.repo.FailImageGenerationJob(job.ID, "failed to store image asset")
			respondError(c, http.StatusInternalServerError, "failed to store image asset")
			return
		}
		if stored != nil {
			assetURL = stored.URL
			assetB64 = ""
		}
	}

	assetParams := database.CreateMediaAssetParams{
		WorkID:        work.ID,
		APIKeyID:      apiKey.ID,
		AssetType:     database.MediaAssetTypeImage,
		Source:        database.MediaAssetSourceGenerated,
		URL:           assetURL,
		B64JSON:       assetB64,
		MimeType:      "image/" + finalFormat,
		Model:         model,
		Size:          size,
		Quality:       quality,
		OutputFormat:  finalFormat,
		Prompt:        prompt,
		RevisedPrompt: item.RevisedPrompt,
		CreditCost:    creditCost,
		Visibility:    work.Visibility,
	}
	if stored != nil {
		assetParams.StorageProvider = stored.StorageProvider
		assetParams.StorageKey = stored.StorageKey
		assetParams.FilePath = stored.FilePath
		assetParams.ByteSize = stored.ByteSize
		assetParams.ChecksumSHA256 = stored.ChecksumSHA256
	}
	asset, err := h.repo.CreateMediaAsset(assetParams)
	if err != nil {
		_, _ = h.repo.FailImageGenerationJob(job.ID, "failed to save media asset")
		respondError(c, http.StatusInternalServerError, "failed to save media asset")
		return
	}

	usage, err := h.repo.RecordAPIKeyUsage(apiKey)
	if err != nil {
		_, _ = h.repo.FailImageGenerationJob(job.ID, "failed to record image usage")
		if errors.Is(err, database.ErrAPIQuotaExceeded) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":             "daily api quota exceeded",
				"recharge_endpoint": "/api/v1/billing/qanlo/recharge-session",
			})
			return
		}
		respondError(c, http.StatusInternalServerError, "failed to record image usage")
		return
	}

	workID := work.ID
	assetIDForCredits := asset.ID
	jobIDForCredits := job.ID
	wallet, _, err = h.repo.SpendCredits(database.SpendCreditsParams{
		APIKeyID:       apiKey.ID,
		WorkID:         &workID,
		MediaAssetID:   &assetIDForCredits,
		JobID:          &jobIDForCredits,
		Amount:         creditCost,
		Reason:         "work_image_generate",
		IdempotencyKey: "image_job:" + formatID(job.ID),
		InitialBalance: imageInitialCredits(h.cfg),
	})
	if err != nil {
		_, _ = h.repo.FailImageGenerationJob(job.ID, "failed to spend image credits")
		if errors.Is(err, database.ErrInsufficientCredits) {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error":             "insufficient image credits",
				"recharge_endpoint": "/api/v1/billing/qanlo/recharge-session",
			})
			return
		}
		respondError(c, http.StatusInternalServerError, "failed to spend image credits")
		return
	}

	assetID := asset.ID
	job, err = h.repo.CompleteImageGenerationJob(job.ID, &assetID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to complete image job")
		return
	}

	c.Header("X-API-Key-Usage-Today", strconv.Itoa(usage))
	if apiKey.DailyLimit > 0 {
		c.Header("X-API-Key-Daily-Limit", strconv.Itoa(apiKey.DailyLimit))
	}
	if wallet != nil {
		c.Header("X-API-Key-Credits-Balance", strconv.Itoa(wallet.Balance))
	}
	c.Header("X-Image-Credit-Cost", strconv.Itoa(creditCost))
	c.Set("api_key_billable", true)

	respondOK(c, gin.H{
		"image_url":   assetURL,
		"b64_json":    b64,
		"credit_cost": creditCost,
		"credits":     formatCreditWallet(wallet),
		"prompt":      formatImagePrompt(*promptRecord),
		"job":         formatImageGenerationJob(*job),
		"asset":       formatMediaAsset(*asset),
	})
}

func normalizeWorkImageStyle(style string) string {
	style = strings.TrimSpace(style)
	if style == "" {
		return "古风水墨"
	}
	return trimRunes(style, 80)
}

func buildWorkImagePrompt(work database.OriginalWork, style, extraPrompt string) string {
	base := strings.TrimSpace(work.ImagePrompt)
	if base == "" {
		base = "根据原创作品《" + work.Title + "》创作一幅" + style + "诗画作品。"
	} else {
		base = "根据以下画面意向创作一幅" + style + "诗画作品：" + base + "。"
	}
	content := strings.TrimSpace(work.Content)
	extra := strings.TrimSpace(extraPrompt)
	parts := []string{
		base,
		"诗词全文：\n" + content,
		"画面要求：整体像一幅完整的中国古典水墨/工笔融合画，不要像素材拼贴，不要像现代 UI 海报。",
		"文字要求：如需出现诗句，请把诗句作为画中题诗、题跋或卷轴书法自然融入留白处，与画面笔墨同源，不要后贴文字框。",
		"构图要求：保留呼吸感和留白，主体、远景、题诗和印章统一在同一幅画里。",
	}
	if extra != "" {
		parts = append(parts, "本次补充要求："+extra)
	}
	return trimRunes(strings.Join(parts, "\n"), 3000)
}

func trimRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit])
}

func formatImagePrompt(prompt database.ImagePrompt) map[string]any {
	return map[string]any{
		"id":         prompt.ID,
		"work_id":    prompt.WorkID,
		"api_key_id": prompt.APIKeyID,
		"prompt":     prompt.Prompt,
		"source":     prompt.Source,
		"style":      prompt.Style,
		"size":       prompt.Size,
		"created_at": prompt.CreatedAt,
	}
}

func formatImageGenerationJob(job database.ImageGenerationJob) map[string]any {
	return map[string]any{
		"id":             job.ID,
		"work_id":        job.WorkID,
		"api_key_id":     job.APIKeyID,
		"status":         job.Status,
		"prompt":         job.Prompt,
		"style":          job.Style,
		"size":           job.Size,
		"model":          job.Model,
		"quality":        job.Quality,
		"output_format":  job.OutputFormat,
		"error_message":  job.ErrorMessage,
		"media_asset_id": job.MediaAssetID,
		"created_at":     job.CreatedAt,
		"updated_at":     job.UpdatedAt,
	}
}

func formatNullableImageGenerationJob(value *database.ImageGenerationJob, fallback database.ImageGenerationJob) map[string]any {
	if value == nil {
		return formatImageGenerationJob(fallback)
	}
	return formatImageGenerationJob(*value)
}

func imageCreditCost(cfg config.ImageConfig) int {
	if cfg.CreditCost < 0 {
		return 0
	}
	if cfg.CreditCost == 0 {
		return 1
	}
	return cfg.CreditCost
}

func imageInitialCredits(cfg config.ImageConfig) int {
	if cfg.InitialCredits < 0 {
		return 0
	}
	if cfg.InitialCredits == 0 {
		return 20
	}
	return cfg.InitialCredits
}

func formatCreditWallet(wallet *database.CreditWallet) map[string]any {
	if wallet == nil {
		return map[string]any{"balance": 0}
	}
	return map[string]any{
		"id":            wallet.ID,
		"api_key_id":    wallet.APIKeyID,
		"balance":       wallet.Balance,
		"total_granted": wallet.TotalGranted,
		"total_spent":   wallet.TotalSpent,
		"updated_at":    wallet.UpdatedAt,
	}
}

func formatMediaAsset(asset database.MediaAsset) map[string]any {
	return map[string]any{
		"id":               asset.ID,
		"work_id":          asset.WorkID,
		"api_key_id":       asset.APIKeyID,
		"asset_type":       asset.AssetType,
		"source":           asset.Source,
		"url":              asset.URL,
		"b64_json":         asset.B64JSON,
		"mime_type":        asset.MimeType,
		"model":            asset.Model,
		"size":             asset.Size,
		"quality":          asset.Quality,
		"output_format":    asset.OutputFormat,
		"prompt":           asset.Prompt,
		"revised_prompt":   asset.RevisedPrompt,
		"storage_provider": asset.StorageProvider,
		"storage_key":      asset.StorageKey,
		"file_path":        asset.FilePath,
		"byte_size":        asset.ByteSize,
		"checksum_sha256":  asset.ChecksumSHA256,
		"credit_cost":      asset.CreditCost,
		"visibility":       asset.Visibility,
		"created_at":       asset.CreatedAt,
	}
}

func mediaAssetImageURL(asset database.MediaAsset) string {
	if strings.TrimSpace(asset.URL) != "" {
		return asset.URL
	}
	b64 := strings.TrimSpace(asset.B64JSON)
	if b64 == "" {
		return ""
	}
	mimeType := strings.TrimSpace(asset.MimeType)
	if mimeType == "" {
		format := normalizeOutputFormat(asset.OutputFormat)
		if format == "" {
			format = "png"
		}
		mimeType = "image/" + format
	}
	return "data:" + mimeType + ";base64," + b64
}
