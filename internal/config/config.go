package config

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

// Config holds all configuration for the application
type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Database  DatabaseConfig  `mapstructure:"database"`
	RateLimit RateLimitConfig `mapstructure:"rate_limit"`
	Abuse     AbuseConfig     `mapstructure:"abuse_protection"`
	APIAuth   APIAuthConfig   `mapstructure:"api_auth"`
	Qanlo     QanloConfig     `mapstructure:"qanlo"`
	Image     ImageConfig     `mapstructure:"image"`
	GraphQL   GraphQLConfig   `mapstructure:"graphql"`
	Search    SearchConfig    `mapstructure:"search"`
}

// ServerConfig holds server configuration
type ServerConfig struct {
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"`
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Path         string `mapstructure:"path"`
	MaxOpenConns int    `mapstructure:"max_open_conns"` // Maximum number of open connections
	MaxIdleConns int    `mapstructure:"max_idle_conns"` // Maximum number of idle connections
}

type DownloadConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	GithubRepo     string `mapstructure:"github_repo"`
	ReleaseVersion string `mapstructure:"release_version"`
}

// RateLimitConfig holds rate limiting configuration
type RateLimitConfig struct {
	Enabled                 bool    `mapstructure:"enabled"`
	RequestsPerSecond       float64 `mapstructure:"requests_per_second"`
	Burst                   int     `mapstructure:"burst"`
	APIKeyRequestsPerSecond float64 `mapstructure:"api_key_requests_per_second"`
	APIKeyBurst             int     `mapstructure:"api_key_burst"`
}

// AbuseConfig holds blocklist and auto-block settings.
type AbuseConfig struct {
	Enabled          bool `mapstructure:"enabled"`
	AutoBlockEnabled bool `mapstructure:"auto_block_enabled"`
	FailureThreshold int  `mapstructure:"failure_threshold"`
	WindowSeconds    int  `mapstructure:"window_seconds"`
	BlockMinutes     int  `mapstructure:"block_minutes"`
}

// APIAuthConfig holds API key authentication settings for commercial endpoints.
type APIAuthConfig struct {
	Enabled           bool   `mapstructure:"enabled"`
	AdminToken        string `mapstructure:"admin_token"`
	DefaultDailyLimit int    `mapstructure:"default_daily_limit"`
}

// QanloConfig holds Qanlo Agent Key and compact recharge settings.
type QanloConfig struct {
	AgentBaseURL   string `mapstructure:"agent_base_url"`
	OpenAIBaseURL  string `mapstructure:"openai_base_url"`
	RechargeURL    string `mapstructure:"recharge_url"`
	AgentAppID     string `mapstructure:"agent_app_id"`
	AgentName      string `mapstructure:"agent_name"`
	AgentModel     string `mapstructure:"agent_model"`
	ReturnURL      string `mapstructure:"return_url"`
	CallbackSecret string `mapstructure:"callback_secret"`
}

// ImageConfig holds image generation gateway defaults. Users provide the image
// API key per request from the console page; it is not a site-wide server key.
type ImageConfig struct {
	APIKey         string `mapstructure:"api_key"`
	BaseURL        string `mapstructure:"base_url"`
	Model          string `mapstructure:"model"`
	Quality        string `mapstructure:"quality"`
	OutputFormat   string `mapstructure:"output_format"`
	TimeoutSeconds int    `mapstructure:"timeout_seconds"`
}

// GraphQLConfig holds GraphQL configuration
type GraphQLConfig struct {
	Playground bool `mapstructure:"playground"`
}

// SearchConfig holds search configuration
type SearchConfig struct {
	MaxResults      int `mapstructure:"max_results"`
	DefaultPageSize int `mapstructure:"default_page_size"`
}

// Load loads configuration from file and environment variables
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Read config file if provided
	if configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	// Override with environment variables
	bindEnvVars(v)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Auto-detect connection pool size based on CPU cores if not configured
	cfg.applyConnectionPoolDefaults()

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.mode", "release")
	v.SetDefault("download.enabled", true)
	v.SetDefault("download.release_version", "latest")
	v.SetDefault("rate_limit.enabled", true)
	v.SetDefault("rate_limit.requests_per_second", 10.0)
	v.SetDefault("rate_limit.burst", 20)
	v.SetDefault("rate_limit.api_key_requests_per_second", 2.0)
	v.SetDefault("rate_limit.api_key_burst", 5)
	v.SetDefault("rate_limit.by_ip", true)
	v.SetDefault("abuse_protection.enabled", true)
	v.SetDefault("abuse_protection.auto_block_enabled", true)
	v.SetDefault("abuse_protection.failure_threshold", 20)
	v.SetDefault("abuse_protection.window_seconds", 60)
	v.SetDefault("abuse_protection.block_minutes", 60)
	v.SetDefault("api_auth.enabled", false)
	v.SetDefault("api_auth.admin_token", "")
	v.SetDefault("api_auth.default_daily_limit", 1000)
	v.SetDefault("qanlo.agent_base_url", "https://qanlo.com")
	v.SetDefault("qanlo.openai_base_url", "https://qanlo.com/v1")
	v.SetDefault("qanlo.recharge_url", "https://qanlo.com/purchase?compact=1&from=agent_key&tab=recharge&ui_mode=embedded")
	v.SetDefault("qanlo.agent_app_id", "")
	v.SetDefault("qanlo.agent_name", "chinese-poetry-api")
	v.SetDefault("qanlo.agent_model", "deepseek-v4-flash")
	v.SetDefault("qanlo.return_url", "http://localhost:1279/api/v1/billing/qanlo/callback")
	v.SetDefault("qanlo.callback_secret", "")
	v.SetDefault("image.api_key", "")
	v.SetDefault("image.base_url", "https://qanlo.com/openai/v1")
	v.SetDefault("image.model", "gpt-image-2")
	v.SetDefault("image.quality", "high")
	v.SetDefault("image.output_format", "png")
	v.SetDefault("image.timeout_seconds", 180)
	v.SetDefault("graphql.playground", false)
	v.SetDefault("graphql.introspection", true)
	v.SetDefault("graphql.complexity_limit", 1000)
	v.SetDefault("search.max_results", 1000)
	v.SetDefault("search.default_page_size", 20)
	// Database connection pool - auto-detect based on CPU cores
	// 0 means auto-detect (will be set to runtime.NumCPU() in Load())
	v.SetDefault("database.max_open_conns", 0)
	v.SetDefault("database.max_idle_conns", 0)
}

func bindEnvVars(v *viper.Viper) {
	// Server
	if port := os.Getenv("PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			v.Set("server.port", p)
		}
	}
	if mode := os.Getenv("GIN_MODE"); mode != "" {
		v.Set("server.mode", mode)
	}

	// Hardcoded data directory (matches docker-compose volume mount)
	dataDir := "data"

	// Database - use unified poetry.db (contains both simplified and traditional tables)
	// The lang parameter in API requests determines which tables to query
	v.Set("database.path", fmt.Sprintf("%s/poetry.db", dataDir))

	// Rate Limit
	if enabled := os.Getenv("RATE_LIMIT_ENABLED"); enabled != "" {
		v.Set("rate_limit.enabled", enabled == "true")
	}
	if rps := os.Getenv("RATE_LIMIT_RPS"); rps != "" {
		if r, err := strconv.ParseFloat(rps, 64); err == nil {
			v.Set("rate_limit.requests_per_second", r)
		}
	}
	if burst := os.Getenv("RATE_LIMIT_BURST"); burst != "" {
		if b, err := strconv.Atoi(burst); err == nil {
			v.Set("rate_limit.burst", b)
		}
	}
	if rps := os.Getenv("API_KEY_RATE_LIMIT_RPS"); rps != "" {
		if r, err := strconv.ParseFloat(rps, 64); err == nil {
			v.Set("rate_limit.api_key_requests_per_second", r)
		}
	}
	if burst := os.Getenv("API_KEY_RATE_LIMIT_BURST"); burst != "" {
		if b, err := strconv.Atoi(burst); err == nil {
			v.Set("rate_limit.api_key_burst", b)
		}
	}

	// Abuse protection
	if enabled := os.Getenv("ABUSE_PROTECTION_ENABLED"); enabled != "" {
		v.Set("abuse_protection.enabled", enabled == "true")
	}
	if enabled := os.Getenv("ABUSE_AUTO_BLOCK_ENABLED"); enabled != "" {
		v.Set("abuse_protection.auto_block_enabled", enabled == "true")
	}
	if threshold := os.Getenv("ABUSE_FAILURE_THRESHOLD"); threshold != "" {
		if value, err := strconv.Atoi(threshold); err == nil {
			v.Set("abuse_protection.failure_threshold", value)
		}
	}
	if seconds := os.Getenv("ABUSE_WINDOW_SECONDS"); seconds != "" {
		if value, err := strconv.Atoi(seconds); err == nil {
			v.Set("abuse_protection.window_seconds", value)
		}
	}
	if minutes := os.Getenv("ABUSE_BLOCK_MINUTES"); minutes != "" {
		if value, err := strconv.Atoi(minutes); err == nil {
			v.Set("abuse_protection.block_minutes", value)
		}
	}

	// API key auth for commercial endpoints
	if enabled := os.Getenv("API_AUTH_ENABLED"); enabled != "" {
		v.Set("api_auth.enabled", enabled == "true")
	}
	if token := os.Getenv("API_ADMIN_TOKEN"); token != "" {
		v.Set("api_auth.admin_token", token)
	}
	if dailyLimit := os.Getenv("API_DEFAULT_DAILY_LIMIT"); dailyLimit != "" {
		if limit, err := strconv.Atoi(dailyLimit); err == nil {
			v.Set("api_auth.default_daily_limit", limit)
		}
	}

	// Qanlo Agent Key / compact recharge settings
	if baseURL := os.Getenv("QANLO_AGENT_BASE_URL"); baseURL != "" {
		v.Set("qanlo.agent_base_url", baseURL)
	}
	if baseURL := os.Getenv("QANLO_OPENAI_BASE_URL"); baseURL != "" {
		v.Set("qanlo.openai_base_url", baseURL)
	}
	if rechargeURL := os.Getenv("QANLO_RECHARGE_URL"); rechargeURL != "" {
		v.Set("qanlo.recharge_url", rechargeURL)
	}
	if appID := os.Getenv("QANLO_AGENT_APP_ID"); appID != "" {
		v.Set("qanlo.agent_app_id", appID)
	}
	if name := os.Getenv("QANLO_AGENT_NAME"); name != "" {
		v.Set("qanlo.agent_name", name)
	}
	if model := os.Getenv("QANLO_AGENT_MODEL"); model != "" {
		v.Set("qanlo.agent_model", model)
	}
	if returnURL := os.Getenv("QANLO_RETURN_URL"); returnURL != "" {
		v.Set("qanlo.return_url", returnURL)
	} else if returnURL := os.Getenv("QANLO_AGENT_RETURN_URL"); returnURL != "" {
		v.Set("qanlo.return_url", returnURL)
	}
	if secret := os.Getenv("QANLO_CALLBACK_SECRET"); secret != "" {
		v.Set("qanlo.callback_secret", secret)
	}

	// Optional image generation gateway defaults. Users paste their own Qanlo
	// image key in the console page; no site-wide image key is read from env.
	if baseURL := os.Getenv("IMAGE_BASE_URL"); baseURL != "" {
		v.Set("image.base_url", baseURL)
	}
	if model := os.Getenv("IMAGE_MODEL"); model != "" {
		v.Set("image.model", model)
	}
	if quality := os.Getenv("IMAGE_QUALITY"); quality != "" {
		v.Set("image.quality", quality)
	}
	if outputFormat := os.Getenv("IMAGE_OUTPUT_FORMAT"); outputFormat != "" {
		v.Set("image.output_format", outputFormat)
	}
	if timeoutSeconds := os.Getenv("IMAGE_TIMEOUT_SECONDS"); timeoutSeconds != "" {
		if value, err := strconv.Atoi(timeoutSeconds); err == nil {
			v.Set("image.timeout_seconds", value)
		}
	}

	// Database connection pool
	if maxOpen := os.Getenv("DB_MAX_OPEN_CONNS"); maxOpen != "" {
		if m, err := strconv.Atoi(maxOpen); err == nil {
			v.Set("database.max_open_conns", m)
		}
	}
	if maxIdle := os.Getenv("DB_MAX_IDLE_CONNS"); maxIdle != "" {
		if m, err := strconv.Atoi(maxIdle); err == nil {
			v.Set("database.max_idle_conns", m)
		}
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid port: %d", c.Server.Port)
	}

	if c.Server.Mode != "debug" && c.Server.Mode != "release" && c.Server.Mode != "test" {
		return fmt.Errorf("invalid server mode: %s (must be 'debug', 'release', or 'test')", c.Server.Mode)
	}

	if c.Database.Path == "" {
		return fmt.Errorf("database path cannot be empty")
	}

	if c.RateLimit.RequestsPerSecond <= 0 {
		return fmt.Errorf("rate limit requests_per_second must be positive")
	}

	if c.RateLimit.Burst <= 0 {
		return fmt.Errorf("rate limit burst must be positive")
	}

	if c.RateLimit.APIKeyRequestsPerSecond <= 0 {
		return fmt.Errorf("api key rate limit requests_per_second must be positive")
	}

	if c.RateLimit.APIKeyBurst <= 0 {
		return fmt.Errorf("api key rate limit burst must be positive")
	}

	if c.Abuse.Enabled {
		if c.Abuse.FailureThreshold <= 0 {
			return fmt.Errorf("abuse_protection failure_threshold must be positive")
		}
		if c.Abuse.WindowSeconds <= 0 {
			return fmt.Errorf("abuse_protection window_seconds must be positive")
		}
		if c.Abuse.BlockMinutes <= 0 {
			return fmt.Errorf("abuse_protection block_minutes must be positive")
		}
	}

	if c.APIAuth.Enabled && c.APIAuth.DefaultDailyLimit < 0 {
		return fmt.Errorf("api_auth default_daily_limit cannot be negative")
	}

	if strings.TrimSpace(c.Qanlo.AgentName) == "" {
		return fmt.Errorf("qanlo agent_name cannot be empty")
	}

	if strings.TrimSpace(c.Qanlo.AgentBaseURL) == "" {
		return fmt.Errorf("qanlo agent_base_url cannot be empty")
	}

	if strings.TrimSpace(c.Qanlo.RechargeURL) == "" {
		return fmt.Errorf("qanlo recharge_url cannot be empty")
	}

	if strings.TrimSpace(c.Qanlo.ReturnURL) == "" {
		return fmt.Errorf("qanlo return_url cannot be empty")
	}

	if strings.TrimSpace(c.Image.BaseURL) == "" {
		return fmt.Errorf("image base_url cannot be empty")
	}
	if strings.TrimSpace(c.Image.Model) == "" {
		return fmt.Errorf("image model cannot be empty")
	}
	if c.Image.TimeoutSeconds <= 0 {
		return fmt.Errorf("image timeout_seconds must be positive")
	}

	return nil
}

// applyConnectionPoolDefaults sets intelligent defaults for connection pool based on CPU cores
func (c *Config) applyConnectionPoolDefaults() {
	numCPU := runtime.NumCPU()

	// Auto-detect max_open_conns if not configured (0 or negative)
	if c.Database.MaxOpenConns <= 0 {
		// Adaptive strategy based on CPU count:
		// - Multi-core (>4): Use NumCPU directly (sufficient parallelism)
		// - Few cores (≤4): Use NumCPU*2 to better utilize I/O wait time
		// - Cap at 50 to prevent excessive connections
		if numCPU > 4 {
			c.Database.MaxOpenConns = min(numCPU, 50)
		} else {
			c.Database.MaxOpenConns = min(numCPU*2, 50)
		}
	}

	// Auto-detect max_idle_conns if not configured
	if c.Database.MaxIdleConns <= 0 {
		// Idle connections should be about half of max open connections
		c.Database.MaxIdleConns = max(c.Database.MaxOpenConns/2, 1)
	}
}
