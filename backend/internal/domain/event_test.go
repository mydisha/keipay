package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// T-B1 / AC-1: the catalog's exact content and order.
func TestEventCatalog(t *testing.T) {
	want := []struct {
		t           EventType
		description string
		status      string
		paidAt      any
		settledAt   any
	}{
		{"payment.paid", "The payer paid. The funds are in your gateway account but may not have settled yet.", "PAID", "2026-01-01T09:59:30Z", nil},
		{"payment.settled", "The gateway settled the funds of a paid payment.", "SETTLED", "2026-01-01T09:59:30Z", "2026-01-02T03:00:00Z"},
		{"payment.expired", "The checkout expired before the payer paid.", "EXPIRED", nil, nil},
		{"payment.failed", "The payment failed and will not be paid.", "FAILED", nil, nil},
		{"payment.duplicate", "A payment was paid more than once, for example through a second checkout. Reserved: not sent yet.", "PAID", "2026-01-01T09:59:30Z", nil},
	}
	got := EventCatalog()
	if len(got) != len(want) {
		t.Fatalf("catalog has %d entries", len(got))
	}
	for i, w := range want {
		e := got[i]
		if e.Type != w.t || e.Description != w.description || !e.Type.Known() {
			t.Fatalf("entry %d = %s %q", i, e.Type, e.Description)
		}
		raw, err := json.Marshal(e.Example)
		if err != nil {
			t.Fatal(err)
		}
		var example map[string]any
		if err := json.Unmarshal(raw, &example); err != nil {
			t.Fatal(err)
		}
		wantExample := map[string]any{
			"id": "evt_01JBKQ7Z8M4X2Y6V3N9T5R1W0C", "type": string(w.t), "api_version": "v2", "created_at": "2026-01-01T10:00:00Z",
			"data": map[string]any{
				"id": "pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C", "order_id": "order_1001", "reference": "kp_pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C",
				"status": w.status, "amount": float64(150000), "currency": "IDR", "metadata": map[string]any{}, "provider": "xendit",
				"connection_id": "con_01JBKQ7Z8M4X2Y6V3N9T5R1W0C", "paid_at": w.paidAt, "settled_at": w.settledAt,
			},
		}
		if !reflect.DeepEqual(example, wantExample) {
			t.Fatalf("%s example = %s", w.t, raw)
		}
	}
	wildcards := EventWildcards()
	if !reflect.DeepEqual(wildcards, []EventWildcard{{"payment.*", "Every payment event."}, {"*", "Every event, including types added later."}}) {
		t.Fatalf("wildcards = %+v", wildcards)
	}
	if EventType("payment.refunded").Known() || EventType("payment.*").Known() || ExampleEventData("x").ID != "" {
		t.Fatal("unknown types are known")
	}
}

// T-B1: four statuses emit an event; PENDING does not.
func TestEventTypeForStatus(t *testing.T) {
	for s, want := range map[PaymentStatus]EventType{
		PaymentStatusPaid: EventPaymentPaid, PaymentStatusSettled: EventPaymentSettled,
		PaymentStatusExpired: EventPaymentExpired, PaymentStatusFailed: EventPaymentFailed,
	} {
		if got, ok := EventTypeForStatus(s); !ok || got != want {
			t.Errorf("%s = %s %v", s, got, ok)
		}
	}
	for _, s := range []PaymentStatus{PaymentStatusPending, "", "REFUNDED"} {
		if got, ok := EventTypeForStatus(s); ok || got != "" {
			t.Errorf("%q = %s %v", s, got, ok)
		}
	}
}

// T-B2 / AC-4
func TestMatchSubscription(t *testing.T) {
	cases := []struct {
		subs []string
		t    EventType
		want bool
	}{
		{[]string{"payment.paid"}, EventPaymentPaid, true},
		{[]string{"payment.paid"}, EventPaymentSettled, false},
		{[]string{"payment.*"}, EventPaymentSettled, true},
		{[]string{"*"}, EventPaymentDuplicate, true},
		{[]string{"*"}, "refund.created", true},
		{[]string{"payment.*"}, "refund.created", false},
		{[]string{"refund.*"}, EventPaymentPaid, false},
		{[]string{"payment"}, EventPaymentPaid, false},
		{[]string{"payment.settled", "payment.paid"}, EventPaymentPaid, true},
		{nil, EventPaymentPaid, false},
		{[]string{"pay*"}, EventPaymentPaid, false},
	}
	for _, tc := range cases {
		if got := MatchSubscription(tc.subs, tc.t); got != tc.want {
			t.Errorf("MatchSubscription(%v, %s) = %v", tc.subs, tc.t, got)
		}
	}
}

// T-B3 / AC-3
func TestValidateSubscriptions(t *testing.T) {
	got, errs := ValidateSubscriptions([]string{" payment.paid ", "payment.*", "*"})
	if errs != nil || !reflect.DeepEqual(got, []string{"payment.paid", "payment.*", "*"}) {
		t.Fatalf("valid = %v %v", got, errs)
	}
	cases := []struct {
		name string
		in   []string
		want []FieldError
	}{
		{"nil", nil, []FieldError{{"events", "required"}}},
		{"empty", []string{}, []FieldError{{"events", "required"}}},
		{"21 entries", make([]string, 21), []FieldError{{"events", "max"}}},
		{"unknown type", []string{"payment.refunded"}, []FieldError{{"events.0", "oneof"}}},
		{"unknown group", []string{"*", "foo.*"}, []FieldError{{"events.1", "oneof"}}},
		{"bare wildcard group", []string{".*"}, []FieldError{{"events.0", "oneof"}}},
		{"duplicate after trim", []string{"payment.paid", " payment.paid\t", "*", "*"}, []FieldError{{"events.1", "duplicate"}, {"events.3", "duplicate"}}},
		{"unicode garbage", []string{"pаyment.paid", "💳", "payment.paid\u0000", strings.Repeat("x", 200)},
			[]FieldError{{"events.0", "oneof"}, {"events.1", "oneof"}, {"events.2", "oneof"}, {"events.3", "oneof"}}},
		{"blank entry", []string{"  "}, []FieldError{{"events.0", "oneof"}}},
	}
	for _, tc := range cases {
		got, errs := ValidateSubscriptions(tc.in)
		if got != nil || !reflect.DeepEqual(errs, tc.want) {
			t.Errorf("%s: %v %v, want %v", tc.name, got, errs, tc.want)
		}
	}
	twenty := make([]string, 0, 20)
	for range 4 {
		for _, c := range EventCatalog() {
			twenty = append(twenty, " "+string(c.Type))
		}
	}
	if _, errs := ValidateSubscriptions(twenty); len(errs) != 15 || errs[0].Rule != "duplicate" {
		t.Fatalf("20 entries are not over the limit: %v", errs)
	}
}

// T-B20: exponential doubles from the interval and stops at 6 hours; fixed
// stays at the interval.
func TestRetryPolicyDelay(t *testing.T) {
	exp := RetryPolicy{MaxAttempts: 20, Backoff: BackoffExponential, IntervalSeconds: 30}
	for n, want := range map[int]time.Duration{1: 30 * time.Second, 2: time.Minute, 3: 2 * time.Minute, 8: 64 * time.Minute,
		10: 256 * time.Minute, 11: 6 * time.Hour, 19: 6 * time.Hour} {
		if got := exp.Delay(n); got != want {
			t.Errorf("exponential Delay(%d) = %v, want %v", n, got, want)
		}
	}
	big := RetryPolicy{Backoff: BackoffExponential, IntervalSeconds: 3600}
	if got := big.Delay(20); got != MaxRetryDelay {
		t.Errorf("Delay(20) at 1h = %v", got)
	}
	fixed := RetryPolicy{Backoff: BackoffFixed, IntervalSeconds: 45}
	for _, n := range []int{1, 5, 20} {
		if got := fixed.Delay(n); got != 45*time.Second {
			t.Errorf("fixed Delay(%d) = %v", n, got)
		}
	}
	if DefaultRetryPolicy() != (RetryPolicy{MaxAttempts: 8, Backoff: BackoffExponential, IntervalSeconds: 30}) {
		t.Fatal("default policy changed")
	}
}

func TestEventEnvelope(t *testing.T) {
	pay := int64(7)
	e := Event{PublicID: "evt_X", Type: EventPaymentPaid, PaymentID: &pay, Data: json.RawMessage(`{"a":1}`),
		CreatedAt: time.Date(2026, 1, 1, 17, 0, 0, 0, time.FixedZone("WIB", 7*3600))}
	raw, _ := json.Marshal(e.Envelope())
	if string(raw) != `{"id":"evt_X","type":"payment.paid","api_version":"v2","created_at":"2026-01-01T10:00:00Z","data":{"a":1}}` {
		t.Fatalf("envelope = %s", raw)
	}
}

func TestInboundResultValid(t *testing.T) {
	for _, r := range []InboundResult{InboundApplied, InboundStale, InboundDuplicate, InboundMismatch, InboundUnmapped,
		InboundInvalidSignature, InboundInvalidPayload, InboundError} {
		if !r.Valid() {
			t.Errorf("%s invalid", r)
		}
	}
	if InboundResult("ok").Valid() || WebhookDeliveryStatus("pending").Valid() || WebhookAPIVersion("v3").Valid() || RetryBackoff("").Valid() {
		t.Fatal("unknown value valid")
	}
}
