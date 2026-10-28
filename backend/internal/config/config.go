package config

import (
	"fmt"
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

	XenditSecretKey     string `mapstructure:"XENDIT_SECRET_KEY"`
	XenditCallbackToken string `mapstructure:"XENDIT_CALLBACK_TOKEN"`

	JWTSecret string `mapstructure:"JWT_SECRET"`

	// EncryptionKey is 64 hex chars (32 bytes) for AES-256-GCM at-rest secrets.
	EncryptionKey string `mapstructure:"ENCRYPTION_KEY"`

	RunWorker       bool `mapstructure:"RUN_WORKER"`
	WebhookMaxRetry int  `mapstructure:"WEBHOOK_MAX_RETRY"`

	// ShutdownTimeout is the maximum duration to wait for graceful shutdown.
	// Defaults to 30 seconds if not set or invalid.
	ShutdownTimeout time.Duration `mapstructure:"SHUTDOWN_TIMEOUT"`
}

// Load reads configuration from environment variables and an optional .env file.
func Load() (*Config, error) {
	v := viper.New()

	v.SetConfigFile(".env")
	v.SetConfigType("env")
	_ = v.ReadInConfig() // .env is optional; env vars still bind below.

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
		"ENCRYPTION_KEY",
		"RUN_WORKER", "WEBHOOK_MAX_RETRY",
		"SHUTDOWN_TIMEOUT",
	} {
		_ = v.BindEnv(key)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

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
}

func (c *Config) validate() error {
	var missing []string
	if c.PostgresDSN == "" {
		missing = append(missing, "POSTGRES_DSN")
	}
	if c.XenditSecretKey == "" {
		missing = append(missing, "XENDIT_SECRET_KEY")
	}
	if c.XenditCallbackToken == "" {
		missing = append(missing, "XENDIT_CALLBACK_TOKEN")
	}
	if c.JWTSecret == "" {
		missing = append(missing, "JWT_SECRET")
	}
	if len(c.EncryptionKey) != 64 {
		return fmt.Errorf("ENCRYPTION_KEY must be 64 hex chars (32 bytes)")
	}
	if len(missing) > 0 {

		return fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}
	return nil
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
