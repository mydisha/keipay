// Package handler holds the Gin handlers: parse the request, call a service,
// write the response. Business rules live in the service layer.
package handler

import (
	"net/http"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/dto"
	"github.com/mydisha/keipay/backend/internal/transport/http/middleware"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// IdempotentReplayedHeader marks a response that returns an existing payment.
const IdempotentReplayedHeader = "Idempotent-Replayed"

// PaymentHandler exposes app-facing payment endpoints.
type PaymentHandler struct {
	svc *service.PaymentService
}

// NewPaymentHandler constructs a PaymentHandler.
func NewPaymentHandler(svc *service.PaymentService) *PaymentHandler {
	return &PaymentHandler{svc: svc}
}

// Create handles POST /v1/payments. A replay of an earlier request answers
// 201 with the original payment and Idempotent-Replayed: true.
func (h *PaymentHandler) Create(c *gin.Context) {
	app, ok := middleware.AppFromContext(c)
	if !ok {
		response.Error(c, domain.ErrUnauthorized)
		return
	}

	var req dto.CreatePaymentRequest
	if err := bindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	p, replayed, err := h.svc.Create(c.Request.Context(), app, service.CreatePaymentInput{
		OrderID:              req.OrderID,
		Amount:               req.Amount,
		Currency:             req.Currency,
		Description:          req.Description,
		PayerEmail:           req.PayerEmail,
		InvoiceDuration:      req.InvoiceDuration,
		Items:                req.ToOrderItems(),
		Customer:             req.ToCustomer(),
		ExpectedSettlementAt: req.ExpectedSettlementAt,
		Metadata:             req.Metadata,
	}, c.GetHeader("Idempotency-Key"))
	if err != nil {
		response.Error(c, err)
		return
	}
	if replayed {
		c.Header(IdempotentReplayedHeader, "true")
	}
	response.JSON(c, http.StatusCreated, dto.PaymentResponseFromDomain(p))
}

// Get handles GET /v1/payments/{id}, by pay_ id or legacy integer id, scoped
// to the authenticated app.
func (h *PaymentHandler) Get(c *gin.Context) {
	app, ok := middleware.AppFromContext(c)
	if !ok {
		response.Error(c, domain.ErrUnauthorized)
		return
	}
	ref, err := paymentRef(c.Param("id"))
	if err != nil {
		response.Error(c, err)
		return
	}
	p, err := h.svc.Get(c.Request.Context(), app, ref)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.PaymentResponseFromDomain(p))
}

var legacyPaymentID = regexp.MustCompile(`^[0-9]{1,19}$`)

// paymentRef parses a payment path id: a pay_ id (lower case accepted) or a
// decimal integer. An integer too large for int64 matches no payment.
func paymentRef(raw string) (service.PaymentRef, error) {
	if legacyPaymentID.MatchString(raw) {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return service.PaymentRef{}, domain.ErrNotFound
		}
		return service.PaymentRef{ID: n}, nil
	}
	pub, err := id.ParseWithPrefix(raw, id.Payment)
	if err != nil {
		return service.PaymentRef{}, invalidRequest("invalid path parameter", domain.FieldError{Field: "id", Rule: "public_id"})
	}
	return service.PaymentRef{PublicID: pub}, nil
}
