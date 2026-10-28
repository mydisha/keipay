package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// Field rules for apps, keys, metadata and idempotency keys, reported in
// details.fields.
const (
	RuleSlug             = "slug"
	RuleURL              = "url"
	RuleImmutable        = "immutable"
	RuleFuture           = "future"
	RuleFormat           = "format"
	RuleObject           = "object"
	RuleMaxKeys          = "max_keys"
	RuleKey              = "key"
	RuleType             = "type"
	RuleInvalidCharacter = "invalid_character"
	RuleMaxBytes         = "max_bytes"
)

// Input limits for apps, keys, metadata and idempotency keys.
const (
	maxLabelRunes          = 60
	maxURLBytes            = 2048
	maxMetadataKeys        = 50
	maxMetadataKeyRunes    = 40
	maxMetadataValueRunes  = 500
	maxMetadataBytes       = 8192
	maxIdempotencyKeyBytes = 255
)

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$`)

// normalizeSlug trims and lower-cases a slug and returns it with the failed
// rule, or "".
func normalizeSlug(s string) (string, string) {
	slug := strings.ToLower(strings.TrimSpace(s))
	if slug == "" {
		return "", RuleRequired
	}
	if !slugRe.MatchString(slug) {
		return slug, RuleSlug
	}
	return slug, ""
}

func environmentRule(e domain.Environment) string {
	if e == "" {
		return RuleRequired
	}
	if !e.Valid() {
		return RuleOneOf
	}
	return ""
}

// normalizeURL trims an optional http(s) URL and returns it with the failed
// rule, or "". Empty means none.
func normalizeURL(s string) (string, string) {
	u := strings.TrimSpace(s)
	if u == "" {
		return "", ""
	}
	if len(u) > maxURLBytes {
		return u, RuleURL
	}
	for _, r := range u {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return u, RuleURL
		}
	}
	parsed, err := url.Parse(u)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return u, RuleURL
	}
	return u, ""
}

// normalizeLabel trims an API key label and returns it with the failed rule,
// or "".
func normalizeLabel(s string) (string, string) {
	label := strings.TrimSpace(s)
	if label == "" {
		return "", RuleRequired
	}
	if utf8.RuneCountInString(label) > maxLabelRunes {
		return label, RuleMax
	}
	for _, r := range label {
		if unicode.IsControl(r) {
			return label, RuleName
		}
	}
	return label, ""
}

// expiresAtRule checks an optional key expiry against now.
func expiresAtRule(t *time.Time, now time.Time) string {
	if t != nil && !t.After(now) {
		return RuleFuture
	}
	return ""
}

// idempotencyKeyRule checks an Idempotency-Key header; "" is no key.
func idempotencyKeyRule(key string) string {
	if len(key) > maxIdempotencyKeyBytes {
		return RuleMax
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x21 || key[i] > 0x7e {
			return RuleFormat
		}
	}
	return ""
}

// parseMetadata validates raw metadata JSON (omitted or null means {}) and
// returns it with numbers as json.Number in the form Postgres stores (see
// storedNumber). Errors are field errors on
// "metadata", value errors on "metadata.<key>" sorted by key.
func parseMetadata(raw json.RawMessage) (domain.Metadata, []domain.FieldError) {
	invalid := func(rule string) []domain.FieldError {
		return []domain.FieldError{{Field: "metadata", Rule: rule}}
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return domain.Metadata{}, nil
	}
	if trimmed[0] != '{' {
		return nil, invalid(RuleObject)
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, invalid(RuleObject)
	}

	var errs []domain.FieldError
	if len(m) > maxMetadataKeys {
		errs = append(errs, domain.FieldError{Field: "metadata", Rule: RuleMaxKeys})
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if !metadataKeyValid(k) {
			errs = append(errs, domain.FieldError{Field: "metadata", Rule: RuleKey})
			break
		}
	}
	oversized := false
	for _, k := range keys {
		if rule := metadataValueRule(m[k]); rule != "" {
			errs = append(errs, domain.FieldError{Field: "metadata." + k, Rule: rule})
			continue
		}
		if n, isNumber := m[k].(json.Number); isNumber {
			stored, ok := storedNumber(n, maxMetadataBytes)
			if !ok {
				oversized = true
				continue
			}
			m[k] = stored
		}
	}
	if oversized || compactJSONSize(m) > maxMetadataBytes {
		errs = append(errs, domain.FieldError{Field: "metadata", Rule: RuleMaxBytes})
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return domain.Metadata(m), nil
}

func metadataKeyValid(k string) bool {
	if k == "" || utf8.RuneCountInString(k) > maxMetadataKeyRunes {
		return false
	}
	for _, r := range k {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// metadataValueRule returns the failed rule for one value, or "". Postgres
// JSONB cannot store U+0000, so it is refused rather than lost.
func metadataValueRule(v any) string {
	switch t := v.(type) {
	case nil, bool, json.Number:
		return ""
	case string:
		if utf8.RuneCountInString(t) > maxMetadataValueRunes {
			return RuleMax
		}
		if strings.ContainsRune(t, 0) {
			return RuleInvalidCharacter
		}
		return ""
	default:
		return RuleType
	}
}

// maxNumberExponent mirrors Postgres numeric input, which refuses an exponent
// of INT_MAX/2 or more in magnitude whatever the mantissa.
const maxNumberExponent = 1<<30 - 1

// storedNumber rewrites a valid JSON number in the form Postgres numeric, and
// so JSONB, stores and returns it: plain decimal with the exponent applied,
// scale max(0, fraction digits - exponent), leading zeros dropped and no
// negative zero. Converting at the boundary keeps the create response, later
// reads and webhooks identical, and lets the byte cap measure what is stored
// rather than a short exponent form that expands. ok is false when that form
// is longer than limit bytes, which covers every number numeric cannot hold;
// the length is computed before the digits are built, so a huge exponent
// never allocates.
func storedNumber(n json.Number, limit int) (json.Number, bool) {
	s := string(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	mantissa, exp := s, int64(0)
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		e, err := strconv.ParseInt(s[i+1:], 10, 64)
		if err != nil || e >= maxNumberExponent || e <= -maxNumberExponent {
			return "", false
		}
		mantissa, exp = s[:i], e
	}
	intPart, frac, _ := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(intPart+frac, "0")
	scale := max(0, int64(len(frac))-exp)
	// The stored value is digits followed by pad zeros, divided by 10^scale.
	pad := scale - int64(len(frac)) + exp
	width := int64(0)
	if digits != "" {
		width = int64(len(digits)) + pad
	}
	size := max(1, width-scale)
	if scale > 0 {
		size += 1 + scale
	}
	if neg && digits != "" {
		size++
	}
	if size > int64(limit) {
		return "", false
	}

	unscaled := ""
	if digits != "" {
		unscaled = digits + strings.Repeat("0", int(pad))
	}
	if short := int(scale) + 1 - len(unscaled); short > 0 {
		unscaled = strings.Repeat("0", short) + unscaled
	}
	cut := len(unscaled) - int(scale)
	out := unscaled[:cut]
	if scale > 0 {
		out += "." + unscaled[cut:]
	}
	if neg && digits != "" {
		out = "-" + out
	}
	return json.Number(out), true
}

// compactJSONSize is the size of m as compact JSON with UTF-8 left
// unescaped, the form clients measure.
func compactJSONSize(m map[string]any) int {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return maxMetadataBytes + 1
	}
	return len(bytes.TrimRight(buf.Bytes(), "\n"))
}

// paymentFingerprint identifies a create-payment request by its normalized
// input, so a retry with the same Idempotency-Key can be told apart from a
// different request. JSON key order and whitespace of the request do not
// matter: the fields are fixed and metadata keys are sorted on encoding.
func paymentFingerprint(in normalizedPayment) string {
	b, _ := json.Marshal(in)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// normalizedPayment is the create-payment input after trimming and
// defaulting; its JSON encoding is what paymentFingerprint hashes.
type normalizedPayment struct {
	OrderID              string             `json:"order_id"`
	Amount               int64              `json:"amount"`
	Currency             string             `json:"currency"`
	Description          string             `json:"description"`
	PayerEmail           string             `json:"payer_email"`
	InvoiceDuration      int                `json:"invoice_duration"`
	Items                []domain.OrderItem `json:"items"`
	Customer             *domain.Customer   `json:"customer"`
	ExpectedSettlementAt string             `json:"expected_settlement_at"`
	Metadata             domain.Metadata    `json:"metadata"`
}
