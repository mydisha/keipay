// Package keygen generates app API keys and secrets and hashes keys for
// lookup.
package keygen

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// KeyPrefix is the start of every API key. Keys issued before apps are
// KeyPrefix + 32 hex; newer keys also carry the environment ("kp_test_").
const KeyPrefix = "kp_"

func random(n int) string {
	b := make([]byte, n)
	// crypto/rand.Read never returns an error on supported platforms.
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// KeyPrefixFor returns the key prefix of an environment, e.g. "kp_test_".
func KeyPrefixFor(env string) string { return KeyPrefix + env + "_" }

// APIKey returns a new API key for env: "kp_<env>_" + 32 lower-case hex.
func APIKey(env string) string { return KeyPrefixFor(env) + random(16) }

// APISecret returns a new HMAC secret for env: "kps_<env>_" + 48 lower-case hex.
func APISecret(env string) string { return "kps_" + env + "_" + random(24) }

// WebhookSecret returns a new webhook signing secret: "kpwh_" + 48 lower-case hex.
func WebhookSecret() string { return "kpwh_" + random(24) }

// HashAPIKey is the stored lookup form of a key: lower-case hex SHA-256 of
// its UTF-8 bytes. Migration 000007 computes the same value in SQL.
func HashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

// ParseKeyEnvironment returns the environment named by a key's prefix ("test"
// or "live"), "" with ok for a legacy "kp_" key without one, and ok false for
// anything that is not a KeiPay key.
func ParseKeyEnvironment(key string) (env string, ok bool) {
	rest, found := strings.CutPrefix(key, KeyPrefix)
	if !found {
		return "", false
	}
	for _, e := range []string{"test", "live"} {
		if strings.HasPrefix(rest, e+"_") {
			return e, true
		}
	}
	return "", true
}
