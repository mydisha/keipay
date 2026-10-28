package config

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all runtime configuration loaded from environment.
type Config struct {
	AppEnv      string `mapstructure:"APP_ENV"`
	HTTPPort    string `mapstructure:"HTTP_PORT"`
	HTTPBaseURL string `mapstructure:"HTTP_BASE_URL"`

	// LogLevel is the minimum log level: debug, info, warn, error.
	LogLevel string `mapstructure:"LOG_LEVEL"`
	// LogFormat is the log encoding: json or console. Empty defaults by env.
	LogFormat string `mapstructure:"LOG_FORMAT"`

	PostgresDSN string `mapstructure:"POSTGRES_DSN"`

	RedisAddr     string `mapstructure:"REDIS_ADDR"`
	RedisPassword string `mapstructure:"REDIS_PASSWORD"`
	RedisDB       int    `mapstructure:"REDIS_DB"`

	// XenditSecretKey and XenditCallbackToken are deprecated: when both are
	// set, the server imports them once into a connection, then ignores them.
	XenditSecretKey     string `mapstructure:"XENDIT_SECRET_KEY"`
	XenditCallbackToken string `mapstructure:"XENDIT_CALLBACK_TOKEN"`

	// JWTSecretSet reports that the retired JWT_SECRET is still configured, so
	// the server can warn about it. Its value is never kept.
	JWTSecretSet bool `mapstructure:"-"`

	// CookieSecureMode is auto, true or false; see CookieSecure.
	CookieSecureMode       string        `mapstructure:"COOKIE_SECURE"`
	SessionIdleTimeout     time.Duration `mapstructure:"SESSION_IDLE_TIMEOUT"`
	SessionAbsoluteTimeout time.Duration `mapstructure:"SESSION_ABSOLUTE_TIMEOUT"`

	LoginMaxFailuresPerIP    int           `mapstructure:"LOGIN_MAX_FAILURES_PER_IP"`
	LoginMaxFailuresPerEmail int           `mapstructure:"LOGIN_MAX_FAILURES_PER_EMAIL"`
	LoginFailureWindow       time.Duration `mapstructure:"LOGIN_FAILURE_WINDOW"`

	// TrustedProxiesRaw is the comma-separated TRUSTED_PROXIES value;
	// TrustedProxies is its validated, trimmed form.
	TrustedProxiesRaw string   `mapstructure:"TRUSTED_PROXIES"`
	TrustedProxies    []string `mapstructure:"-"`

	// SetupToken, when set, must accompany first-run setup. Never logged.
	SetupToken string `mapstructure:"SETUP_TOKEN"`

	// EncryptionKey is 64 hex chars (32 bytes) for AES-256-GCM at-rest secrets.
	EncryptionKey string `mapstructure:"ENCRYPTION_KEY"`

	RunWorker       bool `mapstructure:"RUN_WORKER"`
	WebhookMaxRetry int  `mapstructure:"WEBHOOK_MAX_RETRY"`

	// ShutdownTimeout is the maximum duration to wait for graceful shutdown.
	// Defaults to 30 seconds if not set or invalid.
	ShutdownTimeout time.Duration `mapstructure:"SHUTDOWN_TIMEOUT"`

	// FrontendDir is the admin console build to serve. Empty means auto-detect.
	FrontendDir string `mapstructure:"FRONTEND_DIR"`
}

// envFiles are tried in order; the repository root .env is one level above
// backend/, where the server and migrate commands run.
var envFiles = []string{".env", "../.env"}

// Load reads configuration from environment variables and an optional .env file.
func Load() (*Config, error) {
	v := viper.New()

	for _, f := range envFiles {
		if _, err := os.Stat(f); err == nil {
			v.SetConfigFile(f)
			v.SetConfigType("env")
			_ = v.ReadInConfig() // env vars still bind below and take precedence.
			break
		}
	}

	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	setDefaults(v)

	// Bind keys explicitly so AutomaticEnv populates struct without a config file.
	for _, key := range []string{
		"APP_ENV", "HTTP_PORT", "HTTP_BASE_URL",
		"LOG_LEVEL", "LOG_FORMAT",
		"POSTGRES_DSN",

		"REDIS_ADDR", "REDIS_PASSWORD", "REDIS_DB",
		"XENDIT_SECRET_KEY", "XENDIT_CALLBACK_TOKEN",
		"JWT_SECRET",
		"COOKIE_SECURE", "SESSION_IDLE_TIMEOUT", "SESSION_ABSOLUTE_TIMEOUT",
		"LOGIN_MAX_FAILURES_PER_IP", "LOGIN_MAX_FAILURES_PER_EMAIL", "LOGIN_FAILURE_WINDOW",
		"TRUSTED_PROXIES", "SETUP_TOKEN",
		"ENCRYPTION_KEY",
		"RUN_WORKER", "WEBHOOK_MAX_RETRY",
		"SHUTDOWN_TIMEOUT",
		"FRONTEND_DIR",
	} {
		_ = v.BindEnv(key)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	cfg.JWTSecretSet = v.GetString("JWT_SECRET") != ""

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("APP_ENV", "development")
	v.SetDefault("HTTP_PORT", "8080")
	v.SetDefault("HTTP_BASE_URL", "http://localhost:8080")
	v.SetDefault("LOG_LEVEL", "info")
	v.SetDefault("REDIS_ADDR", "localhost:6379")

	v.SetDefault("REDIS_DB", 0)
	v.SetDefault("RUN_WORKER", true)
	v.SetDefault("WEBHOOK_MAX_RETRY", 5)
	v.SetDefault("SHUTDOWN_TIMEOUT", "30s")
	v.SetDefault("COOKIE_SECURE", "auto")
	v.SetDefault("SESSION_IDLE_TIMEOUT", "30m")
	v.SetDefault("SESSION_ABSOLUTE_TIMEOUT", "12h")
	v.SetDefault("LOGIN_MAX_FAILURES_PER_IP", 20)
	v.SetDefault("LOGIN_MAX_FAILURES_PER_EMAIL", 5)
	v.SetDefault("LOGIN_FAILURE_WINDOW", "15m")
}

func (c *Config) validate() error {
	var missing []string
	if c.PostgresDSN == "" {
		missing = append(missing, "POSTGRES_DSN")
	}
	if (c.XenditSecretKey == "") != (c.XenditCallbackToken == "") {
		return fmt.Errorf("XENDIT_SECRET_KEY and XENDIT_CALLBACK_TOKEN must be set together")
	}
	if len(c.EncryptionKey) != 64 {
		return fmt.Errorf("ENCRYPTION_KEY must be 64 hex chars (32 bytes)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}
	return c.validateAuth()
}

func (c *Config) validateAuth() error {
	switch strings.ToLower(strings.TrimSpace(c.CookieSecureMode)) {
	case "auto", "true", "false":
	default:
		return fmt.Errorf("COOKIE_SECURE must be auto, true or false")
	}
	if c.SessionIdleTimeout <= 0 {
		return fmt.Errorf("SESSION_IDLE_TIMEOUT must be greater than zero")
	}
	if c.SessionAbsoluteTimeout < c.SessionIdleTimeout {
		return fmt.Errorf("SESSION_ABSOLUTE_TIMEOUT must not be shorter than SESSION_IDLE_TIMEOUT")
	}
	if c.LoginMaxFailuresPerIP < 1 {
		return fmt.Errorf("LOGIN_MAX_FAILURES_PER_IP must be at least 1")
	}
	if c.LoginMaxFailuresPerEmail < 1 {
		return fmt.Errorf("LOGIN_MAX_FAILURES_PER_EMAIL must be at least 1")
	}
	if c.LoginFailureWindow <= 0 {
		return fmt.Errorf("LOGIN_FAILURE_WINDOW must be greater than zero")
	}
	proxies, err := parseTrustedProxies(c.TrustedProxiesRaw)
	if err != nil {
		return err
	}
	c.TrustedProxies = proxies
	return nil
}

// parseTrustedProxies splits a comma-separated list of IPs and CIDRs. An empty
// list means no proxy is trusted.
func parseTrustedProxies(raw string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if net.ParseIP(p) == nil {
			if _, _, err := net.ParseCIDR(p); err != nil {
				return nil, fmt.Errorf("TRUSTED_PROXIES: %q is not an IP address or CIDR", p)
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// CookieSecure reports whether admin cookies carry the Secure attribute: always
// for true, never for false, and outside development for auto.
func (c *Config) CookieSecure() bool {
	switch strings.ToLower(strings.TrimSpace(c.CookieSecureMode)) {
	case "true":
		return true
	case "false":
		return false
	default:
		return !strings.EqualFold(c.AppEnv, "development")
	}
}

// IsProduction reports whether the app runs in a production environment.
func (c *Config) IsProduction() bool {
	return strings.EqualFold(c.AppEnv, "production")
}

// GetShutdownTimeout returns the shutdown timeout duration.
// If the value is invalid or zero, returns the default of 30 seconds.
func (c *Config) GetShutdownTimeout() time.Duration {
	if c.ShutdownTimeout <= 0 {
		return 30 * time.Second
	}
	return c.ShutdownTimeout
}
