// Package worker defines asynq tasks and handlers for async webhook forwarding.
package worker

import (
	"encoding/json"

	"github.com/hibiken/asynq"
)

// TypeWebhookForward is the asynq task type for forwarding a webhook to an app.
const TypeWebhookForward = "webhook:forward"

// WebhookForwardPayload carries the delivery id; the handler loads the durable
// record from Postgres to avoid stale data in the queue.
type WebhookForwardPayload struct {
	DeliveryID int64 `json:"delivery_id"`
}

// NewWebhookForwardTask builds an asynq task for a delivery.
func NewWebhookForwardTask(deliveryID int64) (*asynq.Task, error) {
	b, err := json.Marshal(WebhookForwardPayload{DeliveryID: deliveryID})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeWebhookForward, b), nil
}
