// Package hmac provides HMAC-SHA256 signing and constant-time verification
// used for authenticating tenant API requests and signing outbound webhooks.
package hmac

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// Sign returns the hex-encoded HMAC-SHA256 of payload using secret.
func Sign(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify reports whether signature is a valid hex HMAC-SHA256 of payload.
// Comparison is constant-time to avoid timing attacks.
func Verify(secret string, payload []byte, signature string) bool {
	expected := Sign(secret, payload)
	return hmac.Equal([]byte(expected), []byte(signature))
}
