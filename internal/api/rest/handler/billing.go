package handler

import (
	"errors"
	"html"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/palemoky/chinese-poetry-api/internal/config"
	"github.com/palemoky/chinese-poetry-api/internal/database"
	"github.com/palemoky/chinese-poetry-api/internal/qanlo"
)

// BillingHandler manages Qanlo Agent Key provision/recharge/status endpoints.
type BillingHandler struct {
	repo        *database.Repository
	qanloCfg    config.QanloConfig
	qanloClient *qanlo.Client
}

// NewBillingHandler creates a billing handler.
func NewBillingHandler(repo *database.Repository, qanloCfg config.QanloConfig) *BillingHandler {
	return &BillingHandler{
		repo:        repo,
		qanloCfg:    qanloCfg,
		qanloClient: qanlo.NewClient(qanloCfg),
	}
}

// ProvisionQanlo returns a Qanlo /agent/connect URL for creating or binding an Agent Key.
func (h *BillingHandler) ProvisionQanlo(c *gin.Context) {
	apiKey, ok := apiKeyFromContext(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}

	if !h.qanloClient.Configured() && h.qanloClient.CompactRechargeURL() != "" {
		respondOK(c, gin.H{
			"configured":           false,
			"provider_name":        "qanlo",
			"intent":               "provision",
			"url":                  "",
			"connect_url":          "",
			"recharge_url":         h.qanloClient.CompactRechargeURL(),
			"compact_recharge_url": h.qanloClient.CompactRechargeURL(),
			"base_url":             h.qanloClient.OpenAIBaseURL(),
			"model":                h.qanloClient.AgentModel(),
			"agent_name":           h.qanloClient.AgentName(),
			"message":              "QANLO_AGENT_APP_ID 未配置，当前不能创建/绑定 Qanlo Agent Key；请先配置 QANLO_AGENT_APP_ID 后重启服务，或点击旁边的“打开 Qanlo 精简充值页”。",
			"qanlo": gin.H{
				"configured":           h.qanloClient.Configured(),
				"status":               "unbound",
				"has_qanlo_key":        false,
				"base_url":             h.qanloClient.OpenAIBaseURL(),
				"model":                h.qanloClient.AgentModel(),
				"agent_name":           h.qanloClient.AgentName(),
				"external_user_id":     "poetry-user-" + formatID(apiKey.ID),
				"external_device_id":   "poetry-device-" + formatID(apiKey.ID),
				"compact_recharge_url": h.qanloClient.CompactRechargeURL(),
			},
		})
		return
	}

	response, err := h.startQanloSession(apiKey, "provision")
	if err != nil {
		respondError(c, http.StatusServiceUnavailable, err.Error())
		return
	}

	respondOK(c, response)
}

// CreateQanloRechargeSession returns a Qanlo recharge URL for the current API key.
func (h *BillingHandler) CreateQanloRechargeSession(c *gin.Context) {
	apiKey, ok := apiKeyFromContext(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}

	response, err := h.startQanloSession(apiKey, "recharge")
	if err != nil {
		if h.qanloClient.CompactRechargeURL() == "" {
			respondError(c, http.StatusServiceUnavailable, err.Error())
			return
		}
		respondOK(c, gin.H{
			"configured":           false,
			"provider_name":        "qanlo",
			"url":                  h.qanloClient.CompactRechargeURL(),
			"recharge_url":         h.qanloClient.CompactRechargeURL(),
			"compact_recharge_url": h.qanloClient.CompactRechargeURL(),
			"message":              "QANLO_AGENT_APP_ID 未配置，已返回 Qanlo 精简充值页兜底 URL。",
		})
		return
	}

	respondOK(c, response)
}

// QanloCallback receives the browser callback from Qanlo.
func (h *BillingHandler) QanloCallback(c *gin.Context) {
	if h.qanloCfg.CallbackSecret != "" && c.Query("secret") != h.qanloCfg.CallbackSecret {
		c.Data(http.StatusUnauthorized, "text/html; charset=utf-8", []byte(callbackHTML("Qanlo 绑定失败", "回调校验失败，请返回控制台重试。")))
		return
	}

	state := c.Query("state")
	resolvedKey := firstQueryText(c.Query("qanlo_key"), c.Query("api_key"), c.Query("key"))
	if state == "" || resolvedKey == "" {
		if state != "" && h.markRechargeOnly(c, state) {
			return
		}
		c.Data(http.StatusBadRequest, "text/html; charset=utf-8", []byte(callbackHTML("Qanlo 绑定失败", "未收到 Qanlo Key 或 state，请返回控制台重试。")))
		return
	}

	baseURL := firstQueryText(c.Query("qanlo_base_url"), c.Query("base_url"), h.qanloClient.OpenAIBaseURL())
	binding, err := h.repo.SaveQanloCallback(database.QanloCallbackParams{
		CallbackState: state,
		RawQanloKey:   resolvedKey,
		QanloBaseURL:  baseURL,
		RawQuery:      c.Request.URL.RawQuery,
		EventType:     firstQueryText(c.Query("intent"), "callback"),
	})
	if err != nil {
		c.Data(http.StatusBadRequest, "text/html; charset=utf-8", []byte(callbackHTML("Qanlo 绑定失败", "回调已过期或无法匹配本地 API Key，请返回控制台重新发起绑定。")))
		return
	}

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(callbackHTML("Qanlo 绑定成功", "Qanlo Agent Key 已绑定到当前诗词 API Key，可以关闭本页面并回到控制台继续使用。绑定 ID："+html.EscapeString(binding.QanloKeyPrefix))))
}

func (h *BillingHandler) markRechargeOnly(c *gin.Context, state string) bool {
	binding, err := h.repo.RecordQanloReturn(state, firstQueryText(c.Query("intent"), "recharge_return"), c.Request.URL.RawQuery)
	if err != nil {
		return false
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(callbackHTML("Qanlo 充值完成", "已收到 Qanlo 回跳，请回到控制台刷新状态后继续使用。API Key ID："+html.EscapeString(formatID(binding.APIKeyID)))))
	return true
}

// BillingStatus returns local API key usage plus Qanlo binding status.
func (h *BillingHandler) BillingStatus(c *gin.Context) {
	apiKey, ok := apiKeyFromContext(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}

	today := time.Now().UTC().Format("2006-01-02")
	usage, err := h.repo.GetAPIKeyUsageCount(apiKey.ID, today)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to read usage")
		return
	}

	binding, err := h.repo.GetQanloBindingByAPIKeyID(apiKey.ID)
	if err != nil && !errors.Is(err, database.ErrQanloBindingNotFound) {
		respondError(c, http.StatusInternalServerError, "failed to read qanlo binding")
		return
	}

	respondOK(c, gin.H{
		"api_key": gin.H{
			"id":           apiKey.ID,
			"name":         apiKey.Name,
			"tier":         apiKey.Tier,
			"daily_limit":  apiKey.DailyLimit,
			"today_usage":  usage,
			"enabled":      apiKey.Enabled,
			"key_prefix":   apiKey.KeyPrefix,
			"quota_policy": "daily_limit=0 means unlimited",
		},
		"qanlo": h.formatQanloBinding(binding),
	})
}

func (h *BillingHandler) startQanloSession(apiKey *database.APIKey, intent string) (gin.H, error) {
	state, err := qanlo.NewState()
	if err != nil {
		return nil, err
	}

	externalUserID, externalDeviceID, err := h.externalIdentity(apiKey.ID)
	if err != nil {
		return nil, err
	}

	binding, err := h.repo.UpsertQanloBindingSession(database.QanloBindingSessionParams{
		APIKeyID:          apiKey.ID,
		ExternalUserID:    externalUserID,
		ExternalDeviceID:  externalDeviceID,
		CallbackState:     state,
		CallbackExpiresAt: qanlo.StateExpiry(),
	})
	if err != nil {
		return nil, err
	}

	returnURL, err := qanlo.CallbackReturnURL(h.callbackReturnURL(), state)
	if err != nil {
		return nil, err
	}

	connectURL, err := h.qanloClient.BuildConnectURL(qanlo.ConnectParams{
		ExternalUserID:   binding.ExternalUserID,
		ExternalDeviceID: binding.ExternalDeviceID,
		Intent:           intent,
		ReturnURL:        returnURL,
	})
	if err != nil {
		return nil, err
	}

	return gin.H{
		"configured":           h.qanloClient.Configured(),
		"provider_name":        "qanlo",
		"intent":               intent,
		"url":                  connectURL,
		"connect_url":          connectURL,
		"recharge_url":         connectURL,
		"compact_recharge_url": h.qanloClient.CompactRechargeURL(),
		"base_url":             h.qanloClient.OpenAIBaseURL(),
		"model":                h.qanloClient.AgentModel(),
		"agent_name":           h.qanloClient.AgentName(),
		"state_expires_at":     binding.CallbackExpiresAt,
		"qanlo":                h.formatQanloBinding(binding),
		"message":              "已生成 Qanlo Agent Key " + intent + " 跳转地址。",
	}, nil
}

func (h *BillingHandler) externalIdentity(apiKeyID int64) (string, string, error) {
	binding, err := h.repo.GetQanloBindingByAPIKeyID(apiKeyID)
	if err == nil && binding.ExternalUserID != "" && binding.ExternalDeviceID != "" {
		return binding.ExternalUserID, binding.ExternalDeviceID, nil
	}
	if err != nil && !errors.Is(err, database.ErrQanloBindingNotFound) {
		return "", "", err
	}

	userID, err := qanlo.NewExternalID("poetry-user", apiKeyID)
	if err != nil {
		return "", "", err
	}
	deviceID, err := qanlo.NewExternalID("poetry-device", apiKeyID)
	if err != nil {
		return "", "", err
	}
	return userID, deviceID, nil
}

func (h *BillingHandler) callbackReturnURL() string {
	rawReturnURL := h.qanloCfg.ReturnURL
	if h.qanloCfg.CallbackSecret == "" {
		return rawReturnURL
	}
	parsed, err := url.Parse(rawReturnURL)
	if err != nil {
		return rawReturnURL
	}
	q := parsed.Query()
	q.Set("secret", h.qanloCfg.CallbackSecret)
	parsed.RawQuery = q.Encode()
	return parsed.String()
}

func (h *BillingHandler) formatQanloBinding(binding *database.QanloBinding) gin.H {
	if binding == nil {
		return gin.H{
			"configured":           h.qanloClient.Configured(),
			"status":               "unbound",
			"has_qanlo_key":        false,
			"base_url":             h.qanloClient.OpenAIBaseURL(),
			"model":                h.qanloClient.AgentModel(),
			"agent_name":           h.qanloClient.AgentName(),
			"compact_recharge_url": h.qanloClient.CompactRechargeURL(),
		}
	}

	return gin.H{
		"configured":           h.qanloClient.Configured(),
		"status":               binding.Status,
		"has_qanlo_key":        binding.QanloKeyHash != "",
		"qanlo_key_prefix":     binding.QanloKeyPrefix,
		"base_url":             firstQueryText(binding.QanloBaseURL, h.qanloClient.OpenAIBaseURL()),
		"model":                h.qanloClient.AgentModel(),
		"agent_name":           h.qanloClient.AgentName(),
		"external_user_id":     binding.ExternalUserID,
		"external_device_id":   binding.ExternalDeviceID,
		"callback_expires_at":  binding.CallbackExpiresAt,
		"last_synced_at":       binding.LastSyncedAt,
		"compact_recharge_url": h.qanloClient.CompactRechargeURL(),
		"updated_at":           binding.UpdatedAt,
	}
}

func apiKeyFromContext(c *gin.Context) (*database.APIKey, bool) {
	value, ok := c.Get("api_key")
	if !ok {
		return nil, false
	}
	apiKey, ok := value.(*database.APIKey)
	return apiKey, ok && apiKey != nil
}

func firstQueryText(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func callbackHTML(title, message string) string {
	return "<!doctype html><html><head><meta charset=\"utf-8\"><title>" +
		html.EscapeString(title) +
		"</title></head><body><h1>" +
		html.EscapeString(title) +
		"</h1><p>" +
		html.EscapeString(message) +
		"</p></body></html>"
}

func formatID(value int64) string {
	if value == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[i:])
}
