// Package handler contains HTTP handlers for the KeiPay API.
//
// Handler Responsibilities:
// - Parse HTTP requests (JSON body, query params, path params)
// - Validate input using DTO binding tags
// - Call service layer with validated data
// - Return HTTP responses using the response package
//
// Handlers MUST NOT contain business logic. All business rules belong in the
// service layer. This separation ensures:
// - Testability: Services can be tested without HTTP overhead
// - Reusability: Business logic can be exposed via other transports (gRPC, etc.)
// - Maintainability: Changes to HTTP concerns don't affect business logic
package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/dto"
	"github.com/mydisha/keipay/backend/internal/transport/http/middleware"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// PaymentHandler exposes tenant-facing payment endpoints.
type PaymentHandler struct {
	svc *service.PaymentService
}

// NewPaymentHandler constructs a PaymentHandler.
func NewPaymentHandler(svc *service.PaymentService) *PaymentHandler {
	return &PaymentHandler{svc: svc}
}

// Create handles POST /v1/payments.
// Request body: dto.CreatePaymentRequest
// Response: dto.PaymentResponse (201 Created)
func (h *PaymentHandler) Create(c *gin.Context) {
	tenant, ok := middleware.TenantFromContext(c)
	if !ok {
		response.Unauthorized(c, "no tenant context")
		return
	}

	var req dto.CreatePaymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	p, err := h.svc.Create(c.Request.Context(), tenant, service.CreatePaymentInput{
		OrderID:              req.OrderID,
		Amount:               req.Amount,
		Currency:             req.Currency,
		Description:          req.Description,
		PayerEmail:           req.PayerEmail,
		InvoiceDuration:      req.InvoiceDuration,
		Items:                req.ToOrderItems(),
		Customer:             req.ToCustomer(),
		ExpectedSettlementAt: req.ExpectedSettlementAt,
	})

	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, dto.PaymentResponseFromDomain(p))
}

// Get handles GET /v1/payments/:id scoped to the authenticated tenant.
// Response: dto.PaymentResponse (200 OK)
func (h *PaymentHandler) Get(c *gin.Context) {
	tenant, ok := middleware.TenantFromContext(c)
	if !ok {
		response.Unauthorized(c, "no tenant context")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	p, err := h.svc.Get(c.Request.Context(), tenant.ID, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.PaymentResponseFromDomain(p))
}
