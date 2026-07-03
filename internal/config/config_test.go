package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadQanloEnv(t *testing.T) {
	t.Setenv("QANLO_AGENT_BASE_URL", "https://example.qanlo.test")
	t.Setenv("QANLO_OPENAI_BASE_URL", "https://example.qanlo.test/v1")
	t.Setenv("QANLO_RECHARGE_URL", "https://example.qanlo.test/purchase?compact=1")
	t.Setenv("QANLO_AGENT_APP_ID", "agent_poetry")
	t.Setenv("QANLO_AGENT_NAME", "poetry-agent")
	t.Setenv("QANLO_AGENT_MODEL", "gpt-test")
	t.Setenv("QANLO_RETURN_URL", "http://localhost:1279/callback")
	t.Setenv("QANLO_CALLBACK_SECRET", "secret")

	cfg, err := Load("")
	require.NoError(t, err)

	assert.Equal(t, "https://example.qanlo.test", cfg.Qanlo.AgentBaseURL)
	assert.Equal(t, "https://example.qanlo.test/v1", cfg.Qanlo.OpenAIBaseURL)
	assert.Equal(t, "https://example.qanlo.test/purchase?compact=1", cfg.Qanlo.RechargeURL)
	assert.Equal(t, "agent_poetry", cfg.Qanlo.AgentAppID)
	assert.Equal(t, "poetry-agent", cfg.Qanlo.AgentName)
	assert.Equal(t, "gpt-test", cfg.Qanlo.AgentModel)
	assert.Equal(t, "http://localhost:1279/callback", cfg.Qanlo.ReturnURL)
	assert.Equal(t, "secret", cfg.Qanlo.CallbackSecret)
}

func TestLoadAPIKeyRateLimitEnv(t *testing.T) {
	t.Setenv("API_KEY_RATE_LIMIT_RPS", "3.5")
	t.Setenv("API_KEY_RATE_LIMIT_BURST", "9")

	cfg, err := Load("")
	require.NoError(t, err)

	assert.Equal(t, 3.5, cfg.RateLimit.APIKeyRequestsPerSecond)
	assert.Equal(t, 9, cfg.RateLimit.APIKeyBurst)
}

func TestLoadAbuseProtectionEnv(t *testing.T) {
	t.Setenv("ABUSE_PROTECTION_ENABLED", "true")
	t.Setenv("ABUSE_AUTO_BLOCK_ENABLED", "false")
	t.Setenv("ABUSE_FAILURE_THRESHOLD", "7")
	t.Setenv("ABUSE_WINDOW_SECONDS", "30")
	t.Setenv("ABUSE_BLOCK_MINUTES", "15")

	cfg, err := Load("")
	require.NoError(t, err)

	assert.True(t, cfg.Abuse.Enabled)
	assert.False(t, cfg.Abuse.AutoBlockEnabled)
	assert.Equal(t, 7, cfg.Abuse.FailureThreshold)
	assert.Equal(t, 30, cfg.Abuse.WindowSeconds)
	assert.Equal(t, 15, cfg.Abuse.BlockMinutes)
}

func TestLoadImageEnv(t *testing.T) {
	t.Setenv("IMAGE_GENERATION_ENABLED", "true")
	t.Setenv("IMAGE_API_KEY", "env-image-key")
	t.Setenv("IMAGE_BASE_URL", "https://image.example.test/openai/v1")
	t.Setenv("IMAGE_MODEL", "gpt-image-2")
	t.Setenv("IMAGE_QUALITY", "medium")
	t.Setenv("IMAGE_OUTPUT_FORMAT", "webp")
	t.Setenv("IMAGE_TIMEOUT_SECONDS", "45")
	t.Setenv("IMAGE_COST_UNITS", "3")

	cfg, err := Load("")
	require.NoError(t, err)

	assert.True(t, cfg.Image.Enabled)
	assert.Equal(t, "env-image-key", cfg.Image.APIKey)
	assert.Equal(t, "https://image.example.test/openai/v1", cfg.Image.BaseURL)
	assert.Equal(t, "gpt-image-2", cfg.Image.Model)
	assert.Equal(t, "medium", cfg.Image.Quality)
	assert.Equal(t, "webp", cfg.Image.OutputFormat)
	assert.Equal(t, 45, cfg.Image.TimeoutSeconds)
	assert.Equal(t, 3, cfg.Image.CostUnits)
}
