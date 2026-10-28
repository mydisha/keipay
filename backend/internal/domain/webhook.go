package domain

import (
	"encoding/json"
	"time"
)

// WebhookDeliveryStatus is the state of an outbound webhook forward to a tenant.
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
// normalized event to a tenant's webhook URL. Execution/retry is driven by asynq.
type WebhookDelivery struct {
	ID        int64                 `json:"id"`
	PaymentID int64                 `json:"payment_id"`
	TenantID  int64                 `json:"tenant_id"`
	EventType string                `json:"event_type"`
	Payload   json.RawMessage       `json:"payload"`
	Status    WebhookDeliveryStatus `json:"status"`
	Attempts  int                   `json:"attempts"`
	LastError string                `json:"last_error"`
	CreatedAt time.Time             `json:"created_at"`
	UpdatedAt time.Time             `json:"updated_at"`
}
