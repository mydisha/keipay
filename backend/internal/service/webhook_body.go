package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// ForwardPayload is the v1 webhook body: the flat event sent before the v2
// envelope, kept for endpoints migrated from an app's webhook_url. Fields are
// only ever added.
type ForwardPayload struct {
	Event           domain.EventType     `json:"event"`
	EventID         string               `json:"event_id"`
	PaymentID       int64                `json:"payment_id"`
	PaymentPublicID string               `json:"payment_public_id"`
	OrderID         string               `json:"order_id"`
	ExternalID      string               `json:"external_id"`
	Metadata        domain.Metadata      `json:"metadata"`
	Status          domain.PaymentStatus `json:"status"`
	Amount          int64                `json:"amount"`
	Currency        string               `json:"currency"`
	PaidAt          *time.Time           `json:"paid_at,omitempty"`
	SettledAt       *time.Time           `json:"settled_at,omitempty"`
	Timestamp       time.Time            `json:"timestamp"`
}

// renderBody is the body an endpoint with version v receives for e. It
// depends only on e, so every delivery of an event of one version carries
// the same bytes.
func renderBody(e *domain.Event, v domain.WebhookAPIVersion) (json.RawMessage, error) {
	if v != domain.WebhookAPIV1 {
		b, err := json.Marshal(e.Envelope())
		if err != nil {
			return nil, fmt.Errorf("render v2 body: %w", err)
		}
		return b, nil
	}
	var data domain.PaymentEventData
	dec := json.NewDecoder(bytes.NewReader(e.Data))
	dec.UseNumber()
	if err := dec.Decode(&data); err != nil {
		return nil, fmt.Errorf("decode event data: %w", err)
	}
	var paymentID int64
	if e.PaymentID != nil {
		paymentID = *e.PaymentID
	}
	b, err := json.Marshal(ForwardPayload{
		Event: e.Type, EventID: e.PublicID, PaymentID: paymentID, PaymentPublicID: data.ID,
		OrderID: data.OrderID, ExternalID: data.Reference, Metadata: data.Metadata, Status: data.Status,
		Amount: data.Amount, Currency: data.Currency, PaidAt: data.PaidAt, SettledAt: data.SettledAt,
		Timestamp: e.CreatedAt.UTC(),
	})
	if err != nil {
		return nil, fmt.Errorf("render v1 body: %w", err)
	}
	return b, nil
}

// paymentEventData is p as of an event that moved it to status, through conn.
func paymentEventData(p *domain.Payment, conn *domain.Connection, status domain.PaymentStatus, paidAt, settledAt *time.Time) domain.PaymentEventData {
	provider, connID := conn.Provider, conn.PublicID
	return domain.PaymentEventData{
		ID: p.PublicID, OrderID: p.OrderID, Reference: p.Reference(), Status: status, Amount: p.Amount,
		Currency: p.Currency, Metadata: orEmpty(p.Metadata), Provider: &provider, ConnectionID: &connID,
		PaidAt: utcTime(paidAt), SettledAt: utcTime(settledAt),
	}
}

func utcTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// marshalData is the JSON of an event's data.
func marshalData(d domain.PaymentEventData) (json.RawMessage, error) {
	b, err := json.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("encode event data: %w", err)
	}
	return b, nil
}
