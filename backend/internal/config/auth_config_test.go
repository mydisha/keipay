package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// loadWith writes a valid root .env, applies env overrides and loads.
func loadWith(t *testing.T, env map[string]string) (*Config, error) {
	t.Helper()
	root := chdirBackend(t)
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(envBody("postgres://root")), 0o600); err != nil {
		t.Fatal(err)
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	return Load()
}

func TestLoad_AuthDefaults(t *testing.T) {
	cfg, err := loadWith(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionIdleTimeout != 30*time.Minute || cfg.SessionAbsoluteTimeout != 12*time.Hour {
		t.Fatalf("timeouts = %v / %v", cfg.SessionIdleTimeout, cfg.SessionAbsoluteTimeout)
	}
	if cfg.LoginMaxFailuresPerIP != 20 || cfg.LoginMaxFailuresPerEmail != 5 || cfg.LoginFailureWindow != 15*time.Minute {
		t.Fatalf("limits = %d %d %v", cfg.LoginMaxFailuresPerIP, cfg.LoginMaxFailuresPerEmail, cfg.LoginFailureWindow)
	}
	if cfg.TrustedProxies != nil || cfg.SetupToken != "" || cfg.JWTSecretSet {
		t.Fatalf("proxies=%v setup=%q jwt=%v", cfg.TrustedProxies, cfg.SetupToken, cfg.JWTSecretSet)
	}
	// APP_ENV defaults to development, so auto means no Secure flag.
	if cfg.CookieSecureMode != "auto" || cfg.CookieSecure() {
		t.Fatalf("cookie secure = %q %v", cfg.CookieSecureMode, cfg.CookieSecure())
	}
}

func TestLoad_JWTSecretNotRequiredButDetected(t *testing.T) {
	cfg, err := loadWith(t, nil)
	if err != nil || cfg.JWTSecretSet {
		t.Fatalf("without JWT_SECRET: %v, set=%v", err, cfg.JWTSecretSet)
	}
	cfg, err = loadWith(t, map[string]string{"JWT_SECRET": "legacy-value"})
	if err != nil || !cfg.JWTSecretSet {
		t.Fatalf("with JWT_SECRET: %v, set=%v", err, cfg.JWTSecretSet)
	}
}

func TestLoad_CookieSecure(t *testing.T) {
	cases := []struct {
		mode, env string
		want      bool
	}{
		{"auto", "development", false},
		{"auto", "Development", false},
		{"AUTO", "production", true},
		{"auto", "staging", true},
		{"true", "development", true},
		{"TRUE", "development", true},
		{"false", "staging", false},
		{"False", "staging", false},
	}
	for _, tc := range cases {
		cfg, err := loadWith(t, map[string]string{"COOKIE_SECURE": tc.mode, "APP_ENV": tc.env})
		if err != nil {
			t.Fatalf("%s/%s: %v", tc.mode, tc.env, err)
		}
		if got := cfg.CookieSecure(); got != tc.want {
			t.Errorf("COOKIE_SECURE=%s APP_ENV=%s: secure = %v, want %v", tc.mode, tc.env, got, tc.want)
		}
	}
}

func TestLoad_FailureLimitDefaults(t *testing.T) {
	cfg, err := loadWith(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.V1AuthMaxFailuresPerIP != 100 || cfg.V1AuthMaxFailuresPerKey != 50 || cfg.V1AuthFailureWindow != 15*time.Minute {
		t.Fatalf("v1 limits = %d/%d/%v", cfg.V1AuthMaxFailuresPerIP, cfg.V1AuthMaxFailuresPerKey, cfg.V1AuthFailureWindow)
	}
	if cfg.CallbackMaxRejectedPerIP != 60 || cfg.CallbackMaxRejectedPerConnection != 60 || cfg.CallbackRejectedWindow != 15*time.Minute {
		t.Fatalf("callback limits = %d/%d/%v", cfg.CallbackMaxRejectedPerIP, cfg.CallbackMaxRejectedPerConnection, cfg.CallbackRejectedWindow)
	}
	cfg, err = loadWith(t, map[string]string{"V1_AUTH_MAX_FAILURES_PER_KEY": "7", "CALLBACK_REJECTED_WINDOW": "1h"})
	if err != nil || cfg.V1AuthMaxFailuresPerKey != 7 || cfg.CallbackRejectedWindow != time.Hour {
		t.Fatalf("overrides: %+v %v", cfg, err)
	}
}

// HSTS follows the Secure cookie decision unless set explicitly.
func TestLoad_HSTSEnabled(t *testing.T) {
	cases := []struct {
		hsts, cookie, env string
		want              bool
	}{
		{"auto", "auto", "development", false},
		{"auto", "auto", "production", true},
		{"auto", "false", "staging", false},
		{"auto", "true", "development", true},
		{"true", "false", "development", true},
		{"FALSE", "true", "production", false},
	}
	for _, tc := range cases {
		cfg, err := loadWith(t, map[string]string{"HSTS_ENABLED": tc.hsts, "COOKIE_SECURE": tc.cookie, "APP_ENV": tc.env})
		if err != nil {
			t.Fatalf("%s/%s/%s: %v", tc.hsts, tc.cookie, tc.env, err)
		}
		if got := cfg.HSTSEnabled(); got != tc.want {
			t.Errorf("HSTS_ENABLED=%s COOKIE_SECURE=%s APP_ENV=%s: hsts = %v, want %v", tc.hsts, tc.cookie, tc.env, got, tc.want)
		}
	}
}

func TestLoad_AuthValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"cookie maybe", map[string]string{"COOKIE_SECURE": "maybe"}, "COOKIE_SECURE"},
		{"hsts maybe", map[string]string{"HSTS_ENABLED": "maybe"}, "HSTS_ENABLED"},
		{"v1 ip limit zero", map[string]string{"V1_AUTH_MAX_FAILURES_PER_IP": "0"}, "V1_AUTH_MAX_FAILURES_PER_IP"},
		{"v1 key limit zero", map[string]string{"V1_AUTH_MAX_FAILURES_PER_KEY": "0"}, "V1_AUTH_MAX_FAILURES_PER_KEY"},
		{"v1 window zero", map[string]string{"V1_AUTH_FAILURE_WINDOW": "0s"}, "V1_AUTH_FAILURE_WINDOW"},
		{"callback ip limit zero", map[string]string{"CALLBACK_MAX_REJECTED_PER_IP": "0"}, "CALLBACK_MAX_REJECTED_PER_IP"},
		{"callback connection limit zero", map[string]string{"CALLBACK_MAX_REJECTED_PER_CONNECTION": "0"}, "CALLBACK_MAX_REJECTED_PER_CONNECTION"},
		{"callback window zero", map[string]string{"CALLBACK_REJECTED_WINDOW": "-1m"}, "CALLBACK_REJECTED_WINDOW"},
		{"idle zero", map[string]string{"SESSION_IDLE_TIMEOUT": "0s"}, "SESSION_IDLE_TIMEOUT"},
		{"idle negative", map[string]string{"SESSION_IDLE_TIMEOUT": "-1m"}, "SESSION_IDLE_TIMEOUT"},
		{"absolute below idle", map[string]string{"SESSION_IDLE_TIMEOUT": "2h", "SESSION_ABSOLUTE_TIMEOUT": "1h"}, "SESSION_ABSOLUTE_TIMEOUT"},
		{"ip limit zero", map[string]string{"LOGIN_MAX_FAILURES_PER_IP": "0"}, "LOGIN_MAX_FAILURES_PER_IP"},
		{"email limit zero", map[string]string{"LOGIN_MAX_FAILURES_PER_EMAIL": "0"}, "LOGIN_MAX_FAILURES_PER_EMAIL"},
		{"window zero", map[string]string{"LOGIN_FAILURE_WINDOW": "0s"}, "LOGIN_FAILURE_WINDOW"},
		{"cidr /33", map[string]string{"TRUSTED_PROXIES": "10.0.0.0/33"}, "TRUSTED_PROXIES"},
		{"not an ip", map[string]string{"TRUSTED_PROXIES": "127.0.0.1, nope"}, "TRUSTED_PROXIES"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadWith(t, tc.env)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want mention of %s", err, tc.want)
			}
		})
	}
}

func TestLoad_AuthOverrides(t *testing.T) {
	cfg, err := loadWith(t, map[string]string{
		"SESSION_IDLE_TIMEOUT":         "5m",
		"SESSION_ABSOLUTE_TIMEOUT":     "5m",
		"LOGIN_MAX_FAILURES_PER_IP":    "1",
		"LOGIN_MAX_FAILURES_PER_EMAIL": "2",
		"LOGIN_FAILURE_WINDOW":         "90s",
		"TRUSTED_PROXIES":              " 10.0.0.0/8, 127.0.0.1 ,,::1",
		"SETUP_TOKEN":                  "setup-Ω",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionIdleTimeout != 5*time.Minute || cfg.SessionAbsoluteTimeout != 5*time.Minute {
		t.Fatalf("timeouts = %v / %v", cfg.SessionIdleTimeout, cfg.SessionAbsoluteTimeout)
	}
	if cfg.LoginMaxFailuresPerIP != 1 || cfg.LoginMaxFailuresPerEmail != 2 || cfg.LoginFailureWindow != 90*time.Second {
		t.Fatalf("limits = %d %d %v", cfg.LoginMaxFailuresPerIP, cfg.LoginMaxFailuresPerEmail, cfg.LoginFailureWindow)
	}
	if want := []string{"10.0.0.0/8", "127.0.0.1", "::1"}; !slices.Equal(cfg.TrustedProxies, want) {
		t.Fatalf("proxies = %q, want %q", cfg.TrustedProxies, want)
	}
	if cfg.SetupToken != "setup-Ω" {
		t.Fatalf("setup token = %q", cfg.SetupToken)
	}
}
