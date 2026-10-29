package worker

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// VerifyKeiPaySignature reports whether header, the X-KeiPay-Signature
// value, signs body with secret and is at most tolerance away from now.
func VerifyKeiPaySignature(header string, body []byte, secret string, tolerance time.Duration, now time.Time) bool {
	var ts string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	t, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	if d := now.Sub(time.Unix(t, 0)); d > tolerance || d < -tolerance {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	for _, sig := range sigs {
		if hmac.Equal([]byte(sig), []byte(want)) {
			return true
		}
	}
	return false
}

// T-B30: the verifier above is the README's, character for character, so the
// documented code is the code these tests run.
func TestReadmeVerifierIsTested(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", "..", "..", "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	self, err := os.ReadFile("readme_verify_test.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(self)
	from := strings.Index(src, "// VerifyKeiPaySignature reports")
	length := strings.Index(src[max(from, 0):], "\n}\n")
	if from < 0 || length < 0 {
		t.Fatal("verifier not found in this file")
	}
	fn := src[from : from+length+len("\n}")]
	if !strings.Contains(string(readme), fn) {
		t.Fatalf("README.md does not contain the tested verifier verbatim:\n%s", fn)
	}
}
