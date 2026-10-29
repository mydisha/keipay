package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/pkg/logger"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/dto"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// WebhookHandler receives inbound gateway callbacks. Each connection has its
// own URL and is verified with its own credentials, by the service. Every
// callback that reaches a known connection is recorded in the inbound log.
type WebhookHandler struct {
	svc   *service.WebhookService
	conns *service.ConnectionService
	log   *zap.Logger
}

// NewWebhookHandler constructs a WebhookHandler.
func NewWebhookHandler(svc *service.WebhookService, conns *service.ConnectionService, log *zap.Logger) *WebhookHandler {
	return &WebhookHandler{svc: svc, conns: conns, log: log}
}

// MaxCallbackBytes caps an inbound callback body. Callback URLs are public and
// the body is read before the credentials are checked, so an unbounded read
// would let anyone exhaust memory; gateway callbacks are a few KB.
const MaxCallbackBytes = 1 << 20

// resolver finds the connection a callback URL names, without the body.
type resolver func(c *gin.Context) (*domain.Connection, error)

// Connection handles POST /webhooks/{provider}/{connection_id}.
func (h *WebhookHandler) Connection(c *gin.Context) {
	h.receive(c, func(c *gin.Context) (*domain.Connection, error) {
		connID, err := id.ParseWithPrefix(c.Param("connection_id"), id.Connection)
		if err != nil {
			return nil, service.ErrUnknownConnection
		}
		return h.conns.CallbackConnection(c.Request.Context(), c.Param("provider"), connID)
	})
}

// Xendit handles POST /webhooks/xendit, the alias of the connection imported
// from the deprecated XENDIT_* settings.
func (h *WebhookHandler) Xendit(c *gin.Context) {
	h.receive(c, func(c *gin.Context) (*domain.Connection, error) {
		return h.conns.LegacyCallbackConnection(c.Request.Context())
	})
}

// receive resolves the connection, then reads, verifies and applies one
// callback. Accepted, duplicate, stale and recorded-but-mismatched callbacks
// answer 200 so the gateway stops retrying; real processing failures answer
// 500 so it retries.
func (h *WebhookHandler) receive(c *gin.Context, resolve resolver) {
	start := time.Now()
	ctx := c.Request.Context()
	log := logger.Ctx(ctx, h.log)

	conn, err := resolve(c)
	if err != nil {
		h.rejected(c, nil, 0, err)
		return
	}

	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, MaxCallbackBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		event, apiErr := "webhook.inbound.read_error", invalidBody()
		if errors.As(err, &tooLarge) {
			event, apiErr = "webhook.inbound.too_large", invalidRequest("request body too large")
		}
		log.Warn("webhook inbound read failed",
			zap.String(logger.FieldLogType, logger.LogTypeWebhook),
			zap.String(logger.FieldEvent, event),
			zap.String("connection_id", conn.PublicID),
			zap.Error(err),
		)
		response.Error(c, apiErr)
		return
	}

	ev, err := h.conns.VerifyCallback(conn, c.Request.Header, raw)
	if err != nil {
		h.recordRejected(ctx, conn, raw, err)
		h.rejected(c, conn, len(raw), err)
		return
	}

	fields := []zap.Field{
		zap.String("connection_id", conn.PublicID),
		zap.String("provider", conn.Provider),
		zap.String("event_key", ev.EventKey),
		zap.String("external_id", ev.Reference),
		zap.String("event_status", ev.ProviderStatus),
	}
	log.Info("webhook inbound received", append(fields,
		zap.String(logger.FieldLogType, logger.LogTypeWebhook),
		zap.String(logger.FieldEvent, "webhook.inbound.received"),
		zap.Int("bytes", len(raw)),
	)...)

	err = h.svc.Process(ctx, conn, *ev, raw)
	if errors.Is(err, domain.ErrInvalidInput) {
		// Recorded as invalid_payload by the service.
		h.rejected(c, conn, len(raw), err)
		return
	}
	if errors.Is(err, service.ErrForwardNotQueued) {
		// Committed; the sweeper enqueues the forward. Ack so the gateway stops retrying.
		log.Warn("webhook forward enqueue deferred", append(fields,
			zap.String(logger.FieldLogType, logger.LogTypeWebhook),
			zap.String(logger.FieldEvent, "webhook.inbound.enqueue_deferred"),
			zap.Error(err),
		)...)
		err = nil
	}
	if errors.Is(err, service.ErrCallbackMismatch) {
		// Recorded but not applied; retrying would not change the outcome.
		extra := fields
		var mismatch *service.ConnectionMismatchError
		if errors.As(err, &mismatch) {
			extra = append(extra, zap.Int64("connection_db_id", mismatch.ConnectionID))
			if mismatch.PaymentConnectionID != nil {
				extra = append(extra, zap.Int64("payment_connection_db_id", *mismatch.PaymentConnectionID))
			}
		}
		logger.Security(h.log, logger.EventCallbackMismatch, logger.SeverityHigh,
			c.ClientIP(), logger.RequestIDFromContext(ctx), err.Error(), extra...)
		err = nil
	}
	if err != nil {
		log.Error("webhook inbound process failed", append(fields,
			zap.String(logger.FieldLogType, logger.LogTypeWebhook),
			zap.String(logger.FieldEvent, "webhook.inbound.failed"),
			zap.Int64(logger.FieldLatencyMS, time.Since(start).Milliseconds()),
			zap.Error(err),
		)...)
		// Every processing failure is a 500, including an unknown reference,
		// so the gateway retries once the payment row exists.
		response.Error(c, domain.Internal(err))
		return
	}

	log.Info("webhook inbound accepted", append(fields,
		zap.String(logger.FieldLogType, logger.LogTypeWebhook),
		zap.String(logger.FieldEvent, "webhook.inbound.accepted"),
		zap.Int64(logger.FieldLatencyMS, time.Since(start).Milliseconds()),
	)...)
	response.Success(c, dto.ReceivedResponse{Received: true})
}

// Result details of rejected callbacks. They are fixed strings, so neither
// a credential nor the parser's text reaches the console through them.
const (
	detailInvalidSignature = "verification with the connection's credentials failed"
	detailInvalidPayload   = "the body is not a callback this provider sends"
)

// recordRejected logs a callback of a known connection that failed
// verification or parsing; other errors are not about the callback.
func (h *WebhookHandler) recordRejected(ctx context.Context, conn *domain.Connection, raw []byte, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidCallbackToken):
		h.svc.RecordRejected(ctx, conn, raw, domain.InboundInvalidSignature, detailInvalidSignature)
	case errors.Is(err, domain.ErrInvalidInput):
		h.svc.RecordRejected(ctx, conn, raw, domain.InboundInvalidPayload, detailInvalidPayload)
	}
}

// rejected answers a callback that could not be verified or read. conn is set
// when the connection was found.
func (h *WebhookHandler) rejected(c *gin.Context, conn *domain.Connection, bytes int, err error) {
	ctx := c.Request.Context()
	reqID := logger.RequestIDFromContext(ctx)
	switch {
	case errors.Is(err, service.ErrUnknownConnection):
		logger.Security(h.log, logger.EventCallbackUnknownConnection, logger.SeverityLow, c.ClientIP(), reqID,
			"callback for an unknown connection", zap.String("path", c.Request.URL.Path))
		response.Error(c, domain.NewError(domain.CodeNotFound, "not found"))
	case errors.Is(err, domain.ErrInvalidCallbackToken):
		logger.Security(h.log, logger.EventCallbackInvalidToken, logger.SeverityHigh, c.ClientIP(), reqID,
			"invalid callback token", zap.String("connection_id", conn.PublicID), zap.String("provider", conn.Provider))
		response.Error(c, err)
	case errors.Is(err, domain.ErrInvalidInput):
		logger.Ctx(ctx, h.log).Warn("webhook inbound invalid body",
			zap.String(logger.FieldLogType, logger.LogTypeWebhook),
			zap.String(logger.FieldEvent, "webhook.inbound.invalid_body"),
			zap.String("connection_id", conn.PublicID),
			zap.Int("bytes", bytes),
			zap.Error(err),
		)
		response.Error(c, invalidBody())
	default:
		logger.Ctx(ctx, h.log).Error("webhook inbound verification failed",
			zap.String(logger.FieldLogType, logger.LogTypeWebhook),
			zap.String(logger.FieldEvent, "webhook.inbound.failed"),
			zap.Error(err),
		)
		response.Error(c, domain.Internal(err))
	}
}
