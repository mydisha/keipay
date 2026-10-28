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

// SecretDecryptor recovers the plaintext webhook secret used to sign forwards.
type SecretDecryptor interface {
	Decrypt(ciphertext string) (string, error)
}

// AppGetter loads the app a delivery belongs to.
type AppGetter interface {
	GetByID(ctx context.Context, id int64) (*domain.App, error)
}

// Forwarder executes webhook:forward tasks: it POSTs the normalized event to the
// app's webhook URL, HMAC-signed with the app's webhook secret. asynq drives
// retries.
type Forwarder struct {
	apps     AppGetter
	webhooks domain.WebhookRepository
	cipher   SecretDecryptor
	client   *http.Client
	log      *zap.Logger
	maxRetry int
}

// NewForwarder constructs a Forwarder.
func NewForwarder(apps AppGetter, webhooks domain.WebhookRepository, cipher SecretDecryptor, log *zap.Logger, maxRetry int) *Forwarder {
	return &Forwarder{
		apps:     apps,
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

	app, err := f.apps.GetByID(ctx, d.AppID)
	if err != nil {
		return fmt.Errorf("get app %d: %w", d.AppID, err)
	}
	if app.WebhookURL == "" {
		_ = f.webhooks.MarkFailed(ctx, d.ID, d.Attempts+1, "app has no webhook_url")
		return fmt.Errorf("app %s has no webhook_url: %w", app.PublicID, asynq.SkipRetry)
	}

	// Signed with the app's webhook secret, not an API key secret, so rotating
	// or revoking keys never changes how the app verifies webhooks.
	secret, err := f.cipher.Decrypt(app.WebhookSecretEnc)
	if err != nil {
		_ = f.webhooks.MarkFailed(ctx, d.ID, d.Attempts+1, "secret decrypt failed")
		return fmt.Errorf("decrypt app %s webhook secret: %w", app.PublicID, asynq.SkipRetry)
	}
	signature := hmac.Sign(secret, d.Payload)

	attempt := d.Attempts + 1
	log := f.log.With(
		zap.String(logger.FieldLogType, logger.LogTypeWebhook),
		zap.Int64("delivery_id", d.ID),
		zap.String(logger.FieldAppID, app.PublicID),
		zap.String("target_url", app.WebhookURL),
		zap.String("event_type", d.EventType),
		zap.Int("attempt", attempt),
		zap.Int("max_retry", f.maxRetry),
	)

	start := time.Now()
	statusCode, sendErr := f.post(ctx, app.WebhookURL, d.Payload, signature, d.EventType)
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
