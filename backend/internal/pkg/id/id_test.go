package id

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

var allPrefixes = []Prefix{App, Payment, Connection, WebhookEndpoint, Event, Delivery, Inbound, User, APIKey, Reconciliation, Audit}

func TestNewFormatForEveryPrefix(t *testing.T) {
	for _, p := range allPrefixes {
		re := regexp.MustCompile(`^` + string(p) + `_[0-9A-HJKMNP-TV-Z]{26}$`)
		if got := New(p); !re.MatchString(got) {
			t.Errorf("New(%s) = %q, does not match %s", p, got, re)
		}
	}
}

func TestNewIsUnique(t *testing.T) {
	seen := make(map[string]bool, 10000)
	for i := 0; i < 10000; i++ {
		v := New(Payment)
		if seen[v] {
			t.Fatalf("duplicate id %s after %d ids", v, i)
		}
		seen[v] = true
	}
}

func TestNewSortsByCreationTime(t *testing.T) {
	first := New(Audit)
	time.Sleep(2 * time.Millisecond)
	second := New(Audit)
	if first >= second {
		t.Fatalf("ids not time-ordered: %s then %s", first, second)
	}
}

func TestParseAcceptsCanonicalAndLowerCase(t *testing.T) {
	canonical := New(Payment)
	for _, in := range []string{canonical, "pay_" + strings.ToLower(strings.TrimPrefix(canonical, "pay_"))} {
		p, u, err := Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", in, err)
		}
		if p != Payment || "pay_"+u.String() != canonical {
			t.Fatalf("Parse(%q) = %s %s, want %s", in, p, u, canonical)
		}
		got, err := ParseWithPrefix(in, Payment)
		if err != nil || got != canonical {
			t.Fatalf("ParseWithPrefix(%q) = %q, %v; want %q", in, got, err, canonical)
		}
		if !Valid(in, Payment) {
			t.Fatalf("Valid(%q) = false", in)
		}
	}
}

func TestParseRejectsMalformed(t *testing.T) {
	ulidPart := strings.TrimPrefix(New(Payment), "pay_")
	cases := map[string]string{
		"empty":              "",
		"prefix only":        "pay_",
		"no separator":       "pay" + ulidPart,
		"unknown prefix":     "xxx_" + ulidPart,
		"upper-case prefix":  "PAY_" + ulidPart,
		"25 chars":           "pay_" + ulidPart[:25],
		"27 chars":           "pay_" + ulidPart + "0",
		"letter I":           "pay_" + ulidPart[:25] + "I",
		"letter L":           "pay_" + ulidPart[:25] + "L",
		"letter O":           "pay_" + ulidPart[:25] + "O",
		"letter U":           "pay_" + ulidPart[:25] + "U",
		"unicode":            "pay_" + ulidPart[:24] + "é",
		"unicode upper to S": "pay_" + ulidPart[:24] + "ſ",
		"leading space":      " pay_" + ulidPart,
		"trailing space":     "pay_" + ulidPart + " ",
		"overflow":           "pay_8" + ulidPart[1:],
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Parse(in); !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("Parse(%q) error = %v, want ErrInvalidInput", in, err)
			}
			if Valid(in, Payment) {
				t.Fatalf("Valid(%q) = true", in)
			}
		})
	}
}

func TestParseWithPrefixRejectsWrongPrefix(t *testing.T) {
	appID := New(App)
	if _, err := ParseWithPrefix(appID, Payment); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("ParseWithPrefix(app id, pay) error = %v, want ErrInvalidInput", err)
	}
	if Valid(appID, Payment) {
		t.Fatal("Valid(app id, pay) = true")
	}
}
