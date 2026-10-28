package dto

import (
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/service"
)

// ============================================================================
// Tenant DTOs
// ============================================================================

// CreateTenantRequest is the request body for POST /admin/tenants.
type CreateTenantRequest struct {
	Name               string `json:"name" binding:"required"`
	Slug               string `json:"slug" binding:"required"`
	WebhookURL         string `json:"webhook_url"`
	RedirectSuccessURL string `json:"redirect_success_url"`
	RedirectFailureURL string `json:"redirect_failure_url"`
}

// UpdateTenantRequest is the request body for PUT /admin/tenants/:id.
type UpdateTenantRequest struct {
	Name               string `json:"name" binding:"required"`
	WebhookURL         string `json:"webhook_url"`
	RedirectSuccessURL string `json:"redirect_success_url"`
	RedirectFailureURL string `json:"redirect_failure_url"`
	Active             bool   `json:"active"`
}

// TenantResponse wraps tenant data for API responses.
type TenantResponse struct {
	Tenant      *domain.Tenant       `json:"tenant"`
	Credentials *service.Credentials `json:"credentials,omitempty"`
}

// TenantListResponse is the response for GET /admin/tenants.
type TenantListResponse struct {
	Tenants []domain.Tenant `json:"tenants"`
}

// ============================================================================
// Admin Payment/Ledger DTOs
// ============================================================================

// PaymentListResponse is the response for GET /admin/payments.
type PaymentListResponse struct {
	Payments []domain.Payment `json:"payments"`
	Total    int64            `json:"total"`
}

// LedgerListResponse is the response for GET /admin/ledger.
type LedgerListResponse struct {
	Entries []domain.LedgerEntry   `json:"entries"`
	Summary []domain.LedgerSummary `json:"summary"`
}

// ============================================================================
// Webhook Delivery DTOs
// ============================================================================

// DeliveryListResponse is the response for GET /admin/webhook-deliveries.
type DeliveryListResponse struct {
	Deliveries []domain.WebhookDelivery `json:"deliveries"`
}

// ReplayResponse is the response for POST /admin/webhook-deliveries/:id/replay.
type ReplayResponse struct {
	Requeued bool `json:"requeued"`
}

// ============================================================================
// Common Response DTOs
// ============================================================================

// MessageResponse is a generic message response.
type MessageResponse struct {
	Message string `json:"message"`
}

// ReceivedResponse is the standard webhook acknowledgment.
type ReceivedResponse struct {
	Received bool `json:"received"`
}

// ============================================================================
// Query Parameter Helpers
// ============================================================================

// PaymentFilterQuery represents query parameters for payment listing.
type PaymentFilterQuery struct {
	TenantID *int64                `form:"tenant_id"`
	Status   *domain.PaymentStatus `form:"status"`
	From     *time.Time            `form:"from"`
	To       *time.Time            `form:"to"`
	Limit    int                   `form:"limit"`
	Offset   int                   `form:"offset"`
}

// LedgerFilterQuery represents query parameters for ledger listing.
type LedgerFilterQuery struct {
	TenantID *int64     `form:"tenant_id"`
	From     *time.Time `form:"from"`
	To       *time.Time `form:"to"`
	Limit    int        `form:"limit"`
	Offset   int        `form:"offset"`
}

// DeliveryFilterQuery represents query parameters for delivery listing.
type DeliveryFilterQuery struct {
	TenantID *int64                        `form:"tenant_id"`
	Status   *domain.WebhookDeliveryStatus `form:"status"`
	Limit    int                           `form:"limit"`
	Offset   int                           `form:"offset"`
}
