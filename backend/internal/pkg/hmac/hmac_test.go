package hmac

import "testing"

func TestSignVerify(t *testing.T) {
	secret := "sk_test_secret"
	payload := []byte(`{"order_id":"abc","amount":1000}`)

	sig := Sign(secret, payload)
	if sig == "" {
		t.Fatal("empty signature")
	}
	if !Verify(secret, payload, sig) {
		t.Fatal("valid signature rejected")
	}
	if Verify("wrong", payload, sig) {
		t.Fatal("wrong secret accepted")
	}
	if Verify(secret, []byte("tampered"), sig) {
		t.Fatal("tampered payload accepted")
	}
}
