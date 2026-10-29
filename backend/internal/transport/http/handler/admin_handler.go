package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/dto"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// AdminHandler exposes the reporting and audit endpoints of the admin API.
// Sessions, CSRF and permissions are enforced by the router's middleware
// before these handlers run.
type AdminHandler struct {
	payments *service.PaymentService
	audit    *service.AuditService
}

// NewAdminHandler constructs an AdminHandler.
func NewAdminHandler(payments *service.PaymentService, audit *service.AuditService) *AdminHandler {
	return &AdminHandler{payments: payments, audit: audit}
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
