package domain

import "time"

// InboundResult is what KeiPay did with a gateway callback.
type InboundResult string

// Inbound results.
const (
	InboundApplied          InboundResult = "applied"
	InboundStale            InboundResult = "stale"
	InboundDuplicate        InboundResult = "duplicate"
	InboundMismatch         InboundResult = "mismatch"
	InboundUnmapped         InboundResult = "unmapped"
	InboundInvalidSignature InboundResult = "invalid_signature"
	InboundInvalidPayload   InboundResult = "invalid_payload"
	InboundError            InboundResult = "error"
)

// Valid reports whether r is a known result.
func (r InboundResult) Valid() bool {
	switch r {
	case InboundApplied, InboundStale, InboundDuplicate, InboundMismatch, InboundUnmapped,
		InboundInvalidSignature, InboundInvalidPayload, InboundError:
		return true
	default:
		return false
	}
}

// InboundSourceWebhook is the source of a gateway callback.
const InboundSourceWebhook = "webhook"

// MaxRejectedBodyBytes caps the stored body of a callback that failed
// verification: it comes from anyone who knows the URL.
const MaxRejectedBodyBytes = 4096

// InboundWebhook is one callback that reached a known connection, with what
// processing did with it.
type InboundWebhook struct {
	ID             int64
	PublicID       string
	ConnectionID   int64
	Connection     ConnectionSummary
	Provider       string
	Source         string
	SignatureValid bool
	Result         InboundResult
	ResultDetail   string
	// EventKey, ProviderStatus and Reference are nil when the body was not read.
	EventKey       *string
	ProviderStatus *string
	Reference      *string
	// PaymentID is set once the callback was matched to a payment.
	PaymentID       *int64
	PaymentPublicID string
	EventID         *int64
	Event           *EventRef
	RawBody         []byte
	BodyTruncated   bool
	ReceivedAt      time.Time
}

// InboundFilter narrows inbound lists; nil fields match everything.
type InboundFilter struct {
	ConnectionPublicID *string
	Result             *InboundResult
	Environment        *Environment
	From               *time.Time
	To                 *time.Time
}
