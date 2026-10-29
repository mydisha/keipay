package domain

import (
	"encoding/json"
	"testing"
	"time"
)

// Status is derived at the exact instant; revocation wins over expiry.
func TestAPIKeyStatus(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	cases := []struct {
		name string
		key  APIKey
		want APIKeyStatus
	}{
		{"no expiry", APIKey{}, APIKeyActive},
		{"1ns before expiry", APIKey{ExpiresAt: at(time.Nanosecond)}, APIKeyActive},
		{"at expiry", APIKey{ExpiresAt: at(0)}, APIKeyExpired},
		{"after expiry", APIKey{ExpiresAt: at(-time.Hour)}, APIKeyExpired},
		{"revoked", APIKey{RevokedAt: at(-time.Hour)}, APIKeyRevoked},
		{"revoked and expired", APIKey{RevokedAt: at(-time.Hour), ExpiresAt: at(-2 * time.Hour)}, APIKeyRevoked},
		{"revoked in the future still revoked", APIKey{RevokedAt: at(time.Hour)}, APIKeyRevoked},
	}
	for _, tc := range cases {
		if got := tc.key.Status(now); got != tc.want {
			t.Errorf("%s: status = %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestAPIKeyHint(t *testing.T) {
	k := APIKey{Prefix: "kp_test_", Last4: "a1b2"}
	if got := k.Hint(); got != "kp_test_…a1b2" {
		t.Fatalf("hint = %q", got)
	}
}

func TestEnvironmentAndStatusValid(t *testing.T) {
	for _, e := range []Environment{EnvTest, EnvLive} {
		if !e.Valid() {
			t.Errorf("%s invalid", e)
		}
	}
	for _, e := range []Environment{"", "TEST", "prod", "test "} {
		if e.Valid() {
			t.Errorf("%q valid", e)
		}
	}
	for _, s := range []AppStatus{AppActive, AppDisabled} {
		if !s.Valid() {
			t.Errorf("%s invalid", s)
		}
	}
	for _, s := range []AppStatus{"", "Active", "inactive"} {
		if s.Valid() {
			t.Errorf("%q valid", s)
		}
	}
}

func TestMetadataMarshalsEmptyAsObject(t *testing.T) {
	for _, m := range []Metadata{nil, {}} {
		b, err := json.Marshal(m)
		if err != nil || string(b) != "{}" {
			t.Fatalf("marshal %v = %s, %v", m, b, err)
		}
	}
	var s struct {
		M Metadata `json:"m"`
	}
	b, _ := json.Marshal(s)
	if string(b) != `{"m":{}}` {
		t.Fatalf("zero field = %s", b)
	}
	b, _ = json.Marshal(Metadata{"n": json.Number("12345678901234567890"), "f": json.Number("0.1"), "s": "ü", "b": true, "z": nil})
	if string(b) != `{"b":true,"f":0.1,"n":12345678901234567890,"s":"ü","z":null}` {
		t.Fatalf("marshal = %s", b)
	}
}

func TestAppSummary(t *testing.T) {
	a := &App{ID: 7, PublicID: "app_X", Slug: "acme", Name: "Acme", Environment: EnvLive}
	if got := a.Summary(); got != (AppSummary{PublicID: "app_X", Slug: "acme", Name: "Acme", Environment: EnvLive}) {
		t.Fatalf("summary = %+v", got)
	}
}
