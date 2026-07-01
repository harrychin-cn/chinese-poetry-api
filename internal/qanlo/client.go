package qanlo

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/palemoky/chinese-poetry-api/internal/config"
)

const connectPath = "/agent/connect"

// Client builds Qanlo Agent Key connect/recharge URLs without exposing app secrets.
type Client struct {
	cfg config.QanloConfig
}

// ConnectParams contains the per-customer identity used by Qanlo Agent Key.
type ConnectParams struct {
	ExternalUserID   string
	ExternalDeviceID string
	Intent           string
	ReturnURL        string
}

// NewClient creates a Qanlo URL builder.
func NewClient(cfg config.QanloConfig) *Client {
	cfg.AgentBaseURL = strings.TrimRight(strings.TrimSpace(cfg.AgentBaseURL), "/")
	cfg.OpenAIBaseURL = strings.TrimRight(strings.TrimSpace(cfg.OpenAIBaseURL), "/")
	cfg.RechargeURL = strings.TrimSpace(cfg.RechargeURL)
	cfg.AgentAppID = strings.TrimSpace(cfg.AgentAppID)
	cfg.AgentName = strings.TrimSpace(cfg.AgentName)
	cfg.AgentModel = strings.TrimSpace(cfg.AgentModel)
	cfg.ReturnURL = strings.TrimSpace(cfg.ReturnURL)
	return &Client{cfg: cfg}
}

// Configured reports whether the required Qanlo Agent app id is available.
func (c *Client) Configured() bool {
	return c.cfg.AgentAppID != ""
}

// OpenAIBaseURL returns the Qanlo OpenAI-compatible base URL.
func (c *Client) OpenAIBaseURL() string {
	if c.cfg.OpenAIBaseURL != "" {
		return c.cfg.OpenAIBaseURL
	}
	return strings.TrimRight(c.cfg.AgentBaseURL, "/") + "/v1"
}

// CompactRechargeURL returns the configured compact recharge fallback URL.
func (c *Client) CompactRechargeURL() string {
	return c.cfg.RechargeURL
}

// AgentName returns the configured Qanlo agent name.
func (c *Client) AgentName() string {
	return c.cfg.AgentName
}

// AgentModel returns the configured Qanlo model name.
func (c *Client) AgentModel() string {
	return c.cfg.AgentModel
}

// BuildConnectURL builds the Qanlo /agent/connect URL.
func (c *Client) BuildConnectURL(params ConnectParams) (string, error) {
	if c.cfg.AgentBaseURL == "" {
		return "", fmt.Errorf("QANLO_AGENT_BASE_URL is not configured")
	}
	if c.cfg.AgentAppID == "" {
		return "", fmt.Errorf("QANLO_AGENT_APP_ID is not configured")
	}

	intent := strings.TrimSpace(params.Intent)
	if intent == "" {
		intent = "provision"
	}

	values := url.Values{}
	values.Set("app_id", c.cfg.AgentAppID)
	values.Set("agent_name", c.cfg.AgentName)
	values.Set("external_user_id", strings.TrimSpace(params.ExternalUserID))
	values.Set("external_device_id", strings.TrimSpace(params.ExternalDeviceID))
	values.Set("intent", intent)
	values.Set("base_url", c.OpenAIBaseURL())
	if c.cfg.AgentModel != "" {
		values.Set("model", c.cfg.AgentModel)
	}
	if returnURL := strings.TrimSpace(params.ReturnURL); returnURL != "" {
		values.Set("return_url", returnURL)
		values.Set("return_mode", "query")
	}

	return c.cfg.AgentBaseURL + connectPath + "?" + values.Encode(), nil
}

// CallbackReturnURL appends state to the configured callback URL.
func CallbackReturnURL(rawReturnURL, state string) (string, error) {
	rawReturnURL = strings.TrimSpace(rawReturnURL)
	if rawReturnURL == "" {
		return "", fmt.Errorf("QANLO_RETURN_URL is not configured")
	}

	parsed, err := url.Parse(rawReturnURL)
	if err != nil {
		return "", err
	}
	q := parsed.Query()
	q.Set("state", state)
	parsed.RawQuery = q.Encode()
	return parsed.String(), nil
}

// NewState creates an opaque callback state token.
func NewState() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "qst_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

// NewExternalID creates a stable-looking external identity value.
func NewExternalID(prefix string, apiKeyID int64) (string, error) {
	token, err := NewState()
	if err != nil {
		return "", err
	}
	token = strings.TrimPrefix(token, "qst_")
	return fmt.Sprintf("%s-%d-%s", prefix, apiKeyID, token[:16]), nil
}

// StateExpiry returns the default callback state expiration time.
func StateExpiry() time.Time {
	return time.Now().UTC().Add(24 * time.Hour)
}
