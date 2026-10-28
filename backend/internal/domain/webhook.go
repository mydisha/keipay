package domain

import (
	"encoding/json"
	"time"
)

// WebhookDeliveryStatus is the state of an outbound webhook forward to an app.
type WebhookDeliveryStatus string

const (
	WebhookDeliveryPending   WebhookDeliveryStatus = "PENDING"
	WebhookDeliveryDelivered WebhookDeliveryStatus = "DELIVERED"
	WebhookDeliveryFailed    WebhookDeliveryStatus = "FAILED"
)

// WebhookEvent is an audit + idempotency record of an inbound Xendit callback.
type WebhookEvent struct {
	ID         int64     `json:"id"`
	XenditID   string    `json:"xendit_id"`
	Status     string    `json:"status"`
	ReceivedAt time.Time `json:"received_at"`
}

// WebhookDelivery is the source-of-truth audit record for forwarding a
// normalized event to an app's webhook URL. Execution/retry is driven by asynq.
type WebhookDelivery struct {
	ID        int64
	PaymentID int64
	AppID     int64
	// PaymentPublicID and App are filled by list queries only.
	PaymentPublicID string
	App             AppSummary
	EventType       string
	Payload         json.RawMessage
	Status          WebhookDeliveryStatus
	Attempts        int
	LastError       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// DeliveryFilter narrows delivery lists. AppPublicID is a canonical app_ id.
type DeliveryFilter struct {
	AppPublicID *string
	Status      *WebhookDeliveryStatus
	Limit       int
	Offset      int
}
