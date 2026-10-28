package handler

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/dto"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
	"github.com/mydisha/keipay/backend/internal/worker"
)

// AdminHandler exposes admin/UI endpoints (login, tenant CRUD, reports).
//
// Admin endpoints require authentication via JWT token in the Authorization header.
// The JWT is validated by middleware before reaching these handlers.
type AdminHandler struct {
	auth     *service.AuthService
	tenants  *service.TenantService
	payments *service.PaymentService
	ledger   *service.LedgerService
	webhooks domain.WebhookRepository
	enqueuer *worker.Enqueuer
}

// NewAdminHandler constructs an AdminHandler.
func NewAdminHandler(
	auth *service.AuthService,
	tenants *service.TenantService,
	payments *service.PaymentService,
	ledger *service.LedgerService,
	webhooks domain.WebhookRepository,
	enqueuer *worker.Enqueuer,
) *AdminHandler {
	return &AdminHandler{auth: auth, tenants: tenants, payments: payments, ledger: ledger, webhooks: webhooks, enqueuer: enqueuer}
}

// Login handles POST /admin/login.
// Authenticates admin users and returns a JWT token.
func (h *AdminHandler) Login(c *gin.Context) {
	var req dto.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	result, err := h.auth.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		response.Unauthorized(c, "invalid credentials")
		return
	}
	response.Success(c, result)
}

// ChangePassword handles POST /admin/change-password.
// Requires the admin to be authenticated and provide their current password.
func (h *AdminHandler) ChangePassword(c *gin.Context) {
	username, _ := c.Get("admin")
	if username == nil || username == "" {
		response.Unauthorized(c, "")
		return
	}

	var req dto.ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	if err := h.auth.ChangePassword(c.Request.Context(), username.(string), req.CurrentPassword, req.NewPassword); err != nil {
		if err == domain.ErrUnauthorized {
			response.Unauthorized(c, "invalid current password")
			return
		}
		response.InternalError(c, "failed to change password")
		return
	}
	response.Success(c, dto.MessageResponse{Message: "password changed successfully"})
}

// CreateTenant handles POST /admin/tenants.
// Returns one-time API credentials that should be securely stored by the caller.
func (h *AdminHandler) CreateTenant(c *gin.Context) {
	var req dto.CreateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	t, creds, err := h.tenants.Create(c.Request.Context(), service.CreateTenantInput{
		Name:               req.Name,
		Slug:               req.Slug,
		WebhookURL:         req.WebhookURL,
		RedirectSuccessURL: req.RedirectSuccessURL,
		RedirectFailureURL: req.RedirectFailureURL,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, dto.TenantResponse{Tenant: t, Credentials: creds})
}

// ListTenants handles GET /admin/tenants.
func (h *AdminHandler) ListTenants(c *gin.Context) {
	ts, err := h.tenants.List(c.Request.Context())
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.TenantListResponse{Tenants: ts})
}

// UpdateTenant handles PUT /admin/tenants/:id.
func (h *AdminHandler) UpdateTenant(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	var req dto.UpdateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	t, err := h.tenants.Update(c.Request.Context(), id, service.UpdateTenantInput{
		Name:               req.Name,
		WebhookURL:         req.WebhookURL,
		RedirectSuccessURL: req.RedirectSuccessURL,
		RedirectFailureURL: req.RedirectFailureURL,
		Active:             req.Active,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.TenantResponse{Tenant: t})
}

// RotateTenantCredentials handles POST /admin/tenants/:id/rotate.
// Generates new API credentials and invalidates the old ones.
func (h *AdminHandler) RotateTenantCredentials(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	t, creds, err := h.tenants.RotateCredentials(c.Request.Context(), id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.TenantResponse{Tenant: t, Credentials: creds})
}

// ListPayments handles GET /admin/payments with optional filters.
// Query params: tenant_id, status, from, to, limit, offset
func (h *AdminHandler) ListPayments(c *gin.Context) {
	f := domain.PaymentFilter{
		Limit:  atoiDefault(c.Query("limit"), 50),
		Offset: atoiDefault(c.Query("offset"), 0),
	}
	if v := c.Query("tenant_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.TenantID = &id
		}
	}
	if v := c.Query("status"); v != "" {
		s := domain.PaymentStatus(v)
		f.Status = &s
	}
	if t, ok := parseTimeQuery(c.Query("from")); ok {
		f.From = &t
	}
	if t, ok := parseTimeQuery(c.Query("to")); ok {
		f.To = &t
	}
	items, total, err := h.payments.List(c.Request.Context(), f)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.PaymentListResponse{Payments: items, Total: total})
}

// ListLedger handles GET /admin/ledger and returns entries + per-tenant summary.
// Query params: tenant_id, from, to, limit, offset
func (h *AdminHandler) ListLedger(c *gin.Context) {
	f := domain.LedgerFilter{
		Limit:  atoiDefault(c.Query("limit"), 100),
		Offset: atoiDefault(c.Query("offset"), 0),
	}
	if v := c.Query("tenant_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.TenantID = &id
		}
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
	response.Success(c, dto.LedgerListResponse{Entries: entries, Summary: summary})
}

// ListDeliveries handles GET /admin/webhook-deliveries.
// Query params: tenant_id, status, limit, offset
func (h *AdminHandler) ListDeliveries(c *gin.Context) {
	var tenantID *int64
	if v := c.Query("tenant_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			tenantID = &id
		}
	}
	var status *domain.WebhookDeliveryStatus
	if v := c.Query("status"); v != "" {
		s := domain.WebhookDeliveryStatus(v)
		status = &s
	}
	items, err := h.webhooks.ListDeliveries(c.Request.Context(), tenantID, status,
		atoiDefault(c.Query("limit"), 50), atoiDefault(c.Query("offset"), 0))
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.DeliveryListResponse{Deliveries: items})
}

// ReplayDelivery handles POST /admin/webhook-deliveries/:id/replay.
// Re-enqueues a webhook delivery for retry.
func (h *AdminHandler) ReplayDelivery(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	if _, err := h.webhooks.GetDelivery(c.Request.Context(), id); err != nil {
		response.Error(c, err)
		return
	}
	if err := h.enqueuer.ReplayWebhookForward(c.Request.Context(), id); err != nil {
		response.InternalError(c, "enqueue failed")
		return
	}
	response.Accepted(c, dto.ReplayResponse{Requeued: true})
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
