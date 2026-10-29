// Package id generates and parses prefixed public identifiers such as
// "pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C": a type prefix, an underscore and a ULID in
// upper-case Crockford base32. Public ids sort by creation time.
package id

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// Prefix names the entity type an id belongs to.
type Prefix string

// Known prefixes. Parse rejects any other prefix.
const (
	App                Prefix = "app"
	Payment            Prefix = "pay"
	Connection         Prefix = "con"
	WebhookEndpoint    Prefix = "whe"
	Event              Prefix = "evt"
	Delivery           Prefix = "dlv"
	Inbound            Prefix = "inb"
	User               Prefix = "usr"
	APIKey             Prefix = "key"
	Reconciliation     Prefix = "rec"
	ReconciliationItem Prefix = "rci"
	Audit              Prefix = "aud"
)

var known = map[Prefix]bool{
	App: true, Payment: true, Connection: true, WebhookEndpoint: true, Event: true,
	Delivery: true, Inbound: true, User: true, APIKey: true, Reconciliation: true, ReconciliationItem: true, Audit: true,
}

const ulidLen = 26

// New returns a fresh id for prefix, e.g. New(id.Payment) = "pay_01J...".
func New(prefix Prefix) string {
	// crypto/rand.Reader never fails on supported platforms, so MustNew cannot panic.
	return string(prefix) + "_" + ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
}

// Parse splits s into its prefix and ULID. The ULID part may be lower case; the
// prefix must be one of the known lower-case prefixes. Errors wrap
// domain.ErrInvalidInput.
func Parse(s string) (Prefix, ulid.ULID, error) {
	head, tail, ok := strings.Cut(s, "_")
	if !ok {
		return "", ulid.ULID{}, invalid("missing prefix separator")
	}
	p := Prefix(head)
	if !known[p] {
		return "", ulid.ULID{}, invalid("unknown prefix")
	}
	if len(tail) != ulidLen {
		return "", ulid.ULID{}, invalid("wrong length")
	}
	// Upper-casing non-ASCII can yield ASCII (e.g. U+017F becomes "S"), so only
	// ASCII input is normalised.
	for i := 0; i < len(tail); i++ {
		if tail[i] >= 0x80 {
			return "", ulid.ULID{}, invalid("invalid character")
		}
	}
	u, err := ulid.ParseStrict(strings.ToUpper(tail))
	if err != nil {
		return "", ulid.ULID{}, invalid("invalid character")
	}
	return p, u, nil
}

// ParseWithPrefix parses s, requires its prefix to be want, and returns the
// canonical (upper-case ULID) form.
func ParseWithPrefix(s string, want Prefix) (string, error) {
	p, u, err := Parse(s)
	if err != nil {
		return "", err
	}
	if p != want {
		return "", invalid(fmt.Sprintf("expected %s id", want))
	}
	return string(p) + "_" + u.String(), nil
}

// Valid reports whether s is a well-formed id with the given prefix.
func Valid(s string, prefix Prefix) bool {
	_, err := ParseWithPrefix(s, prefix)
	return err == nil
}

func invalid(reason string) error {
	return fmt.Errorf("%w: malformed id: %s", domain.ErrInvalidInput, reason)
}
