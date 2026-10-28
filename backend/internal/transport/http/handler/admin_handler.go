package handler

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/dto"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// AdminHandler exposes the reporting, delivery and audit endpoints of the
// admin API. Sessions, CSRF and permissions are enforced by the router's
// middleware before these handlers run.
type AdminHandler struct {
	payments   *service.PaymentService
	ledger     *service.LedgerService
	deliveries *service.DeliveryService
	audit      *service.AuditService
}

// NewAdminHandler constructs an AdminHandler.
func NewAdminHandler(
	payments *service.PaymentService,
	ledger *service.LedgerService,
	deliveries *service.DeliveryService,
	audit *service.AuditService,
) *AdminHandler {
	return &AdminHandler{payments: payments, ledger: ledger, deliveries: deliveries, audit: audit}
}

// ListPayments handles GET /admin/payments (cursor-paginated, newest first).
// Every filter is validated strictly.
func (h *AdminHandler) ListPayments(c *gin.Context) {
	var f domain.PaymentFilter
	var err error
	if f.AppPublicID, err = queryPublicID(c, "app_id", id.App); err != nil {
		response.Error(c, err)
		return
	}
	if f.Status, err = queryEnum(c, "status", validPaymentStatus); err != nil {
		response.Error(c, err)
		return
	}
	if f.From, err = queryTime(c, "from"); err != nil {
		response.Error(c, err)
		return
	}
	if f.To, err = queryTime(c, "to"); err != nil {
		response.Error(c, err)
		return
	}
	req, err := pageRequest(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	page, err := h.payments.List(c.Request.Context(), f, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.PaymentListFromDomain(page))
}

func validPaymentStatus(s domain.PaymentStatus) bool {
	switch s {
	case domain.PaymentStatusPending, domain.PaymentStatusPaid, domain.PaymentStatusSettled,
		domain.PaymentStatusExpired, domain.PaymentStatusFailed:
		return true
	default:
		return false
	}
}

// ListLedger handles GET /admin/ledger and returns entries + per-app summary.
// Query params: app_id, from, to, limit, offset. Unparseable filters are
// ignored.
func (h *AdminHandler) ListLedger(c *gin.Context) {
	f := domain.LedgerFilter{
		AppPublicID: lenientPublicID(c, "app_id", id.App),
		Limit:       atoiDefault(c.Query("limit"), 100),
		Offset:      atoiDefault(c.Query("offset"), 0),
	}
	if t, ok := parseTimeQuery(c.Query("from")); ok {
		f.From = &t
	}
	if t, ok := parseTimeQuery(c.Query("to")); ok {
		f.To = &t
	}
	entries, err := h.ledger.List(c.Request.Context(), f)
	if err != nil {
		response.Error(c, err)
		return
	}
	summary, err := h.ledger.Summary(c.Request.Context(), f)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.LedgerListFromDomain(entries, summary))
}

// ListDeliveries handles GET /admin/webhook-deliveries.
// Query params: app_id, status, limit, offset. An unparseable app_id is ignored.
func (h *AdminHandler) ListDeliveries(c *gin.Context) {
	f := domain.DeliveryFilter{
		AppPublicID: lenientPublicID(c, "app_id", id.App),
		Limit:       atoiDefault(c.Query("limit"), 50),
		Offset:      atoiDefault(c.Query("offset"), 0),
	}
	if v := c.Query("status"); v != "" {
		s := domain.WebhookDeliveryStatus(v)
		f.Status = &s
	}
	items, err := h.deliveries.List(c.Request.Context(), f)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.DeliveryListFromDomain(items))
}

// ReplayDelivery handles POST /admin/webhook-deliveries/:id/replay.
// Re-enqueues a webhook delivery for retry.
func (h *AdminHandler) ReplayDelivery(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	if err := h.deliveries.Replay(c.Request.Context(), id); err != nil {
		response.Error(c, err)
		return
	}
	response.Accepted(c, dto.ReplayResponse{Requeued: true})
}

// ListAuditLogs handles GET /admin/audit-logs (cursor-paginated, newest first).
func (h *AdminHandler) ListAuditLogs(c *gin.Context) {
	req, err := pageRequest(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	page, err := h.audit.List(c.Request.Context(), req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.AuditLogListFromDomain(page))
}

// atoiDefault converts a string to int, returning a default value on failure.
func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	return def
}

// parseTimeQuery parses an RFC3339 timestamp from query parameter.
func parseTimeQuery(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	return time.Time{}, false
}
