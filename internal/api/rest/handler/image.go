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

	"github.com/palemoky/chinese-poetry-api/internal/config"
	"github.com/palemoky/chinese-poetry-api/internal/database"
)

// ImageHandler proxies image generation to the configured OpenAI-compatible
// gateway. The gateway API key is supplied by the user per request.
type ImageHandler struct {
	repo   *database.Repository
	cfg    config.ImageConfig
	client *http.Client
}

// NewImageHandler creates an image handler.
func NewImageHandler(repo *database.Repository, cfg config.ImageConfig) *ImageHandler {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	return &ImageHandler{
		repo: repo,
		cfg:  cfg,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

type generateImageRequest struct {
	Prompt      string `json:"prompt"`
	Size        string `json:"size"`
	ImageAPIKey string `json:"image_api_key"`
}

type openAIImageRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	Size           string `json:"size,omitempty"`
	N              int    `json:"n,omitempty"`
	Quality        string `json:"quality,omitempty"`
	OutputFormat   string `json:"output_format,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
}

type openAIImagesResponse struct {
	Created int64 `json:"created,omitempty"`
	Data    []struct {
		URL           string `json:"url,omitempty"`
		B64JSON       string `json:"b64_json,omitempty"`
		RevisedPrompt string `json:"revised_prompt,omitempty"`
		OutputFormat  string `json:"output_format,omitempty"`
	} `json:"data"`
	Error any `json:"error,omitempty"`
}

// Generate creates an image for the current poetry prompt and returns a URL or data URL.
func (h *ImageHandler) Generate(c *gin.Context) {
	apiKey, ok := apiKeyFromContext(c)
	if !ok || apiKey == nil {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}

	var req generateImageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid image request")
		return
	}

	imageAPIKey := strings.TrimSpace(c.GetHeader("X-Image-API-Key"))
	if imageAPIKey == "" {
		imageAPIKey = strings.TrimSpace(req.ImageAPIKey)
	}
	if imageAPIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "image api key required",
			"code":    "image_api_key_required",
			"message": "\u8bf7\u5148\u5728\u9875\u9762\u586b\u5199\u5e76\u4fdd\u5b58 Qanlo \u751f\u56fe API Key\uff0c\u518d\u751f\u6210\u56fe\u7247\u3002",
		})
		return
	}

	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		respondError(c, http.StatusBadRequest, "prompt is required")
		return
	}
	if len([]rune(prompt)) > 3000 {
		respondError(c, http.StatusBadRequest, "prompt is too long")
		return
	}

	todayUsage, err := h.repo.GetAPIKeyUsageCount(apiKey.ID, time.Now().UTC().Format("2006-01-02"))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to read image usage")
		return
	}
	if apiKey.DailyLimit > 0 && todayUsage >= apiKey.DailyLimit {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":             "daily api quota exceeded",
			"recharge_endpoint": "/api/v1/billing/qanlo/recharge-session",
		})
		return
	}

	size := normalizeImageSize(req.Size)
	upstreamReq := openAIImageRequest{
		Model:          strings.TrimSpace(h.cfg.Model),
		Prompt:         prompt,
		Size:           size,
		N:              1,
		Quality:        normalizeImageQuality(h.cfg.Quality),
		OutputFormat:   normalizeOutputFormat(h.cfg.OutputFormat),
		ResponseFormat: "b64_json",
	}

	upstreamURL := imageGenerationsURL(h.cfg.BaseURL)
	body, err := json.Marshal(upstreamReq)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to build image request")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.client.Timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to build upstream request")
		return
	}
	httpReq.Header.Set("Authorization", "Bearer "+imageAPIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	res, err := h.client.Do(httpReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "image gateway request failed",
			"message": err.Error(),
		})
		return
	}
	defer res.Body.Close()

	resBody, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		respondError(c, http.StatusBadGateway, "failed to read image gateway response")
		return
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		c.JSON(http.StatusBadGateway, gin.H{
			"error":           "image gateway returned error",
			"upstream_status": res.StatusCode,
			"message":         safeUpstreamMessage(resBody),
		})
		return
	}

	var upstream openAIImagesResponse
	if err := json.Unmarshal(resBody, &upstream); err != nil {
		respondError(c, http.StatusBadGateway, "invalid image gateway response")
		return
	}
	if len(upstream.Data) == 0 {
		respondError(c, http.StatusBadGateway, "image gateway returned no image")
		return
	}

	item := upstream.Data[0]
	outputFormat := normalizeOutputFormat(firstImageNonEmpty(item.OutputFormat, upstreamReq.OutputFormat, "png"))
	imageURL := strings.TrimSpace(item.URL)
	b64 := strings.TrimSpace(item.B64JSON)
	if imageURL == "" && b64 != "" {
		imageURL = "data:image/" + outputFormat + ";base64," + b64
	}
	if imageURL == "" {
		respondError(c, http.StatusBadGateway, "image gateway returned empty image")
		return
	}

	usage, err := h.repo.RecordAPIKeyUsage(apiKey)
	if err != nil {
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
	c.Header("X-API-Key-Usage-Today", strconv.Itoa(usage))
	if apiKey.DailyLimit > 0 {
		c.Header("X-API-Key-Daily-Limit", strconv.Itoa(apiKey.DailyLimit))
	}
	c.Set("api_key_billable", true)

	respondOK(c, gin.H{
		"image_url":      imageURL,
		"b64_json":       b64,
		"mime_type":      "image/" + outputFormat,
		"model":          upstreamReq.Model,
		"size":           size,
		"quality":        upstreamReq.Quality,
		"output_format":  outputFormat,
		"prompt":         prompt,
		"revised_prompt": item.RevisedPrompt,
	})
}

func imageGenerationsURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(base, "/images/generations") {
		return base
	}
	return base + "/images/generations"
}

func normalizeImageSize(size string) string {
	switch strings.TrimSpace(size) {
	case "1024x1024", "1024x1536", "1536x1024", "2048x1152":
		return strings.TrimSpace(size)
	default:
		return "1024x1024"
	}
}

func normalizeImageQuality(quality string) string {
	switch strings.ToLower(strings.TrimSpace(quality)) {
	case "low", "medium", "high", "auto":
		return strings.ToLower(strings.TrimSpace(quality))
	default:
		return "high"
	}
}

func normalizeOutputFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "jpeg", "jpg":
		return "jpeg"
	case "webp":
		return "webp"
	default:
		return "png"
	}
}

func safeUpstreamMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err == nil {
		if msg := firstNestedString(parsed, "error", "message"); msg != "" {
			return msg
		}
		if msg, ok := parsed["message"].(string); ok {
			return msg
		}
		if errText, ok := parsed["error"].(string); ok {
			return errText
		}
	}
	text := strings.TrimSpace(string(body))
	if len(text) > 500 {
		text = text[:500] + "..."
	}
	return text
}

func firstNestedString(m map[string]any, path ...string) string {
	var cur any = m
	for _, key := range path {
		typed, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = typed[key]
	}
	if s, ok := cur.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func firstImageNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
