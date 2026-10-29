package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

func TestNormalizeSlug(t *testing.T) {
	cases := []struct{ in, want, rule string }{
		{" Acme-Shop ", "acme-shop", ""},
		{"abc", "abc", ""},
		{strings.Repeat("a", 32), strings.Repeat("a", 32), ""},
		{"a1-2b", "a1-2b", ""},
		{"ab", "ab", RuleSlug},
		{strings.Repeat("a", 33), strings.Repeat("a", 33), RuleSlug},
		{"-abc", "-abc", RuleSlug},
		{"abc-", "abc-", RuleSlug},
		{"ab_c", "ab_c", RuleSlug},
		{"ácme", "ácme", RuleSlug},
		{"a b c", "a b c", RuleSlug},
		{"", "", RuleRequired},
		{"   ", "", RuleRequired},
	}
	for _, tc := range cases {
		got, rule := normalizeSlug(tc.in)
		if got != tc.want || rule != tc.rule {
			t.Errorf("normalizeSlug(%q) = %q, %q; want %q, %q", tc.in, got, rule, tc.want, tc.rule)
		}
	}
}

func TestEnvironmentRule(t *testing.T) {
	for env, want := range map[domain.Environment]string{"test": "", "live": "", "": RuleRequired, "prod": RuleOneOf, "TEST": RuleOneOf} {
		if got := environmentRule(env); got != want {
			t.Errorf("environmentRule(%q) = %q, want %q", env, got, want)
		}
	}
}

func TestNormalizeURL(t *testing.T) {
	long := "https://a.example/" + strings.Repeat("x", 2048-len("https://a.example/"))
	cases := []struct{ in, want, rule string }{
		{"", "", ""},
		{"   ", "", ""},
		{" https://a.example/x ", "https://a.example/x", ""},
		{"http://localhost:8080/hook?a=1", "http://localhost:8080/hook?a=1", ""},
		{long, long, ""},
		{long + "x", long + "x", RuleURL},
		{"ftp://x", "ftp://x", RuleURL},
		{"/relative", "/relative", RuleURL},
		{"https://", "https://", RuleURL},
		{"http://exa mple.com", "http://exa mple.com", RuleURL},
		{"https://a.example/\x01", "https://a.example/\x01", RuleURL},
		{"javascript:alert(1)", "javascript:alert(1)", RuleURL},
		{"HTTPS://a.example", "HTTPS://a.example", ""},
	}
	for _, tc := range cases {
		got, rule := normalizeURL(tc.in)
		if got != tc.want || rule != tc.rule {
			t.Errorf("normalizeURL(%q) = %q, %q; want rule %q", tc.in, got, rule, tc.rule)
		}
	}
}

func TestNormalizeLabel(t *testing.T) {
	cases := []struct{ in, want, rule string }{
		{" Production ", "Production", ""},
		{strings.Repeat("é", 60), strings.Repeat("é", 60), ""},
		{strings.Repeat("é", 61), strings.Repeat("é", 61), RuleMax},
		{"", "", RuleRequired},
		{"a\tb", "a\tb", RuleName},
	}
	for _, tc := range cases {
		got, rule := normalizeLabel(tc.in)
		if got != tc.want || rule != tc.rule {
			t.Errorf("normalizeLabel(%q) = %q, %q; want %q, %q", tc.in, got, rule, tc.want, tc.rule)
		}
	}
}

func TestExpiresAtRule(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Second), now.Add(time.Nanosecond)
	for _, tc := range []struct {
		t    *time.Time
		want string
	}{{nil, ""}, {&future, ""}, {&now, RuleFuture}, {&past, RuleFuture}} {
		if got := expiresAtRule(tc.t, now); got != tc.want {
			t.Errorf("expiresAtRule(%v) = %q, want %q", tc.t, got, tc.want)
		}
	}
}

func TestParseMetadata(t *testing.T) {
	keys := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = fmt.Sprintf(`"k%03d":1`, i)
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	str := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	// sized is 20 short string values whose compact JSON is exactly n bytes:
	// braces and commas take 21 bytes, each "kNN":"" 8 more.
	sized := func(n int) string {
		obj := map[string]string{"k00": strings.Repeat("x", n-181-19*400)}
		for i := 1; i < 20; i++ {
			obj[fmt.Sprintf("k%02d", i)] = strings.Repeat("x", 400)
		}
		b, _ := json.Marshal(obj)
		if len(b) != n {
			t.Fatalf("sized(%d) is %d bytes", n, len(b))
		}
		return string(b)
	}

	cases := []struct {
		name string
		raw  string
		want []domain.FieldError
	}{
		{"omitted", ``, nil},
		{"null", `null`, nil},
		{"empty", `{}`, nil},
		{"array", `[]`, []domain.FieldError{fe("metadata", RuleObject)}},
		{"string", `"x"`, []domain.FieldError{fe("metadata", RuleObject)}},
		{"number", `1`, []domain.FieldError{fe("metadata", RuleObject)}},
		{"50 keys", keys(50), nil},
		{"51 keys", keys(51), []domain.FieldError{fe("metadata", RuleMaxKeys)}},
		{"40-rune key with emoji", `{"` + strings.Repeat("🌏", 40) + `":1}`, nil},
		{"41-rune key", `{"` + strings.Repeat("a", 41) + `":1}`, []domain.FieldError{fe("metadata", RuleKey)}},
		{"empty key", `{"":1}`, []domain.FieldError{fe("metadata", RuleKey)}},
		{"nul in key", `{"a\u0000":1}`, []domain.FieldError{fe("metadata", RuleKey)}},
		{"newline in key, reported once", `{"a\n":1,"b\n":2}`, []domain.FieldError{fe("metadata", RuleKey)}},
		{"500 é", `{"v":` + str(strings.Repeat("é", 500)) + `}`, nil},
		{"500 emoji", `{"v":` + str(strings.Repeat("🌏", 500)) + `}`, nil},
		{"500 CJK", `{"v":` + str(strings.Repeat("你", 500)) + `}`, nil},
		{"501 runes", `{"v":` + str(strings.Repeat("é", 501)) + `}`, []domain.FieldError{fe("metadata.v", RuleMax)}},
		{"nested object", `{"v":{"a":1}}`, []domain.FieldError{fe("metadata.v", RuleType)}},
		{"nested array", `{"v":[1]}`, []domain.FieldError{fe("metadata.v", RuleType)}},
		{"nul in value", `{"v":"a\u0000b"}`, []domain.FieldError{fe("metadata.v", RuleInvalidCharacter)}},
		{"value errors sorted by key", `{"z":[],"a":{},"m":"ok"}`, []domain.FieldError{fe("metadata.a", RuleType), fe("metadata.z", RuleType)}},
		{"8192 bytes", sized(8192), nil},
		{"8193 bytes", sized(8193), []domain.FieldError{fe("metadata", RuleMaxBytes)}},
		{"number 1e300", `{"n":1e300}`, nil},
		{"number 1E2", `{"n":1E2}`, nil},
		{"number numeric cannot hold", `{"n":1e200000}`, []domain.FieldError{fe("metadata", RuleMaxBytes)}},
		{"negative tiny number numeric cannot hold", `{"n":-1e-200000}`, []domain.FieldError{fe("metadata", RuleMaxBytes)}},
		{"exponent past numeric input", `{"n":0e1073741823}`, []domain.FieldError{fe("metadata", RuleMaxBytes)}},
		{"exponent past int64", `{"n":1e99999999999999999999}`, []domain.FieldError{fe("metadata", RuleMaxBytes)}},
		{"short exponent over the cap once stored", `{"n":1e131071}`, []domain.FieldError{fe("metadata", RuleMaxBytes)}},
		{"oversized numbers reported once", `{"a":1e9000,"b":1e9000}`, []domain.FieldError{fe("metadata", RuleMaxBytes)}},
		{"exponents that only fit as sent", `{"a":1e4100,"b":1e4100}`, []domain.FieldError{fe("metadata", RuleMaxBytes)}},
		{"value error and oversized number", `{"a":{},"b":1e9000}`, []domain.FieldError{fe("metadata.a", RuleType), fe("metadata", RuleMaxBytes)}},
	}
	for _, tc := range cases {
		m, errs := parseMetadata(json.RawMessage(tc.raw))
		if len(errs) != len(tc.want) {
			t.Errorf("%s: errors = %v, want %v", tc.name, errs, tc.want)
			continue
		}
		for i := range errs {
			if errs[i] != tc.want[i] {
				t.Errorf("%s: errors = %v, want %v", tc.name, errs, tc.want)
			}
		}
		if len(tc.want) == 0 && m == nil {
			t.Errorf("%s: nil metadata", tc.name)
		}
	}
}

func TestParseMetadataKeepsNumbers(t *testing.T) {
	m, errs := parseMetadata(json.RawMessage(`{"big":12345678901234567890,"f":0.1,"e":1e3,"s":"x","b":false,"n":null}`))
	if errs != nil {
		t.Fatal(errs)
	}
	b, _ := json.Marshal(m)
	if string(b) != `{"b":false,"big":12345678901234567890,"e":1000,"f":0.1,"n":null,"s":"x"}` {
		t.Fatalf("round trip = %s", b)
	}
}

// storedNumberCases are JSON numbers and the text Postgres JSONB returns for
// them; TestIntegrationStoredNumberMatchesPostgres checks them against a
// real server.
var storedNumberCases = []struct{ in, want string }{
	{"0", "0"},
	{"-0", "0"},
	{"-0.00", "0.00"},
	{"0e-5", "0.00000"},
	{"0e1000", "0"},
	{"1", "1"},
	{"-17", "-17"},
	{"12345678901234567890", "12345678901234567890"},
	{"0.1", "0.1"},
	{"1.50", "1.50"},
	{"-0.0001", "-0.0001"},
	{"1e3", "1000"},
	{"1E2", "100"},
	{"1e+2", "100"},
	{"1.50e1", "15.0"},
	{"1.5e0", "1.5"},
	{"123.456e-1", "12.3456"},
	{"5e-1", "0.5"},
	{"1e-3", "0.001"},
	{"-1.0e-3", "-0.0010"},
	{"2.5E-0", "2.5"},
	{"1e30", "1" + strings.Repeat("0", 30)},
}

func TestStoredNumber(t *testing.T) {
	for _, tc := range storedNumberCases {
		got, ok := storedNumber(json.Number(tc.in), maxMetadataBytes)
		if !ok || string(got) != tc.want {
			t.Errorf("storedNumber(%s) = %q, %v; want %q", tc.in, got, ok, tc.want)
		}
	}
}

func TestStoredNumberLimit(t *testing.T) {
	cases := []struct {
		in    string
		limit int
		ok    bool
	}{
		{"1e7", 8, true},
		{"1e8", 8, false},
		{"-1e6", 8, true},
		{"-1e7", 8, false},
		{"1e-6", 8, true},
		{"1e-7", 8, false},
		{"-0e-6", 8, true},
		{"0e99999999", 1, true},
		{"1e131071", maxMetadataBytes, false},
		{"1e-200000", maxMetadataBytes, false},
		{"1e1073741823", maxMetadataBytes, false},
		{"0e-1073741823", maxMetadataBytes, false},
		{"1e99999999999999999999", maxMetadataBytes, false},
	}
	for _, tc := range cases {
		got, ok := storedNumber(json.Number(tc.in), tc.limit)
		if ok != tc.ok {
			t.Errorf("storedNumber(%s, %d) ok = %v, want %v", tc.in, tc.limit, ok, tc.ok)
		}
		if ok && len(got) > tc.limit {
			t.Errorf("storedNumber(%s, %d) = %q, longer than the limit", tc.in, tc.limit, got)
		}
	}
}

func TestIdempotencyKeyRule(t *testing.T) {
	cases := []struct{ key, want string }{
		{"", ""},
		{"a", ""},
		{"3f1c2d9e-7b1a-4c55-9a1e-2f0f6f1b9c11", ""},
		{"!~", ""},
		{strings.Repeat("k", 255), ""},
		{strings.Repeat("k", 256), RuleMax},
		{"a b", RuleFormat},
		{"a\tb", RuleFormat},
		{"é", RuleFormat},
		{"a\x7f", RuleFormat},
		{"a\x00", RuleFormat},
	}
	for _, tc := range cases {
		if got := idempotencyKeyRule(tc.key); got != tc.want {
			t.Errorf("idempotencyKeyRule(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func TestPaymentFingerprint(t *testing.T) {
	settle := time.Date(2026, 1, 2, 10, 0, 0, 0, time.FixedZone("WIB", 7*3600))
	fp := func(in CreatePaymentInput) string {
		meta, errs := parseMetadata(in.Metadata)
		if errs != nil {
			t.Fatal(errs)
		}
		n, err := normalizePayment(in, meta)
		if err != nil {
			t.Fatal(err)
		}
		return paymentFingerprint(n)
	}
	base := CreatePaymentInput{OrderID: "ord-1", Amount: 1000, Items: []domain.OrderItem{{Name: "a", Quantity: 1, Price: 1000}},
		ExpectedSettlementAt: &settle, Metadata: json.RawMessage(`{"a":1,"b":"x"}`)}
	same := base
	same.OrderID = " ord-1 "
	same.Currency = "IDR"
	utc := settle.UTC()
	same.ExpectedSettlementAt = &utc
	same.Metadata = json.RawMessage(" {\n \"b\" : \"x\",\"a\":1 } ")

	if got := fp(base); len(got) != 64 || got != fp(same) || got != fp(base) {
		t.Fatalf("fingerprints differ for the same request: %s %s", fp(base), fp(same))
	}
	noItems, emptyItems := base, base
	noItems.Items, emptyItems.Items = nil, []domain.OrderItem{}
	if fp(noItems) != fp(emptyItems) {
		t.Fatal("omitted and empty items differ")
	}
	for name, change := range map[string]func(*CreatePaymentInput){
		"amount":   func(in *CreatePaymentInput) { in.Amount = 1001 },
		"items":    func(in *CreatePaymentInput) { in.Items = []domain.OrderItem{{Name: "a", Quantity: 2, Price: 500}} },
		"metadata": func(in *CreatePaymentInput) { in.Metadata = json.RawMessage(`{"a":2,"b":"x"}`) },
		"currency": func(in *CreatePaymentInput) { in.Currency = "USD" },
		"desc":     func(in *CreatePaymentInput) { in.Description = "x" },
	} {
		in := base
		change(&in)
		if fp(in) == fp(base) {
			t.Errorf("%s change kept the fingerprint", name)
		}
	}
}

func fe(field, rule string) domain.FieldError { return domain.FieldError{Field: field, Rule: rule} }
