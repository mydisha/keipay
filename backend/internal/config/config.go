package config

import (
	"fmt"
	"net"
	"net/url"
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
	CookieSecureMode string `mapstructure:"COOKIE_SECURE"`
	// HSTSMode is auto, true or false; see HSTSEnabled.
	HSTSMode               string        `mapstructure:"HSTS_ENABLED"`
	SessionIdleTimeout     time.Duration `mapstructure:"SESSION_IDLE_TIMEOUT"`
	SessionAbsoluteTimeout time.Duration `mapstructure:"SESSION_ABSOLUTE_TIMEOUT"`

	LoginMaxFailuresPerIP    int           `mapstructure:"LOGIN_MAX_FAILURES_PER_IP"`
	LoginMaxFailuresPerEmail int           `mapstructure:"LOGIN_MAX_FAILURES_PER_EMAIL"`
	LoginFailureWindow       time.Duration `mapstructure:"LOGIN_FAILURE_WINDOW"`

	// V1AuthMaxFailuresPerIP and V1AuthMaxFailuresPerKey cap the /v1
	// authentication failures (unknown key, bad signature, replay) a client
	// IP and an API key may make within V1AuthFailureWindow.
	V1AuthMaxFailuresPerIP  int           `mapstructure:"V1_AUTH_MAX_FAILURES_PER_IP"`
	V1AuthMaxFailuresPerKey int           `mapstructure:"V1_AUTH_MAX_FAILURES_PER_KEY"`
	V1AuthFailureWindow     time.Duration `mapstructure:"V1_AUTH_FAILURE_WINDOW"`

	// CallbackMaxRejectedPerIP and CallbackMaxRejectedPerConnection cap the
	// gateway callbacks that fail verification or parsing, per sender IP and
	// per connection, within CallbackRejectedWindow. Accepted callbacks are
	// never limited.
	CallbackMaxRejectedPerIP         int           `mapstructure:"CALLBACK_MAX_REJECTED_PER_IP"`
	CallbackMaxRejectedPerConnection int           `mapstructure:"CALLBACK_MAX_REJECTED_PER_CONNECTION"`
	CallbackRejectedWindow           time.Duration `mapstructure:"CALLBACK_REJECTED_WINDOW"`

	// TrustedProxiesRaw is the comma-separated TRUSTED_PROXIES value;
	// TrustedProxies is its validated, trimmed form.
	TrustedProxiesRaw string   `mapstructure:"TRUSTED_PROXIES"`
	TrustedProxies    []string `mapstructure:"-"`

	// SetupToken, when set, must accompany first-run setup. Never logged.
	SetupToken string `mapstructure:"SETUP_TOKEN"`

	// EncryptionKey is 64 hex chars (32 bytes) for AES-256-GCM at-rest secrets.
	EncryptionKey string `mapstructure:"ENCRYPTION_KEY"`

	RunWorker bool `mapstructure:"RUN_WORKER"`
	// WebhookDisableAfterFailures disables a webhook endpoint after this many
	// deliveries in a row ran out of attempts; 0 never does.
	WebhookDisableAfterFailures int `mapstructure:"WEBHOOK_DISABLE_AFTER_FAILURES"`

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
		"COOKIE_SECURE", "HSTS_ENABLED", "SESSION_IDLE_TIMEOUT", "SESSION_ABSOLUTE_TIMEOUT",
		"LOGIN_MAX_FAILURES_PER_IP", "LOGIN_MAX_FAILURES_PER_EMAIL", "LOGIN_FAILURE_WINDOW",
		"V1_AUTH_MAX_FAILURES_PER_IP", "V1_AUTH_MAX_FAILURES_PER_KEY", "V1_AUTH_FAILURE_WINDOW",
		"CALLBACK_MAX_REJECTED_PER_IP", "CALLBACK_MAX_REJECTED_PER_CONNECTION", "CALLBACK_REJECTED_WINDOW",
		"TRUSTED_PROXIES", "SETUP_TOKEN",
		"ENCRYPTION_KEY",
		"RUN_WORKER", "WEBHOOK_DISABLE_AFTER_FAILURES",
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
	v.SetDefault("WEBHOOK_DISABLE_AFTER_FAILURES", 5)
	v.SetDefault("SHUTDOWN_TIMEOUT", "30s")
	v.SetDefault("COOKIE_SECURE", "auto")
	v.SetDefault("HSTS_ENABLED", "auto")
	v.SetDefault("SESSION_IDLE_TIMEOUT", "30m")
	v.SetDefault("SESSION_ABSOLUTE_TIMEOUT", "12h")
	v.SetDefault("LOGIN_MAX_FAILURES_PER_IP", 20)
	v.SetDefault("LOGIN_MAX_FAILURES_PER_EMAIL", 5)
	v.SetDefault("LOGIN_FAILURE_WINDOW", "15m")
	v.SetDefault("V1_AUTH_MAX_FAILURES_PER_IP", 100)
	v.SetDefault("V1_AUTH_MAX_FAILURES_PER_KEY", 50)
	v.SetDefault("V1_AUTH_FAILURE_WINDOW", "15m")
	v.SetDefault("CALLBACK_MAX_REJECTED_PER_IP", 60)
	v.SetDefault("CALLBACK_MAX_REJECTED_PER_CONNECTION", 60)
	v.SetDefault("CALLBACK_REJECTED_WINDOW", "15m")
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
	if c.WebhookDisableAfterFailures < 0 {
		return fmt.Errorf("WEBHOOK_DISABLE_AFTER_FAILURES must be 0 (never) or more")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}
	if c.IsProduction() && strings.EqualFold(strings.TrimSpace(c.CookieSecureMode), "false") {
		return fmt.Errorf("COOKIE_SECURE=false is not allowed when APP_ENV=production; serve the console over HTTPS")
	}
	return c.validateAuth()
}

// Warnings lists production settings that work but weaken the deployment.
// They are logged at startup; none of them stops the server.
func (c *Config) Warnings() []string {
	if !c.IsProduction() {
		return nil
	}
	var out []string
	if c.RedisPassword == "" {
		out = append(out, "REDIS_PASSWORD is empty: Redis holds sessions, nonces and rate limits, so require AUTH on it")
	}
	if dsnSSLMode(c.PostgresDSN) == "disable" {
		out = append(out, "POSTGRES_DSN has sslmode=disable: payments and encrypted secrets travel in clear to the database")
	}
	if c.SetupToken == "" {
		out = append(out, "SETUP_TOKEN is empty: first-run setup is refused until it is set")
	}
	return out
}

// dsnSSLMode reads sslmode from a URL (postgres://…?sslmode=x) or key=value
// DSN; "" when it is not set or the DSN cannot be read.
func dsnSSLMode(dsn string) string {
	if u, err := url.Parse(dsn); err == nil && u.Scheme != "" {
		return u.Query().Get("sslmode")
	}
	for _, part := range strings.Fields(dsn) {
		if v, ok := strings.CutPrefix(part, "sslmode="); ok {
			return strings.Trim(v, "'\"")
		}
	}
	return ""
}

func (c *Config) validateAuth() error {
	switch strings.ToLower(strings.TrimSpace(c.CookieSecureMode)) {
	case "auto", "true", "false":
	default:
		return fmt.Errorf("COOKIE_SECURE must be auto, true or false")
	}
	switch strings.ToLower(strings.TrimSpace(c.HSTSMode)) {
	case "auto", "true", "false":
	default:
		return fmt.Errorf("HSTS_ENABLED must be auto, true or false")
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
	for name, n := range map[string]int{
		"V1_AUTH_MAX_FAILURES_PER_IP": c.V1AuthMaxFailuresPerIP, "V1_AUTH_MAX_FAILURES_PER_KEY": c.V1AuthMaxFailuresPerKey,
		"CALLBACK_MAX_REJECTED_PER_IP": c.CallbackMaxRejectedPerIP, "CALLBACK_MAX_REJECTED_PER_CONNECTION": c.CallbackMaxRejectedPerConnection,
	} {
		if n < 1 {
			return fmt.Errorf("%s must be at least 1", name)
		}
	}
	for name, d := range map[string]time.Duration{"V1_AUTH_FAILURE_WINDOW": c.V1AuthFailureWindow, "CALLBACK_REJECTED_WINDOW": c.CallbackRejectedWindow} {
		if d <= 0 {
			return fmt.Errorf("%s must be greater than zero", name)
		}
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

// HSTSEnabled reports whether responses carry Strict-Transport-Security:
// always for true, never for false, and for auto whenever cookies are
// Secure, since both answer the same question: do browsers reach this
// server over HTTPS. Browsers ignore the header over plain HTTP, so sending
// it behind a TLS-terminating proxy is safe; an operator who sets the
// policy at the proxy can turn it off here.
func (c *Config) HSTSEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(c.HSTSMode)) {
	case "true":
		return true
	case "false":
		return false
	default:
		return c.CookieSecure()
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
