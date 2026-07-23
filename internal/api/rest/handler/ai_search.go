package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/palemoky/chinese-poetry-api/internal/config"
	"github.com/palemoky/chinese-poetry-api/internal/database"
	"github.com/palemoky/chinese-poetry-api/internal/qanlo"
)

// AISearchHandler invokes the customer's bound Qanlo Agent Key for poetry intent parsing.
type AISearchHandler struct {
	repo      *database.Repository
	cfg       config.QanloConfig
	box       *qanlo.SecretBox
	client    *http.Client
	knowledge *KnowledgeHandler
}

func NewAISearchHandler(repo *database.Repository, cfg config.QanloConfig) *AISearchHandler {
	box, _ := qanlo.NewSecretBox(cfg.KeyEncryptionKey)
	return &AISearchHandler{repo: repo, cfg: cfg, box: box, client: &http.Client{Timeout: 45 * time.Second}, knowledge: NewKnowledgeHandler(repo)}
}

type aiChatRequest struct {
	Model          string            `json:"model"`
	Messages       []aiChatMessage   `json:"messages"`
	Temperature    float64           `json:"temperature"`
	ResponseFormat map[string]string `json:"response_format,omitempty"`
}
type aiChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type aiChatResponse struct {
	Choices []struct {
		Message aiChatMessage `json:"message"`
	} `json:"choices"`
}
type aiSearchModelOutput struct {
	Query string `json:"query"`
}

func (h *AISearchHandler) Search(c *gin.Context) {
	apiKey, ok := apiKeyFromContext(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	request := strings.TrimSpace(c.Query("q"))
	if request == "" {
		respondError(c, http.StatusBadRequest, "query parameter 'q' is required")
		return
	}
	if len([]rune(request)) > 500 {
		respondError(c, http.StatusBadRequest, "query is too long")
		return
	}
	if h.box == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "model search unavailable", "code": "qanlo_key_encryption_not_configured"})
		return
	}
	binding, err := h.repo.GetQanloBindingByAPIKeyID(apiKey.ID)
	if err != nil || binding.QanloKeyCiphertext == "" {
		c.JSON(http.StatusConflict, gin.H{"error": "qanlo agent key needs binding", "code": "qanlo_key_rebind_required"})
		return
	}
	key, err := h.box.Open(binding.QanloKeyCiphertext)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to open bound Qanlo Agent Key")
		return
	}
	modelQuery, err := h.expand(c.Request.Context(), key, firstNonEmpty(binding.QanloBaseURL, h.cfg.OpenAIBaseURL), request)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "qanlo model search failed", "code": "qanlo_model_request_failed", "message": err.Error()})
		return
	}
	pagination := ParsePagination(c)
	repo := h.repo.WithLang(parseLang(c))
	filter, ok := buildKnowledgeQueryFilter(c, repo, pagination)
	if !ok {
		return
	}
	data, total, metadata, err := h.knowledge.runKnowledgeRecall(repo, pagination, knowledgeRecallOptions{Intent: modelQuery, BaseFilter: filter})
	if err != nil {
		respondError(c, http.StatusInternalServerError, "model recall failed")
		return
	}
	out := NewPaginationResponse(data, pagination, total)
	out["ai"] = gin.H{"provider": "qanlo", "model": firstNonEmpty(h.cfg.AgentModel, "default"), "normalized_query": modelQuery}
	out["knowledge"] = metadata
	c.JSON(http.StatusOK, out)
}
func (h *AISearchHandler) expand(ctx context.Context, key, baseURL, request string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://qanlo.com/v1"
	}
	body, _ := json.Marshal(aiChatRequest{Model: firstNonEmpty(h.cfg.AgentModel, "deepseek-v4-flash"), Temperature: 0, ResponseFormat: map[string]string{"type": "json_object"}, Messages: []aiChatMessage{{Role: "system", Content: "Convert the user's Chinese poetry request into one concise Chinese search term. Return JSON only: {\\\"query\\\":\\\"...\\\"}. Do not invent quotations."}, {Role: "user", Content: request}}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	res, err := h.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", errors.New(safeUpstreamMessage(raw))
	}
	var parsed aiChatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", io.ErrUnexpectedEOF
	}
	var output aiSearchModelOutput
	if err := json.Unmarshal([]byte(parsed.Choices[0].Message.Content), &output); err != nil {
		return "", err
	}
	output.Query = strings.TrimSpace(output.Query)
	if output.Query == "" || len([]rune(output.Query)) > 120 {
		return "", io.ErrUnexpectedEOF
	}
	return output.Query, nil
}
