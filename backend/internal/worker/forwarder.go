package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/hmac"
	"github.com/mydisha/keipay/backend/internal/pkg/logger"
)

// SecretDecryptor recovers the plaintext tenant secret used to sign forwards.
type SecretDecryptor interface {
	Decrypt(ciphertext string) (string, error)
}

// Forwarder executes webhook:forward tasks: it POSTs the normalized event to the
// tenant's webhook URL, HMAC-signed with the tenant secret. asynq drives retries.
type Forwarder struct {
	tenants  domain.TenantRepository
	webhooks domain.WebhookRepository
	cipher   SecretDecryptor
	client   *http.Client
	log      *zap.Logger
	maxRetry int
}

// NewForwarder constructs a Forwarder.
func NewForwarder(tenants domain.TenantRepository, webhooks domain.WebhookRepository, cipher SecretDecryptor, log *zap.Logger, maxRetry int) *Forwarder {
	return &Forwarder{
		tenants:  tenants,
		webhooks: webhooks,
		cipher:   cipher,
		client:   &http.Client{Timeout: 15 * time.Second},
		log:      log,
		maxRetry: maxRetry,
	}
}

// HandleWebhookForward implements asynq.HandlerFunc for TypeWebhookForward.
// Returning an error triggers asynq retry; nil marks the task done.
func (f *Forwarder) HandleWebhookForward(ctx context.Context, task *asynq.Task) error {
	var p WebhookForwardPayload
	if err := json.Unmarshal(task.Payload(), &p); err != nil {
		return fmt.Errorf("unmarshal payload: %w: %v", asynq.SkipRetry, err)
	}

	d, err := f.webhooks.GetDelivery(ctx, p.DeliveryID)
	if err != nil {
		return fmt.Errorf("get delivery %d: %w", p.DeliveryID, err)
	}
	if d.Status == domain.WebhookDeliveryDelivered {
		return nil // already delivered
	}

	t, err := f.tenants.GetByID(ctx, d.TenantID)
	if err != nil {
		return fmt.Errorf("get tenant %d: %w", d.TenantID, err)
	}
	if t.WebhookURL == "" {
		_ = f.webhooks.MarkFailed(ctx, d.ID, d.Attempts+1, "tenant has no webhook_url")
		return fmt.Errorf("tenant %d has no webhook_url: %w", t.ID, asynq.SkipRetry)
	}

	// Sign the forward with the tenant's plaintext secret (decrypted at rest) so
	// the tenant verifies using the same secret KeiPayment issued to them.
	secret, err := f.cipher.Decrypt(t.APISecretEnc)
	if err != nil {
		_ = f.webhooks.MarkFailed(ctx, d.ID, d.Attempts+1, "secret decrypt failed")
		return fmt.Errorf("decrypt tenant %d secret: %w", t.ID, asynq.SkipRetry)
	}
	signature := hmac.Sign(secret, d.Payload)

	attempt := d.Attempts + 1
	log := f.log.With(
		zap.String(logger.FieldLogType, logger.LogTypeWebhook),
		zap.Int64("delivery_id", d.ID),
		zap.Int64(logger.FieldTenantID, t.ID),
		zap.String("target_url", t.WebhookURL),
		zap.String("event_type", d.EventType),
		zap.Int("attempt", attempt),
		zap.Int("max_retry", f.maxRetry),
	)

	start := time.Now()
	statusCode, sendErr := f.post(ctx, t.WebhookURL, d.Payload, signature, d.EventType)
	latencyMS := time.Since(start).Milliseconds()
	if sendErr != nil || statusCode < 200 || statusCode >= 300 {
		lastErr := fmt.Sprintf("status=%d err=%v", statusCode, sendErr)
		// Record progress + error; asynq retries until MaxRetry then archives.
		_ = f.webhooks.MarkFailed(ctx, d.ID, attempt, lastErr)
		log.Warn("webhook outbound attempt failed",
			zap.String(logger.FieldEvent, "webhook.outbound.failed"),
			zap.Int("status_code", statusCode),
			zap.Int64(logger.FieldLatencyMS, latencyMS),
			zap.Error(sendErr),
		)
		return fmt.Errorf("forward delivery %d: %s", d.ID, lastErr)
	}

	if err := f.webhooks.MarkDelivered(ctx, d.ID, attempt); err != nil {
		log.Warn("mark delivered failed", zap.Error(err))
	}
	log.Info("webhook outbound delivered",
		zap.String(logger.FieldEvent, "webhook.outbound.delivered"),
		zap.Int("status_code", statusCode),
		zap.Int64(logger.FieldLatencyMS, latencyMS),
	)
	return nil
}

func (f *Forwarder) post(ctx context.Context, url string, body []byte, signature, eventType string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Kei-Signature", signature)
	req.Header.Set("X-Kei-Event", eventType)

	resp, err := f.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}
