package service

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/text/unicode/norm"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// Field validation rules reported in details.fields.
const (
	RuleRequired = "required"
	RuleEmail    = "email"
	RuleName     = "name"
	RuleMin      = "min"
	RuleMax      = "max"
	RuleOneOf    = "oneof"
)

// Input limits for console users.
const (
	maxEmailBytes       = 254
	maxEmailLocalBytes  = 64
	maxEmailDomainBytes = 253
	minEmailBytes       = 3
	maxNameRunes        = 100
	minPasswordRunes    = 12
	// bcrypt ignores everything after 72 bytes, so longer passwords are refused
	// rather than silently truncated.
	maxPasswordBytes = 72
)

// CanonicalEmail is the stored and compared form of an email: trimmed, Unicode
// NFC and lower case.
func CanonicalEmail(s string) string {
	return strings.ToLower(norm.NFC.String(strings.TrimSpace(s)))
}

// emailRule validates a canonical email and returns the failed rule, or "".
func emailRule(email string) string {
	if email == "" {
		return RuleRequired
	}
	if len(email) < minEmailBytes || len(email) > maxEmailBytes || !utf8.ValidString(email) {
		return RuleEmail
	}
	local, domainPart, ok := strings.Cut(email, "@")
	if !ok || strings.Contains(domainPart, "@") {
		return RuleEmail
	}
	if len(local) < 1 || len(local) > maxEmailLocalBytes || len(domainPart) < 1 || len(domainPart) > maxEmailDomainBytes {
		return RuleEmail
	}
	if !strings.Contains(domainPart, ".") || strings.HasPrefix(domainPart, ".") ||
		strings.HasSuffix(domainPart, ".") || strings.Contains(domainPart, "..") {
		return RuleEmail
	}
	for _, r := range email {
		if unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune(`<>()[]\,;:"`, r) {
			return RuleEmail
		}
	}
	return ""
}

// normalizeName trims a display name and returns it with the failed rule, or "".
func normalizeName(s string) (string, string) {
	name := strings.TrimSpace(s)
	if name == "" {
		return "", RuleRequired
	}
	if utf8.RuneCountInString(name) > maxNameRunes {
		return name, RuleMax
	}
	if !utf8.ValidString(name) {
		return name, RuleName
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return name, RuleName
		}
	}
	return name, ""
}

// passwordRule checks a new password exactly as sent (no trimming or
// normalization) and returns the failed rule, or "".
func passwordRule(p string) string {
	if utf8.RuneCountInString(p) < minPasswordRunes {
		return RuleMin
	}
	if len(p) > maxPasswordBytes {
		return RuleMax
	}
	return ""
}

func roleRule(r domain.Role) string {
	if r == "" {
		return RuleRequired
	}
	if !r.Valid() {
		return RuleOneOf
	}
	return ""
}

// fieldErrors collects failed rules in field order.
type fieldErrors []domain.FieldError

func (f *fieldErrors) check(field, rule string) {
	if rule != "" {
		*f = append(*f, domain.FieldError{Field: field, Rule: rule})
	}
}

// sorted returns the errors ordered by field, following order; fields not in
// order (such as metadata.<key>) keep their place after the field they
// belong to.
func (f fieldErrors) sorted(order []string) fieldErrors {
	rank := func(field string) int {
		base, _, _ := strings.Cut(field, ".")
		for i, o := range order {
			if o == base {
				return i
			}
		}
		return len(order)
	}
	out := slices.Clone(f)
	slices.SortStableFunc(out, func(a, b domain.FieldError) int { return rank(a.Field) - rank(b.Field) })
	return out
}

func (f fieldErrors) err() error {
	if len(f) == 0 {
		return nil
	}
	return domain.InvalidFields(f...)
}

// PasswordHasher hashes and verifies passwords.
type PasswordHasher interface {
	Hash(password string) (string, error)
	// Matches reports whether password matches hash; a malformed hash never matches.
	Matches(hash, password string) bool
}

// DefaultBcryptCost is the production bcrypt cost.
const DefaultBcryptCost = 12

// BcryptHasher is a PasswordHasher using bcrypt at a fixed cost.
type BcryptHasher struct{ cost int }

// NewBcryptHasher returns a bcrypt hasher. Tests pass bcrypt.MinCost.
func NewBcryptHasher(cost int) *BcryptHasher { return &BcryptHasher{cost: cost} }

// Hash returns the bcrypt hash of password.
func (h *BcryptHasher) Hash(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), h.cost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(b), nil
}

// Matches reports whether password matches the bcrypt hash.
func (h *BcryptHasher) Matches(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// temporaryAlphabet leaves out look-alikes (0 O 1 l I).
const temporaryAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"

const temporaryPasswordLen = 20

// TemporaryPassword returns a random password for a new or reset user.
func TemporaryPassword() (string, error) {
	n := len(temporaryAlphabet)
	// Bytes at or above limit are rejected so every character is equally likely.
	limit := 256 - 256%n
	out := make([]byte, 0, temporaryPasswordLen)
	buf := make([]byte, 32)
	for len(out) < temporaryPasswordLen {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("generate temporary password: %w", err)
		}
		for _, b := range buf {
			if int(b) >= limit {
				continue
			}
			out = append(out, temporaryAlphabet[int(b)%n])
			if len(out) == temporaryPasswordLen {
				break
			}
		}
	}
	return string(out), nil
}

// NewToken returns 32 random bytes as unpadded base64url, used for session
// and CSRF tokens.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// SecureEqual compares two secrets in constant time, including their length.
func SecureEqual(a, b string) bool {
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}

// sha256Hex is the key form for values that must not appear in Redis as-is.
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// truncateBytes cuts s to at most n bytes on a rune boundary.
func truncateBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
