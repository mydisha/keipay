package service

import (
	"encoding/base64"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/text/unicode/norm"

	"github.com/mydisha/keipay/backend/internal/domain"
)

func TestCanonicalEmail(t *testing.T) {
	nfd := norm.NFD.String("üser@example.com")
	if nfd == "üser@example.com" {
		t.Fatal("test input is not decomposed")
	}
	cases := map[string]string{
		"  Ops@Example.COM ":       "ops@example.com",
		"ÜSER@Example.COM":         "üser@example.com",
		nfd:                        "üser@example.com",
		"\tadmin@keipay.invalid\n": "admin@keipay.invalid",
	}
	for in, want := range cases {
		if got := CanonicalEmail(in); got != want {
			t.Errorf("CanonicalEmail(%q) = %q, want %q", in, got, want)
		}
	}
	if CanonicalEmail(nfd) != CanonicalEmail("ÜSER@example.com") {
		t.Fatal("NFD and NFC forms differ after canonicalization")
	}
}

func TestEmailRule(t *testing.T) {
	valid := []string{
		"ops@example.com",
		"üser@example.com",
		"a.b+tag@sub.example.co.id",
		"用户@例子.广告",
		strings.Repeat("a", 64) + "@example.com",
		"a@" + strings.Repeat("b", 248) + ".com",
	}
	for _, e := range valid {
		if rule := emailRule(CanonicalEmail(e)); rule != "" {
			t.Errorf("emailRule(%q) = %q, want valid", e, rule)
		}
	}
	invalid := map[string]string{
		"":                                       RuleRequired,
		"   ":                                    RuleRequired,
		"a":                                      RuleEmail,
		"a@b":                                    RuleEmail,
		"a@@b.com":                               RuleEmail,
		"a@b@c.com":                              RuleEmail,
		"a b@c.com":                              RuleEmail,
		"a@.com":                                 RuleEmail,
		"a@b..com":                               RuleEmail,
		"a@b.com.":                               RuleEmail,
		"@b.com":                                 RuleEmail,
		"a@":                                     RuleEmail,
		`"a"@b.com`:                              RuleEmail,
		"a<b>@c.com":                             RuleEmail,
		"a,b@c.com":                              RuleEmail,
		"a;b@c.com":                              RuleEmail,
		"a:b@c.com":                              RuleEmail,
		`a\b@c.com`:                              RuleEmail,
		"a(b)@c.com":                             RuleEmail,
		"a[b]@c.com":                             RuleEmail,
		"a\x00b@c.com":                           RuleEmail,
		"a\u0007@c.com":                          RuleEmail,
		"a b@c.com":                              RuleEmail,
		strings.Repeat("a", 65) + "@b.com":       RuleEmail,
		"a@" + strings.Repeat("b", 249) + ".com": RuleEmail, // 255 bytes
	}
	for e, want := range invalid {
		if got := emailRule(CanonicalEmail(e)); got != want {
			t.Errorf("emailRule(%q) = %q, want %q", e, got, want)
		}
	}
	if total := "a@" + strings.Repeat("b", 248) + ".com"; len(total) != 254 {
		t.Fatalf("boundary fixture is %d bytes", len(total))
	}
}

func TestNormalizeName(t *testing.T) {
	cases := []struct {
		in, want, rule string
	}{
		{"  Ops Lead  ", "Ops Lead", ""},
		{"", "", RuleRequired},
		{" \t ", "", RuleRequired},
		{strings.Repeat("é", 100), strings.Repeat("é", 100), ""},
		{strings.Repeat("🙂", 100), strings.Repeat("🙂", 100), ""},
		{strings.Repeat("a", 101), strings.Repeat("a", 101), RuleMax},
		{"Ops\x07Lead", "Ops\x07Lead", RuleName},
		{"Ops\u0085Lead", "Ops\u0085Lead", RuleName},
		{"Dewi Ünïcode 漢字", "Dewi Ünïcode 漢字", ""},
	}
	for _, tc := range cases {
		got, rule := normalizeName(tc.in)
		if rule != tc.rule || (rule == "" && got != tc.want) {
			t.Errorf("normalizeName(%q) = %q, %q; want %q, %q", tc.in, got, rule, tc.want, tc.rule)
		}
	}
}

func TestPasswordRule(t *testing.T) {
	cases := []struct {
		name, in, rule string
	}{
		{"11 runes", "abcdefghijk", RuleMin},
		{"12 ascii", "abcdefghijkl", ""},
		{"12 multibyte runes", "ééééééééééé1", ""},
		{"11 multibyte runes over 12 bytes", "ééééééééééé", RuleMin},
		{"72 bytes", strings.Repeat("a", 72), ""},
		{"73 bytes", strings.Repeat("a", 73), RuleMax},
		{"emoji over 72 bytes", strings.Repeat("🙂", 19), RuleMax},
		{"spaces count", "  abcdefghij", ""},
		{"empty", "", RuleMin},
	}
	for _, tc := range cases {
		if got := passwordRule(tc.in); got != tc.rule {
			t.Errorf("%s: passwordRule(%q) = %q, want %q", tc.name, tc.in, got, tc.rule)
		}
	}
}

// Passwords are hashed exactly as sent, surrounding spaces included.
func TestPasswordHashedWithoutTrimming(t *testing.T) {
	h := NewBcryptHasher(bcrypt.MinCost)
	hash, err := h.Hash("  padded-pass  ")
	if err != nil {
		t.Fatal(err)
	}
	if !h.Matches(hash, "  padded-pass  ") {
		t.Fatal("exact password does not match")
	}
	if h.Matches(hash, "padded-pass") || h.Matches(hash, "  padded-pass") {
		t.Fatal("trimmed password matched")
	}
	if h.Matches("not-a-hash", "  padded-pass  ") {
		t.Fatal("malformed hash matched")
	}
}

func TestBcryptHasherCost(t *testing.T) {
	hash, err := NewBcryptHasher(bcrypt.MinCost).Hash("abcdefghijkl")
	if err != nil {
		t.Fatal(err)
	}
	if cost, _ := bcrypt.Cost([]byte(hash)); cost != bcrypt.MinCost {
		t.Fatalf("cost = %d", cost)
	}
	if DefaultBcryptCost != 12 {
		t.Fatalf("production cost = %d, want 12", DefaultBcryptCost)
	}
}

func TestTemporaryPassword(t *testing.T) {
	seen := map[string]bool{}
	counts := map[rune]int{}
	for range 1000 {
		p, err := TemporaryPassword()
		if err != nil {
			t.Fatal(err)
		}
		if len(p) != 20 {
			t.Fatalf("len(%q) = %d", p, len(p))
		}
		for _, r := range p {
			if !strings.ContainsRune(temporaryAlphabet, r) {
				t.Fatalf("%q has %q outside the alphabet", p, r)
			}
			counts[r]++
		}
		if seen[p] {
			t.Fatalf("duplicate %q", p)
		}
		seen[p] = true
	}
	for _, r := range "0O1lI" {
		if strings.ContainsRune(temporaryAlphabet, r) || counts[r] > 0 {
			t.Fatalf("look-alike %q is possible", r)
		}
	}
	if len(counts) != len(temporaryAlphabet) {
		t.Fatalf("only %d of %d characters appeared in 20000 draws", len(counts), len(temporaryAlphabet))
	}
	if passwordRule(mustTemp(t)) != "" {
		t.Fatal("temporary password breaks the password rules")
	}
}

func mustTemp(t *testing.T) string {
	t.Helper()
	p, err := TemporaryPassword()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNewToken(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		tok, err := NewToken()
		if err != nil {
			t.Fatal(err)
		}
		if len(tok) != 43 {
			t.Fatalf("len(%q) = %d, want 43", tok, len(tok))
		}
		raw, err := base64.RawURLEncoding.DecodeString(tok)
		if err != nil || len(raw) != 32 {
			t.Fatalf("%q is not 32 bytes of base64url: %v", tok, err)
		}
		if seen[tok] {
			t.Fatalf("duplicate token %q", tok)
		}
		seen[tok] = true
	}
}

func TestSecureEqual(t *testing.T) {
	if !SecureEqual("abc", "abc") || SecureEqual("abc", "abd") || SecureEqual("abc", "abcd") || SecureEqual("", "x") {
		t.Fatal("SecureEqual is wrong")
	}
}

func TestTruncateBytes(t *testing.T) {
	if got := truncateBytes("ab"+"é", 3); got != "ab" {
		t.Fatalf("got %q, want a cut on the rune boundary", got)
	}
	if got := truncateBytes("short", 10); got != "short" {
		t.Fatalf("got %q", got)
	}
}

func TestFieldErrorsAreInvalidRequest(t *testing.T) {
	var fe fieldErrors
	if fe.err() != nil {
		t.Fatal("no fields should be no error")
	}
	fe.check("email", RuleEmail)
	fe.check("name", "")
	fe.check("password", RuleMin)
	err := fe.err()
	coded, ok := err.(*domain.Error)
	if !ok || coded.Code != domain.CodeInvalidRequest {
		t.Fatalf("err = %v", err)
	}
	fields := coded.Details["fields"].([]domain.FieldError)
	if len(fields) != 2 || fields[0] != (domain.FieldError{Field: "email", Rule: RuleEmail}) || fields[1].Field != "password" {
		t.Fatalf("fields = %+v", fields)
	}
}
