package crypto

import (
	"bytes"
	"encoding/base64"
	"testing"
)

const otherKey = "1f1e1d1c1b1a191817161514131211100f0e0d0c0b0a09080706050403020100"

func TestSealOpen(t *testing.T) {
	c, _ := New(testKey)
	plain := []byte(`{"secret_key":"xnd_development_abc","callback_token":"tok"}`)
	ct, dek, err := c.Seal(plain, "con_A")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains([]byte(ct), []byte("xnd_")) {
		t.Fatal("ciphertext contains the plaintext")
	}
	got, err := c.Open(ct, dek, "con_A")
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("open = %q %v", got, err)
	}

	// Every seal uses a new data key and nonce.
	ct2, dek2, _ := c.Seal(plain, "con_A")
	if ct2 == ct || dek2 == dek {
		t.Fatal("seal reused a data key or nonce")
	}
	raw1, _ := base64.StdEncoding.DecodeString(ct)
	raw2, _ := base64.StdEncoding.DecodeString(ct2)
	if bytes.Equal(raw1[:12], raw2[:12]) {
		t.Fatal("nonce reused")
	}

	// Ciphertext moved to another row does not open.
	if _, err := c.Open(ct, dek, "con_B"); err == nil {
		t.Fatal("opened with another public id")
	}
	// Tampering is detected.
	raw1[len(raw1)-1] ^= 1
	if _, err := c.Open(base64.StdEncoding.EncodeToString(raw1), dek, "con_A"); err == nil {
		t.Fatal("tampered ciphertext opened")
	}
	// Another master key cannot unwrap the data key.
	other, _ := New(otherKey)
	if _, err := other.Open(ct, dek, "con_A"); err == nil {
		t.Fatal("opened with another master key")
	}
	// Mixing the data key of one seal with the ciphertext of another fails.
	if _, err := c.Open(ct, dek2, "con_A"); err == nil {
		t.Fatal("opened with another data key")
	}
	for _, bad := range [][2]string{{"%%%", dek}, {ct, "%%%"}, {"AAAA", dek}} {
		if _, err := c.Open(bad[0], bad[1], "con_A"); err == nil {
			t.Fatalf("malformed input %q opened", bad)
		}
	}
}

// Rotating the master key re-wraps data keys only; sealed data stays.
func TestRewrapUnderNewMasterKey(t *testing.T) {
	old, _ := New(testKey)
	next, _ := New(otherKey)
	ct, dek, _ := old.Seal([]byte("secret"), "con_A")
	rewrapped, err := old.Rewrap(dek, next)
	if err != nil {
		t.Fatal(err)
	}
	got, err := next.Open(ct, rewrapped, "con_A")
	if err != nil || string(got) != "secret" {
		t.Fatalf("open after rewrap = %q %v", got, err)
	}
	if _, err := old.Open(ct, rewrapped, "con_A"); err == nil {
		t.Fatal("old master key still opens the rewrapped data key")
	}
	if _, err := next.Rewrap(dek, old); err == nil {
		t.Fatal("rewrap with the wrong master key succeeded")
	}
}
