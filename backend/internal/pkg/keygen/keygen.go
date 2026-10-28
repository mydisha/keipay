// Package keygen generates random API keys and secrets.
package keygen

import (
	"crypto/rand"
	"encoding/hex"
)

func random(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// APIKey returns a public tenant API key with a recognizable prefix.
func APIKey() string { return "kp_" + random(16) }

// Secret returns a tenant API secret used for HMAC signing.
func Secret() string { return "sk_" + random(24) }
