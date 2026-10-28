package handler

import (
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/pkg/logger"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/dto"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// WebhookHandler receives inbound Xendit callbacks.
//
// Webhook endpoints are authenticated via Xendit's callback token header,
// verified by middleware before reaching this handler.
type WebhookHandler struct {
	svc *service.WebhookService
	log *zap.Logger
}

// NewWebhookHandler constructs a WebhookHandler.
func NewWebhookHandler(svc *service.WebhookService, log *zap.Logger) *WebhookHandler {
	return &WebhookHandler{svc: svc, log: log}
}

// Xendit handles POST /webhooks/xendit.
//
// The x-callback-token is verified by middleware before this runs.
// Always returns 200 for accepted/duplicate events so Xendit does not retry
// on our internal hiccups; real failures return 5xx.
func (h *WebhookHandler) Xendit(c *gin.Context) {
	start := time.Now()
	ctx := c.Request.Context()
	// Correlate inbound webhook logs with the request id for end-to-end tracing.
	log := logger.Ctx(ctx, h.log)

	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		log.Warn("webhook inbound read failed",
			zap.String(logger.FieldLogType, logger.LogTypeWebhook),
			zap.String(logger.FieldEvent, "webhook.inbound.read_error"),
			zap.Error(err),
		)
		response.BadRequest(c, "cannot read body")
		return
	}

	var cb service.XenditInvoiceCallback
	if err := json.Unmarshal(raw, &cb); err != nil {
		log.Warn("webhook inbound invalid json",
			zap.String(logger.FieldLogType, logger.LogTypeWebhook),
			zap.String(logger.FieldEvent, "webhook.inbound.invalid_json"),
			zap.Int("bytes", len(raw)),
			zap.Error(err),
		)
		response.BadRequest(c, "invalid json")
		return
	}

	log.Info("webhook inbound received",
		zap.String(logger.FieldLogType, logger.LogTypeWebhook),
		zap.String(logger.FieldEvent, "webhook.inbound.received"),
		zap.String("provider", "xendit"),
		zap.String("xendit_id", cb.ID),
		zap.String("external_id", cb.ExternalID),
		zap.String("event_status", cb.Status),
		zap.Int("bytes", len(raw)),
	)

	err = h.svc.Process(ctx, cb, raw)
	if errors.Is(err, service.ErrForwardNotQueued) {
		// Committed; the sweeper enqueues the forward. Ack so Xendit stops retrying.
		log.Warn("webhook forward enqueue deferred",
			zap.String(logger.FieldLogType, logger.LogTypeWebhook),
			zap.String(logger.FieldEvent, "webhook.inbound.enqueue_deferred"),
			zap.String("xendit_id", cb.ID),
			zap.String("external_id", cb.ExternalID),
			zap.Error(err),
		)
		err = nil
	}
	if errors.Is(err, service.ErrCallbackMismatch) {
		// Recorded but not applied; retrying would not change the outcome.
		logger.Security(h.log, logger.EventCallbackMismatch, logger.SeverityHigh,
			c.ClientIP(), logger.RequestIDFromContext(ctx), err.Error(),
			zap.String("xendit_id", cb.ID),
			zap.String("external_id", cb.ExternalID),
		)
		err = nil
	}
	if err != nil {
		log.Error("webhook inbound process failed",
			zap.String(logger.FieldLogType, logger.LogTypeWebhook),
			zap.String(logger.FieldEvent, "webhook.inbound.failed"),
			zap.String("xendit_id", cb.ID),
			zap.String("external_id", cb.ExternalID),
			zap.Int64(logger.FieldLatencyMS, time.Since(start).Milliseconds()),
			zap.Error(err),
		)
		response.InternalError(c, "processing failed")
		return
	}

	log.Info("webhook inbound accepted",
		zap.String(logger.FieldLogType, logger.LogTypeWebhook),
		zap.String(logger.FieldEvent, "webhook.inbound.accepted"),
		zap.String("xendit_id", cb.ID),
		zap.String("external_id", cb.ExternalID),
		zap.Int64(logger.FieldLatencyMS, time.Since(start).Milliseconds()),
	)
	response.Success(c, dto.ReceivedResponse{Received: true})
}
