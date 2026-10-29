// Package worker defines asynq tasks and handlers for async webhook forwarding.
package worker

import (
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
)

// TypeWebhookForward is the asynq task type for one attempt of a delivery.
const TypeWebhookForward = "webhook:forward"

// QueueWebhooks is the queue forward tasks run on.
const QueueWebhooks = "webhooks"

// WebhookForwardPayload names one attempt of a delivery; the handler loads
// the durable record from Postgres to avoid stale data in the queue. Attempt
// 0 (tasks queued before attempts were numbered) means the next attempt.
type WebhookForwardPayload struct {
	DeliveryID int64 `json:"delivery_id"`
	Attempt    int   `json:"attempt"`
	Manual     bool  `json:"manual"`
}

// NewWebhookForwardTask builds the task of one attempt.
func NewWebhookForwardTask(deliveryID int64, attempt int, manual bool) (*asynq.Task, error) {
	b, err := json.Marshal(WebhookForwardPayload{DeliveryID: deliveryID, Attempt: attempt, Manual: manual})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeWebhookForward, b), nil
}

// ForwardTaskID is the task id of one attempt, so an attempt is queued at
// most once.
func ForwardTaskID(deliveryID int64, attempt int) string {
	return fmt.Sprintf("%s:%d:%d", TypeWebhookForward, deliveryID, attempt)
}
