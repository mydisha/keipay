package domain

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// EventType names a kind of event KeiPay sends to apps.
type EventType string

// Event types, in catalog order.
const (
	EventPaymentPaid      EventType = "payment.paid"
	EventPaymentSettled   EventType = "payment.settled"
	EventPaymentExpired   EventType = "payment.expired"
	EventPaymentFailed    EventType = "payment.failed"
	EventPaymentDuplicate EventType = "payment.duplicate"
)

// EventAPIVersion is the version of the event envelope.
const EventAPIVersion = "v2"

// SubscribeAll is the subscription entry that matches every event type,
// including types added later.
const SubscribeAll = "*"

// MaxEventSubscriptions caps the subscription entries of one endpoint.
const MaxEventSubscriptions = 20

// EventTypeInfo is one catalog entry: a type, what it means, and an example
// v2 envelope.
type EventTypeInfo struct {
	Type        EventType
	Description string
	Example     EventEnvelope
}

// EventWildcard is a subscription entry that matches several types.
type EventWildcard struct {
	Pattern     string
	Description string
}

type catalogEntry struct {
	eventType   EventType
	status      PaymentStatus
	description string
}

var catalog = []catalogEntry{
	{EventPaymentPaid, PaymentStatusPaid, "The payer paid. The funds are in your gateway account but may not have settled yet."},
	{EventPaymentSettled, PaymentStatusSettled, "The gateway settled the funds of a paid payment."},
	{EventPaymentExpired, PaymentStatusExpired, "The checkout expired before the payer paid."},
	{EventPaymentFailed, PaymentStatusFailed, "The payment failed and will not be paid."},
	{EventPaymentDuplicate, PaymentStatusPaid, "A payment was paid more than once, for example through a second checkout. Reserved: not sent yet."},
}

// Example values shared by every catalog entry.
const (
	exampleEventID      = "evt_01JBKQ7Z8M4X2Y6V3N9T5R1W0C"
	examplePaymentID    = "pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C"
	exampleConnectionID = "con_01JBKQ7Z8M4X2Y6V3N9T5R1W0C"
)

var (
	exampleCreatedAt = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	examplePaidAt    = time.Date(2026, 1, 1, 9, 59, 30, 0, time.UTC)
	exampleSettledAt = time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)
)

// EventCatalog returns every event type KeiPay can send, in display order.
func EventCatalog() []EventTypeInfo {
	out := make([]EventTypeInfo, 0, len(catalog))
	for _, c := range catalog {
		data, _ := json.Marshal(ExampleEventData(c.eventType))
		out = append(out, EventTypeInfo{
			Type:        c.eventType,
			Description: c.description,
			Example: EventEnvelope{
				ID: exampleEventID, Type: c.eventType, APIVersion: EventAPIVersion,
				CreatedAt: exampleCreatedAt, Data: data,
			},
		})
	}
	return out
}

// EventWildcards returns the accepted wildcard subscriptions, in display order.
func EventWildcards() []EventWildcard {
	return []EventWildcard{
		{Pattern: "payment.*", Description: "Every payment event."},
		{Pattern: SubscribeAll, Description: "Every event, including types added later."},
	}
}

// ExampleEventData is the data of the catalog example of t, also sent by test
// events. The zero value is returned for a type outside the catalog.
func ExampleEventData(t EventType) PaymentEventData {
	for _, c := range catalog {
		if c.eventType != t {
			continue
		}
		provider, conn := "xendit", exampleConnectionID
		d := PaymentEventData{
			ID: examplePaymentID, OrderID: "order_1001", Reference: "kp_" + examplePaymentID,
			Status: c.status, Amount: 150000, Currency: "IDR", Metadata: Metadata{},
			Provider: &provider, ConnectionID: &conn,
		}
		if c.status.IsSuccessful() {
			paid := examplePaidAt
			d.PaidAt = &paid
		}
		if c.status == PaymentStatusSettled {
			settled := exampleSettledAt
			d.SettledAt = &settled
		}
		return d
	}
	return PaymentEventData{}
}

// Known reports whether t is in the catalog.
func (t EventType) Known() bool {
	for _, c := range catalog {
		if c.eventType == t {
			return true
		}
	}
	return false
}

// EventTypeForStatus is the event a payment moving to s emits. PENDING emits
// none.
func EventTypeForStatus(s PaymentStatus) (EventType, bool) {
	switch s {
	case PaymentStatusPaid, PaymentStatusSettled, PaymentStatusExpired, PaymentStatusFailed:
		return EventType("payment." + strings.ToLower(string(s))), true
	default:
		return "", false
	}
}

// MatchSubscription reports whether an endpoint subscribed to subs receives
// events of type t: "*" matches every type, "<group>.*" every type of the
// group, anything else only itself.
func MatchSubscription(subs []string, t EventType) bool {
	for _, s := range subs {
		switch {
		case s == SubscribeAll:
			return true
		case strings.HasSuffix(s, ".*"):
			if strings.HasPrefix(string(t), strings.TrimSuffix(s, "*")) {
				return true
			}
		case s == string(t):
			return true
		}
	}
	return false
}

// validSubscription reports whether s is a catalog type, "*", or a group
// wildcard whose group has at least one catalog type.
func validSubscription(s string) bool {
	if s == SubscribeAll || EventType(s).Known() {
		return true
	}
	group, ok := strings.CutSuffix(s, ".*")
	if !ok || group == "" {
		return false
	}
	for _, c := range catalog {
		if strings.HasPrefix(string(c.eventType), group+".") {
			return true
		}
	}
	return false
}

// ValidateSubscriptions trims every entry and checks the list, keeping its
// order. Errors use the field names of the events request field.
func ValidateSubscriptions(in []string) ([]string, []FieldError) {
	if len(in) == 0 {
		return nil, []FieldError{{Field: "events", Rule: "required"}}
	}
	if len(in) > MaxEventSubscriptions {
		return nil, []FieldError{{Field: "events", Rule: "max"}}
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	var errs []FieldError
	for i, raw := range in {
		s := strings.TrimSpace(raw)
		field := "events." + strconv.Itoa(i)
		switch {
		case !validSubscription(s):
			errs = append(errs, FieldError{Field: field, Rule: "oneof"})
		case seen[s]:
			errs = append(errs, FieldError{Field: field, Rule: "duplicate"})
		}
		seen[s] = true
		out = append(out, s)
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return out, nil
}

// EventEnvelope is the v2 webhook body. Data is the type's payload as JSON.
type EventEnvelope struct {
	ID         string          `json:"id"`
	Type       EventType       `json:"type"`
	APIVersion string          `json:"api_version"`
	CreatedAt  time.Time       `json:"created_at"`
	Data       json.RawMessage `json:"data"`
}

// PaymentEventData is the data of a payment event: the payment as of the
// event.
type PaymentEventData struct {
	ID           string        `json:"id"`
	OrderID      string        `json:"order_id"`
	Reference    string        `json:"reference"`
	Status       PaymentStatus `json:"status"`
	Amount       int64         `json:"amount"`
	Currency     string        `json:"currency"`
	Metadata     Metadata      `json:"metadata"`
	Provider     *string       `json:"provider"`
	ConnectionID *string       `json:"connection_id"`
	PaidAt       *time.Time    `json:"paid_at"`
	SettledAt    *time.Time    `json:"settled_at"`
}

// Event is one thing that happened to an app's payment, or a test send. It
// is fanned out as one delivery per matching endpoint.
type Event struct {
	ID         int64
	PublicID   string
	Type       EventType
	APIVersion string
	AppID      int64
	// PaymentID is nil for test events.
	PaymentID *int64
	Data      json.RawMessage
	IsTest    bool
	CreatedAt time.Time
}

// Envelope returns the v2 body of e.
func (e *Event) Envelope() EventEnvelope {
	return EventEnvelope{ID: e.PublicID, Type: e.Type, APIVersion: EventAPIVersion, CreatedAt: e.CreatedAt.UTC(), Data: e.Data}
}

// EventRef identifies the event a delivery or inbound webhook refers to.
type EventRef struct {
	PublicID string
	Type     EventType
	IsTest   bool
}
