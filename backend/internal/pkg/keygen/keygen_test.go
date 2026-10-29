package keygen

import (
	"regexp"
	"testing"
)

// Formats, uniqueness and a known hash vector.
func TestFormats(t *testing.T) {
	keyRe := regexp.MustCompile(`^kp_(test|live)_[0-9a-f]{32}$`)
	secretRe := regexp.MustCompile(`^kps_(test|live)_[0-9a-f]{48}$`)
	whRe := regexp.MustCompile(`^kpwh_[0-9a-f]{48}$`)
	for _, env := range []string{"test", "live"} {
		if k := APIKey(env); !keyRe.MatchString(k) || k[:len(KeyPrefixFor(env))] != "kp_"+env+"_" {
			t.Errorf("APIKey(%s) = %q", env, k)
		}
		if s := APISecret(env); !secretRe.MatchString(s) || s[:5+len(env)] != "kps_"+env+"_" {
			t.Errorf("APISecret(%s) = %q", env, s)
		}
	}
	if w := WebhookSecret(); !whRe.MatchString(w) {
		t.Errorf("WebhookSecret = %q", w)
	}
}

func TestUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		for _, v := range []string{APIKey("test"), APISecret("live"), WebhookSecret()} {
			if seen[v] {
				t.Fatalf("duplicate %q", v)
			}
			seen[v] = true
		}
	}
}

func TestHashAPIKey(t *testing.T) {
	// sha256("abc"), FIPS 180-2 test vector.
	if got := HashAPIKey("abc"); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("hash = %s", got)
	}
	if HashAPIKey("kp_a") == HashAPIKey("kp_b") {
		t.Fatal("different keys hash the same")
	}
}

func TestParseKeyEnvironment(t *testing.T) {
	cases := []struct {
		key    string
		env    string
		wantOK bool
	}{
		{"kp_test_0123456789abcdef0123456789abcdef", "test", true},
		{"kp_live_0123456789abcdef0123456789abcdef", "live", true},
		{"kp_0123456789abcdef0123456789abcdef", "", true},
		{"kp_", "", true},
		{"kp_testing", "", true},
		{"kps_test_abc", "", false},
		{"sk_live_abc", "", false},
		{"", "", false},
		{"KP_TEST_abc", "", false},
	}
	for _, tc := range cases {
		env, ok := ParseKeyEnvironment(tc.key)
		if env != tc.env || ok != tc.wantOK {
			t.Errorf("ParseKeyEnvironment(%q) = %q, %v; want %q, %v", tc.key, env, ok, tc.env, tc.wantOK)
		}
	}
}
