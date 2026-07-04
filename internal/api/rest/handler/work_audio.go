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

// WorkAudioHandler connects original works with recitation and music assets.
type WorkAudioHandler struct {
	repo     *database.Repository
	cfg      config.AudioConfig
	mediaCfg config.ImageConfig
	client   *http.Client
}

// NewWorkAudioHandler creates a work-audio handler.
func NewWorkAudioHandler(repo *database.Repository, cfg config.AudioConfig, mediaCfg config.ImageConfig) *WorkAudioHandler {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	return &WorkAudioHandler{
		repo:     repo,
		cfg:      cfg,
		mediaCfg: mediaCfg,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

type generateWorkAudioRequest struct {
	Voice           string `json:"voice"`
	Style           string `json:"style"`
	BackgroundStyle string `json:"background_style"`
	Prompt          string `json:"prompt"`
	OutputFormat    string `json:"output_format"`
	AudioAPIKey     string `json:"audio_api_key,omitempty"`
	DryRun          bool   `json:"dry_run"`
}

type generateWorkMusicRequest struct {
	Mode       string `json:"mode"`
	MusicStyle string `json:"music_style"`
	Prompt     string `json:"prompt"`
	DryRun     bool   `json:"dry_run"`
}

type openAIAudioSpeechRequest struct {
	Model          string `json:"model"`
	Input          string `json:"input"`
	Voice          string `json:"voice"`
	ResponseFormat string `json:"response_format,omitempty"`
	Instructions   string `json:"instructions,omitempty"`
}

// ListAudioJobs returns recitation-generation jobs for an owned work.
func (h *WorkAudioHandler) ListAudioJobs(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	id, ok := parseWorkID(c)
	if !ok {
		return
	}
	items, err := h.repo.ListWorkAudioJobs(apiKeyID, id, queryInt(c, "limit", 20))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusNotFound, "work not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to list audio jobs")
		return
	}
	data := make([]map[string]any, len(items))
	for i, item := range items {
		data[i] = formatAudioGenerationJob(item)
	}
	respondOK(c, gin.H{"items": data})
}

// ListMusicJobs returns background-music draft jobs for an owned work.
func (h *WorkAudioHandler) ListMusicJobs(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	id, ok := parseWorkID(c)
	if !ok {
		return
	}
	items, err := h.repo.ListWorkMusicJobs(apiKeyID, id, queryInt(c, "limit", 20))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusNotFound, "work not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to list music jobs")
		return
	}
	data := make([]map[string]any, len(items))
	for i, item := range items {
		data[i] = formatMusicGenerationJob(item)
	}
	respondOK(c, gin.H{"items": data})
}

// GenerateAudio prepares and optionally calls an OpenAI-compatible TTS endpoint.
func (h *WorkAudioHandler) GenerateAudio(c *gin.Context) {
	apiKey, ok := apiKeyFromContext(c)
	if !ok || apiKey == nil {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	id, ok := parseWorkID(c)
	if !ok {
		return
	}

	var req generateWorkAudioRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid audio request")
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

	voice := normalizeAudioVoice(firstImageNonEmpty(req.Voice, h.cfg.Voice))
	style := normalizeAudioStyle(req.Style)
	backgroundStyle := normalizeBackgroundStyle(req.BackgroundStyle)
	outputFormat := normalizeAudioFormat(firstImageNonEmpty(req.OutputFormat, h.cfg.OutputFormat))
	model := normalizeAudioModel(h.cfg.Model)
	input := buildWorkAudioInput(*work, req.Prompt)
	instructions := buildWorkAudioInstructions(*work, style, backgroundStyle)
	if input == "" {
		respondError(c, http.StatusBadRequest, "audio input is required")
		return
	}
	if len([]rune(input)) > 3000 {
		input = trimRunes(input, 3000)
	}
	prompt := trimRunes(strings.Join([]string{instructions, "Recitation text:\n" + input}, "\n"), 3000)

	initialStatus := database.ImageJobStatusPending
	if req.DryRun {
		initialStatus = database.ImageJobStatusPromptReady
	}
	job, err := h.repo.CreateAudioGenerationJob(database.CreateAudioGenerationJobParams{
		WorkID:          work.ID,
		APIKeyID:        apiKey.ID,
		Status:          initialStatus,
		Prompt:          prompt,
		Voice:           voice,
		Style:           style,
		BackgroundStyle: backgroundStyle,
		Model:           model,
		OutputFormat:    outputFormat,
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to create audio job")
		return
	}

	if req.DryRun {
		wallet, _ := h.repo.GetOrCreateCreditWallet(apiKey.ID, audioInitialCredits(h.cfg))
		respondOK(c, gin.H{
			"dry_run":      true,
			"credit_cost":  0,
			"credits":      formatCreditWallet(wallet),
			"input":        input,
			"instructions": instructions,
			"job":          formatAudioGenerationJob(*job),
		})
		return
	}

	audioGatewayKey := strings.TrimSpace(h.cfg.APIKey)
	requestAudioKey := strings.TrimSpace(c.GetHeader("X-Audio-API-Key"))
	if requestAudioKey == "" {
		requestAudioKey = strings.TrimSpace(req.AudioAPIKey)
	}
	if requestAudioKey != "" {
		audioGatewayKey = requestAudioKey
	}
	if audioGatewayKey == "" {
		failedJob, _ := h.repo.FailAudioGenerationJob(job.ID, "audio_config_missing")
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "audio generation is not configured",
			"code":    "audio_config_missing",
			"message": "Server AUDIO_API_KEY is not configured and this request did not provide X-Audio-API-Key; no audio credits were spent.",
			"data": gin.H{
				"input":        input,
				"instructions": instructions,
				"job":          formatNullableAudioGenerationJob(failedJob, *job),
			},
		})
		return
	}

	todayUsage, err := h.repo.GetAPIKeyUsageCount(apiKey.ID, time.Now().UTC().Format("2006-01-02"))
	if err != nil {
		_, _ = h.repo.FailAudioGenerationJob(job.ID, "failed to read audio usage")
		respondError(c, http.StatusInternalServerError, "failed to read audio usage")
		return
	}
	if apiKey.DailyLimit > 0 && todayUsage >= apiKey.DailyLimit {
		failedJob, _ := h.repo.FailAudioGenerationJob(job.ID, "daily api quota exceeded")
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":             "daily api quota exceeded",
			"recharge_endpoint": "/api/v1/billing/qanlo/recharge-session",
			"data": gin.H{
				"job": formatNullableAudioGenerationJob(failedJob, *job),
			},
		})
		return
	}

	creditCost := audioCreditCost(h.cfg)
	wallet, err := h.repo.EnsureCreditsAvailable(apiKey.ID, creditCost, audioInitialCredits(h.cfg))
	if err != nil {
		failedJob, _ := h.repo.FailAudioGenerationJob(job.ID, "insufficient audio credits")
		if errors.Is(err, database.ErrInsufficientCredits) {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error":             "insufficient audio credits",
				"recharge_endpoint": "/api/v1/billing/qanlo/recharge-session",
				"data": gin.H{
					"credit_cost": creditCost,
					"credits":     formatCreditWallet(wallet),
					"job":         formatNullableAudioGenerationJob(failedJob, *job),
				},
			})
			return
		}
		respondError(c, http.StatusInternalServerError, "failed to read audio credits")
		return
	}

	upstreamReq := openAIAudioSpeechRequest{
		Model:          model,
		Input:          input,
		Voice:          voice,
		ResponseFormat: outputFormat,
		Instructions:   instructions,
	}
	body, err := json.Marshal(upstreamReq)
	if err != nil {
		_, _ = h.repo.FailAudioGenerationJob(job.ID, "failed to build audio request")
		respondError(c, http.StatusInternalServerError, "failed to build audio request")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.client.Timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, audioSpeechURL(h.cfg.BaseURL), bytes.NewReader(body))
	if err != nil {
		_, _ = h.repo.FailAudioGenerationJob(job.ID, "failed to build upstream request")
		respondError(c, http.StatusInternalServerError, "failed to build upstream request")
		return
	}
	httpReq.Header.Set("Authorization", "Bearer "+audioGatewayKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", audioMimeType(outputFormat))

	res, err := h.client.Do(httpReq)
	if err != nil {
		failedJob, _ := h.repo.FailAudioGenerationJob(job.ID, err.Error())
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "audio gateway request failed",
			"message": err.Error(),
			"data": gin.H{
				"job": formatNullableAudioGenerationJob(failedJob, *job),
			},
		})
		return
	}
	defer res.Body.Close()

	resBody, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		_, _ = h.repo.FailAudioGenerationJob(job.ID, "failed to read audio gateway response")
		respondError(c, http.StatusBadGateway, "failed to read audio gateway response")
		return
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		message := safeUpstreamMessage(resBody)
		failedJob, _ := h.repo.FailAudioGenerationJob(job.ID, message)
		c.JSON(http.StatusBadGateway, gin.H{
			"error":           "audio gateway returned error",
			"upstream_status": res.StatusCode,
			"message":         message,
			"data": gin.H{
				"job": formatNullableAudioGenerationJob(failedJob, *job),
			},
		})
		return
	}
	if len(resBody) == 0 {
		_, _ = h.repo.FailAudioGenerationJob(job.ID, "audio gateway returned empty audio")
		respondError(c, http.StatusBadGateway, "audio gateway returned empty audio")
		return
	}

	finalFormat := normalizeAudioFormat(firstImageNonEmpty(outputFormat, contentTypeAudioFormat(res.Header.Get("Content-Type"))))
	stored, err := storeWorkAudioBytes(h.mediaCfg, apiKey.ID, work.ID, "audio", audioFormatExt(finalFormat), resBody)
	if err != nil {
		_, _ = h.repo.FailAudioGenerationJob(job.ID, "failed to store audio asset")
		respondError(c, http.StatusInternalServerError, "failed to store audio asset")
		return
	}

	asset, err := h.repo.CreateMediaAsset(database.CreateMediaAssetParams{
		WorkID:          work.ID,
		APIKeyID:        apiKey.ID,
		AssetType:       database.MediaAssetTypeAudio,
		Source:          database.MediaAssetSourceGenerated,
		URL:             stored.URL,
		MimeType:        audioMimeType(finalFormat),
		Model:           model,
		OutputFormat:    finalFormat,
		Prompt:          prompt,
		StorageProvider: stored.StorageProvider,
		StorageKey:      stored.StorageKey,
		FilePath:        stored.FilePath,
		ByteSize:        stored.ByteSize,
		ChecksumSHA256:  stored.ChecksumSHA256,
		CreditCost:      creditCost,
		Visibility:      work.Visibility,
	})
	if err != nil {
		_, _ = h.repo.FailAudioGenerationJob(job.ID, "failed to save audio asset")
		respondError(c, http.StatusInternalServerError, "failed to save audio asset")
		return
	}

	usage, err := h.repo.RecordAPIKeyUsage(apiKey)
	if err != nil {
		_, _ = h.repo.FailAudioGenerationJob(job.ID, "failed to record audio usage")
		if errors.Is(err, database.ErrAPIQuotaExceeded) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":             "daily api quota exceeded",
				"recharge_endpoint": "/api/v1/billing/qanlo/recharge-session",
			})
			return
		}
		respondError(c, http.StatusInternalServerError, "failed to record audio usage")
		return
	}

	workID := work.ID
	assetIDForCredits := asset.ID
	wallet, _, err = h.repo.SpendCredits(database.SpendCreditsParams{
		APIKeyID:       apiKey.ID,
		WorkID:         &workID,
		MediaAssetID:   &assetIDForCredits,
		Amount:         creditCost,
		Reason:         "work_audio_generate",
		IdempotencyKey: "audio_job:" + formatID(job.ID),
		InitialBalance: audioInitialCredits(h.cfg),
	})
	if err != nil {
		_, _ = h.repo.FailAudioGenerationJob(job.ID, "failed to spend audio credits")
		if errors.Is(err, database.ErrInsufficientCredits) {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error":             "insufficient audio credits",
				"recharge_endpoint": "/api/v1/billing/qanlo/recharge-session",
			})
			return
		}
		respondError(c, http.StatusInternalServerError, "failed to spend audio credits")
		return
	}

	assetID := asset.ID
	job, err = h.repo.CompleteAudioGenerationJob(job.ID, &assetID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to complete audio job")
		return
	}

	c.Header("X-API-Key-Usage-Today", strconv.Itoa(usage))
	if apiKey.DailyLimit > 0 {
		c.Header("X-API-Key-Daily-Limit", strconv.Itoa(apiKey.DailyLimit))
	}
	if wallet != nil {
		c.Header("X-API-Key-Credits-Balance", strconv.Itoa(wallet.Balance))
	}
	c.Header("X-Audio-Credit-Cost", strconv.Itoa(creditCost))
	c.Set("api_key_billable", true)

	respondOK(c, gin.H{
		"audio_url":    stored.URL,
		"credit_cost":  creditCost,
		"credits":      formatCreditWallet(wallet),
		"input":        input,
		"instructions": instructions,
		"job":          formatAudioGenerationJob(*job),
		"asset":        formatMediaAsset(*asset),
	})
}

// GenerateMusic creates a local structured background-music draft asset for a work.
func (h *WorkAudioHandler) GenerateMusic(c *gin.Context) {
	apiKey, ok := apiKeyFromContext(c)
	if !ok || apiKey == nil {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	id, ok := parseWorkID(c)
	if !ok {
		return
	}

	var req generateWorkMusicRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid music request")
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

	mode := normalizeMusicMode(req.Mode)
	musicStyle := normalizeMusicStyle(req.MusicStyle)
	model := "stage4-music-draft"
	prompt := buildWorkMusicDraftPrompt(*work, musicStyle, mode, req.Prompt)
	if prompt == "" {
		respondError(c, http.StatusBadRequest, "music prompt is required")
		return
	}

	initialStatus := database.ImageJobStatusPending
	if req.DryRun {
		initialStatus = database.ImageJobStatusPromptReady
	}
	job, err := h.repo.CreateMusicGenerationJob(database.CreateMusicGenerationJobParams{
		WorkID:       work.ID,
		APIKeyID:     apiKey.ID,
		Status:       initialStatus,
		Prompt:       prompt,
		MusicStyle:   musicStyle,
		Mode:         mode,
		Model:        model,
		OutputFormat: "json",
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to create music job")
		return
	}

	if req.DryRun {
		wallet, _ := h.repo.GetOrCreateCreditWallet(apiKey.ID, audioInitialCredits(h.cfg))
		respondOK(c, gin.H{
			"dry_run":     true,
			"credit_cost": 0,
			"credits":     formatCreditWallet(wallet),
			"prompt":      prompt,
			"job":         formatMusicGenerationJob(*job),
		})
		return
	}

	creditCost := audioMusicCreditCost(h.cfg)
	wallet, err := h.repo.EnsureCreditsAvailable(apiKey.ID, creditCost, audioInitialCredits(h.cfg))
	if err != nil {
		failedJob, _ := h.repo.FailMusicGenerationJob(job.ID, "insufficient music credits")
		if errors.Is(err, database.ErrInsufficientCredits) {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error":             "insufficient music credits",
				"recharge_endpoint": "/api/v1/billing/qanlo/recharge-session",
				"data": gin.H{
					"credit_cost": creditCost,
					"credits":     formatCreditWallet(wallet),
					"job":         formatNullableMusicGenerationJob(failedJob, *job),
				},
			})
			return
		}
		respondError(c, http.StatusInternalServerError, "failed to read music credits")
		return
	}

	draft := map[string]any{
		"work_id":       work.ID,
		"title":         work.Title,
		"work_type":     work.WorkType,
		"mode":          mode,
		"music_style":   musicStyle,
		"model":         model,
		"prompt":        prompt,
		"tempo":         musicDraftTempo(mode, musicStyle),
		"instruments":   musicDraftInstruments(musicStyle),
		"arrangement":   []string{"4 bars of sparse intro", "main motif follows each line", "fade out after the final line"},
		"mixing_notes":  "Keep background volume low and leave space for recitation voice.",
		"content_lines": normalizeLinesForDraft(work.Content),
		"created_at":    time.Now().UTC().Format(time.RFC3339),
	}
	draftBytes, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		_, _ = h.repo.FailMusicGenerationJob(job.ID, "failed to build music draft")
		respondError(c, http.StatusInternalServerError, "failed to build music draft")
		return
	}
	stored, err := storeWorkAudioBytes(h.mediaCfg, apiKey.ID, work.ID, "music", "json", draftBytes)
	if err != nil {
		_, _ = h.repo.FailMusicGenerationJob(job.ID, "failed to store music draft")
		respondError(c, http.StatusInternalServerError, "failed to store music draft")
		return
	}

	asset, err := h.repo.CreateMediaAsset(database.CreateMediaAssetParams{
		WorkID:          work.ID,
		APIKeyID:        apiKey.ID,
		AssetType:       database.MediaAssetTypeMusic,
		Source:          database.MediaAssetSourceGenerated,
		URL:             stored.URL,
		MimeType:        "application/json",
		Model:           model,
		OutputFormat:    "json",
		Prompt:          prompt,
		StorageProvider: stored.StorageProvider,
		StorageKey:      stored.StorageKey,
		FilePath:        stored.FilePath,
		ByteSize:        stored.ByteSize,
		ChecksumSHA256:  stored.ChecksumSHA256,
		CreditCost:      creditCost,
		Visibility:      work.Visibility,
	})
	if err != nil {
		_, _ = h.repo.FailMusicGenerationJob(job.ID, "failed to save music asset")
		respondError(c, http.StatusInternalServerError, "failed to save music asset")
		return
	}

	if creditCost > 0 {
		workID := work.ID
		assetIDForCredits := asset.ID
		wallet, _, err = h.repo.SpendCredits(database.SpendCreditsParams{
			APIKeyID:       apiKey.ID,
			WorkID:         &workID,
			MediaAssetID:   &assetIDForCredits,
			Amount:         creditCost,
			Reason:         "work_music_generate",
			IdempotencyKey: "music_job:" + formatID(job.ID),
			InitialBalance: audioInitialCredits(h.cfg),
		})
		if err != nil {
			_, _ = h.repo.FailMusicGenerationJob(job.ID, "failed to spend music credits")
			if errors.Is(err, database.ErrInsufficientCredits) {
				c.JSON(http.StatusPaymentRequired, gin.H{
					"error":             "insufficient music credits",
					"recharge_endpoint": "/api/v1/billing/qanlo/recharge-session",
				})
				return
			}
			respondError(c, http.StatusInternalServerError, "failed to spend music credits")
			return
		}
	}

	assetID := asset.ID
	job, err = h.repo.CompleteMusicGenerationJob(job.ID, &assetID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to complete music job")
		return
	}
	if wallet != nil {
		c.Header("X-API-Key-Credits-Balance", strconv.Itoa(wallet.Balance))
	}
	c.Header("X-Music-Credit-Cost", strconv.Itoa(creditCost))

	respondOK(c, gin.H{
		"music_url":   stored.URL,
		"credit_cost": creditCost,
		"credits":     formatCreditWallet(wallet),
		"prompt":      prompt,
		"job":         formatMusicGenerationJob(*job),
		"asset":       formatMediaAsset(*asset),
		"draft":       draft,
	})
}

func audioSpeechURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "https://qanlo.com/openai/v1"
	}
	if strings.HasSuffix(base, "/audio/speech") {
		return base
	}
	return base + "/audio/speech"
}

func normalizeAudioModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return "gpt-4o-mini-tts"
	}
	return trimRunes(model, 80)
}

func normalizeAudioVoice(voice string) string {
	voice = strings.ToLower(strings.TrimSpace(voice))
	if voice == "" {
		return "alloy"
	}
	return trimRunes(voice, 80)
}

func normalizeAudioStyle(style string) string {
	style = strings.TrimSpace(style)
	if style == "" {
		return "classical recitation"
	}
	return trimRunes(style, 80)
}

func normalizeBackgroundStyle(style string) string {
	style = strings.TrimSpace(style)
	if style == "" {
		return "light ambience"
	}
	return trimRunes(style, 120)
}

func normalizeMusicStyle(style string) string {
	style = strings.TrimSpace(style)
	if style == "" {
		return "guqin calm"
	}
	return trimRunes(style, 120)
}

func normalizeMusicMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "recitation", "chant":
		return "recitation"
	case "song", "melody":
		return "song"
	default:
		return "background"
	}
}

func normalizeAudioFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "wav":
		return "wav"
	case "opus":
		return "opus"
	case "flac":
		return "flac"
	case "aac":
		return "aac"
	case "mp3", "mpeg":
		return "mp3"
	default:
		return "mp3"
	}
}

func audioFormatExt(format string) string {
	format = normalizeAudioFormat(format)
	if format == "opus" {
		return "opus"
	}
	return format
}

func audioMimeType(format string) string {
	switch normalizeAudioFormat(format) {
	case "wav":
		return "audio/wav"
	case "opus":
		return "audio/ogg"
	case "flac":
		return "audio/flac"
	case "aac":
		return "audio/aac"
	default:
		return "audio/mpeg"
	}
}

func contentTypeAudioFormat(contentType string) string {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.Contains(contentType, "wav"):
		return "wav"
	case strings.Contains(contentType, "ogg") || strings.Contains(contentType, "opus"):
		return "opus"
	case strings.Contains(contentType, "flac"):
		return "flac"
	case strings.Contains(contentType, "aac"):
		return "aac"
	case strings.Contains(contentType, "mpeg") || strings.Contains(contentType, "mp3"):
		return "mp3"
	default:
		return ""
	}
}

func audioCreditCost(cfg config.AudioConfig) int {
	if cfg.CreditCost < 0 {
		return 0
	}
	if cfg.CreditCost == 0 {
		return 1
	}
	return cfg.CreditCost
}

func audioMusicCreditCost(cfg config.AudioConfig) int {
	if cfg.MusicCreditCost < 0 {
		return 0
	}
	return cfg.MusicCreditCost
}

func audioInitialCredits(cfg config.AudioConfig) int {
	if cfg.InitialCredits < 0 {
		return 0
	}
	if cfg.InitialCredits == 0 {
		return 20
	}
	return cfg.InitialCredits
}

func buildWorkAudioInput(work database.OriginalWork, extraPrompt string) string {
	parts := []string{}
	title := strings.TrimSpace(work.Title)
	if title != "" {
		parts = append(parts, title)
	}
	content := strings.TrimSpace(work.Content)
	if content != "" {
		parts = append(parts, content)
	}
	extra := strings.TrimSpace(extraPrompt)
	if extra != "" {
		parts = append(parts, "Extra note: "+extra)
	}
	return trimRunes(strings.Join(parts, "\n"), 3000)
}

func buildWorkAudioInstructions(work database.OriginalWork, style, backgroundStyle string) string {
	workType := strings.TrimSpace(work.WorkType)
	if workType == "" {
		workType = "poem"
	}
	parts := []string{
		"Generate a Chinese recitation audio for this original " + workType + ".",
		"Style: " + normalizeAudioStyle(style) + ".",
		"Background: " + normalizeBackgroundStyle(backgroundStyle) + ".",
		"Requirements: clear pronunciation, natural pauses, classical rhythm, no extra explanation.",
	}
	return trimRunes(strings.Join(parts, "\n"), 1200)
}

func buildWorkMusicDraftPrompt(work database.OriginalWork, musicStyle, mode, extraPrompt string) string {
	parts := []string{
		"Create a music/background draft for original work: " + strings.TrimSpace(work.Title) + ".",
		"Mode: " + normalizeMusicMode(mode) + ".",
		"Music style: " + normalizeMusicStyle(musicStyle) + ".",
		"Content:\n" + strings.TrimSpace(work.Content),
		"Requirements: provide rhythm, instruments, sections, and mixing notes for a later music model.",
	}
	if extra := strings.TrimSpace(extraPrompt); extra != "" {
		parts = append(parts, "Extra requirement: "+extra)
	}
	return trimRunes(strings.Join(parts, "\n"), 3000)
}

func normalizeLinesForDraft(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	raw := strings.FieldsFunc(content, func(r rune) bool { return r == '\n' || r == ';' || r == '.' })
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func musicDraftTempo(mode, style string) string {
	text := strings.ToLower(mode + " " + style)
	switch {
	case strings.Contains(text, "song") || strings.Contains(text, "melody"):
		return "72-88 BPM, suitable for light melody"
	case strings.Contains(text, "solemn") || strings.Contains(text, "sad") || strings.Contains(text, "night"):
		return "48-60 BPM, slow with space"
	default:
		return "56-72 BPM, steady background"
	}
}

func musicDraftInstruments(style string) []string {
	style = strings.ToLower(strings.TrimSpace(style))
	items := []string{"guqin", "xiao", "low ambient pad"}
	if strings.Contains(style, "pipa") {
		items = []string{"pipa", "guqin", "light percussion"}
	}
	if strings.Contains(style, "flute") || strings.Contains(style, "xiao") {
		items = []string{"xiao", "bamboo flute", "guqin harmonics"}
	}
	return items
}

func formatAudioGenerationJob(job database.AudioGenerationJob) map[string]any {
	return map[string]any{
		"id":               job.ID,
		"work_id":          job.WorkID,
		"api_key_id":       job.APIKeyID,
		"status":           job.Status,
		"prompt":           job.Prompt,
		"voice":            job.Voice,
		"style":            job.Style,
		"background_style": job.BackgroundStyle,
		"model":            job.Model,
		"output_format":    job.OutputFormat,
		"error_message":    job.ErrorMessage,
		"media_asset_id":   job.MediaAssetID,
		"created_at":       job.CreatedAt,
		"updated_at":       job.UpdatedAt,
	}
}

func formatNullableAudioGenerationJob(value *database.AudioGenerationJob, fallback database.AudioGenerationJob) map[string]any {
	if value == nil {
		return formatAudioGenerationJob(fallback)
	}
	return formatAudioGenerationJob(*value)
}

func formatMusicGenerationJob(job database.MusicGenerationJob) map[string]any {
	return map[string]any{
		"id":             job.ID,
		"work_id":        job.WorkID,
		"api_key_id":     job.APIKeyID,
		"status":         job.Status,
		"prompt":         job.Prompt,
		"music_style":    job.MusicStyle,
		"mode":           job.Mode,
		"model":          job.Model,
		"output_format":  job.OutputFormat,
		"error_message":  job.ErrorMessage,
		"media_asset_id": job.MediaAssetID,
		"created_at":     job.CreatedAt,
		"updated_at":     job.UpdatedAt,
	}
}

func formatNullableMusicGenerationJob(value *database.MusicGenerationJob, fallback database.MusicGenerationJob) map[string]any {
	if value == nil {
		return formatMusicGenerationJob(fallback)
	}
	return formatMusicGenerationJob(*value)
}
