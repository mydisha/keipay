package crypto

import "testing"

const testKey = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c, err := New(testKey)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	plain := "sk_super_secret_value"
	enc, err := c.Encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if enc == plain {
		t.Fatal("ciphertext equals plaintext")
	}
	got, err := c.Decrypt(enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != plain {
		t.Fatalf("round trip mismatch: got %q want %q", got, plain)
	}
}

func TestEncryptUniqueNonce(t *testing.T) {
	c, _ := New(testKey)
	a, _ := c.Encrypt("same")
	b, _ := c.Encrypt("same")
	if a == b {
		t.Fatal("expected distinct ciphertexts due to random nonce")
	}
}

func TestNewRejectsBadKey(t *testing.T) {
	if _, err := New("tooshort"); err == nil {
		t.Fatal("expected error for short key")
	}
}
