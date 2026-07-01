package qanlo

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/palemoky/chinese-poetry-api/internal/config"
)

func TestBuildConnectURL(t *testing.T) {
	client := NewClient(config.QanloConfig{
		AgentBaseURL:  "https://qanlo.com/",
		OpenAIBaseURL: "https://qanlo.com/v1/",
		AgentAppID:    "agent_test",
		AgentName:     "chinese-poetry-api",
		AgentModel:    "deepseek-v4-flash",
	})

	connectURL, err := client.BuildConnectURL(ConnectParams{
		ExternalUserID:   "poetry-user-1",
		ExternalDeviceID: "poetry-device-1",
		Intent:           "recharge",
		ReturnURL:        "http://localhost:1279/api/v1/billing/qanlo/callback?state=abc",
	})
	require.NoError(t, err)

	parsed, err := url.Parse(connectURL)
	require.NoError(t, err)
	assert.Equal(t, "https", parsed.Scheme)
	assert.Equal(t, "qanlo.com", parsed.Host)
	assert.Equal(t, "/agent/connect", parsed.Path)

	query := parsed.Query()
	assert.Equal(t, "agent_test", query.Get("app_id"))
	assert.Equal(t, "chinese-poetry-api", query.Get("agent_name"))
	assert.Equal(t, "poetry-user-1", query.Get("external_user_id"))
	assert.Equal(t, "poetry-device-1", query.Get("external_device_id"))
	assert.Equal(t, "recharge", query.Get("intent"))
	assert.Equal(t, "query", query.Get("return_mode"))
	assert.Equal(t, "https://qanlo.com/v1", query.Get("base_url"))
	assert.Equal(t, "deepseek-v4-flash", query.Get("model"))
	assert.Contains(t, query.Get("return_url"), "state=abc")
}

func TestBuildConnectURLRequiresAppID(t *testing.T) {
	client := NewClient(config.QanloConfig{
		AgentBaseURL: "https://qanlo.com",
		AgentName:    "chinese-poetry-api",
	})

	_, err := client.BuildConnectURL(ConnectParams{
		ExternalUserID:   "u",
		ExternalDeviceID: "d",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "QANLO_AGENT_APP_ID")
}

func TestCallbackReturnURL(t *testing.T) {
	callbackURL, err := CallbackReturnURL("http://localhost:1279/api/v1/billing/qanlo/callback?secret=s1", "state-1")
	require.NoError(t, err)

	parsed, err := url.Parse(callbackURL)
	require.NoError(t, err)
	assert.Equal(t, "s1", parsed.Query().Get("secret"))
	assert.Equal(t, "state-1", parsed.Query().Get("state"))
}

func TestNewState(t *testing.T) {
	state, err := NewState()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(state, "qst_"))
	assert.Greater(t, len(state), len("qst_"))
}
