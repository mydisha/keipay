package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

const dataKeyBytes = 32

// Seal encrypts plaintext with a fresh random data key, binding it to aad
// (e.g. the owning row's public id) so the ciphertext only opens for that
// row. It returns base64(nonce || ciphertext) and the data key encrypted
// with the master key; rotating the master key only re-encrypts the latter.
func (c *Cipher) Seal(plaintext []byte, aad string) (ciphertext, dataKeyEnc string, err error) {
	key := make([]byte, dataKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return "", "", fmt.Errorf("data key: %w", err)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", "", fmt.Errorf("nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, []byte(aad))
	dataKeyEnc, err = c.Encrypt(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		return "", "", fmt.Errorf("wrap data key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sealed), dataKeyEnc, nil
}

// Open reverses Seal. A different aad, a tampered ciphertext or a data key
// wrapped under another master key fails.
func (c *Cipher) Open(ciphertext, dataKeyEnc, aad string) ([]byte, error) {
	key, err := c.unwrap(dataKeyEnc)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}
	if len(data) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], []byte(aad))
	if err != nil {
		return nil, fmt.Errorf("open sealed data: %w", err)
	}
	return plain, nil
}

// Rewrap re-encrypts a data key made by Seal under another master key.
func (c *Cipher) Rewrap(dataKeyEnc string, to *Cipher) (string, error) {
	key, err := c.unwrap(dataKeyEnc)
	if err != nil {
		return "", err
	}
	return to.Encrypt(base64.StdEncoding.EncodeToString(key))
}

func (c *Cipher) unwrap(dataKeyEnc string) ([]byte, error) {
	encoded, err := c.Decrypt(dataKeyEnc)
	if err != nil {
		return nil, fmt.Errorf("unwrap data key: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != dataKeyBytes {
		return nil, errors.New("unwrap data key: malformed key")
	}
	return key, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
