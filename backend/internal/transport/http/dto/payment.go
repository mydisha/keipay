// Package dto contains all HTTP Data Transfer Objects (request/response structs).
//
// DTOs are the external contract between the API and its clients. They handle:
// - JSON serialization/deserialization
// - Input validation via struct tags
// - Transformation to/from domain entities
//
// Guidelines:
// - Keep DTOs separate from domain entities
// - Use struct tags for validation (binding) and JSON naming
// - Include transformation methods (ToDomain/FromDomain) when needed
package dto

import (
	"encoding/json"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// ============================================================================
// Payment DTOs
// ============================================================================

// OrderItemRequest represents an order item in a payment request.
type OrderItemRequest struct {
	Name        string `json:"name" binding:"required"`
	Quantity    int32  `json:"quantity" binding:"required,gt=0"`
	Price       int64  `json:"price" binding:"gte=0"`
	Category    string `json:"category"`
	ReferenceID string `json:"reference_id"`
	URL         string `json:"url"`
}

// CustomerRequest represents optional payer information.
type CustomerRequest struct {
	GivenNames   string `json:"given_names"`
	Surname      string `json:"surname"`
	Email        string `json:"email"`
	MobileNumber string `json:"mobile_number"`
}

// CreatePaymentRequest is the request body for POST /v1/payments.
type CreatePaymentRequest struct {
	OrderID              string             `json:"order_id" binding:"required"`
	Amount               int64              `json:"amount" binding:"required,gt=0"`
	Currency             string             `json:"currency"`
	Description          string             `json:"description"`
	PayerEmail           string             `json:"payer_email"`
	InvoiceDuration      int                `json:"invoice_duration"`
	Items                []OrderItemRequest `json:"items" binding:"omitempty,dive"`
	Customer             *CustomerRequest   `json:"customer"`
	ExpectedSettlementAt *time.Time         `json:"expected_settlement_at"`
	Metadata             json.RawMessage    `json:"metadata"`
}

// PaymentResponse is the response body for payment endpoints.
type PaymentResponse struct {
	ID                   string               `json:"id"`
	PaymentID            int64                `json:"payment_id"`
	Reference            string               `json:"reference"`
	OrderID              string               `json:"order_id"`
	Status               domain.PaymentStatus `json:"status"`
	Amount               int64                `json:"amount"`
	Currency             string               `json:"currency"`
	InvoiceURL           string               `json:"invoice_url"`
	Items                []domain.OrderItem   `json:"items,omitempty"`
	Metadata             domain.Metadata      `json:"metadata"`
	ExpiresAt            *time.Time           `json:"expires_at,omitempty"`
	PaidAt               *time.Time           `json:"paid_at,omitempty"`
	SettledAt            *time.Time           `json:"settled_at,omitempty"`
	ExpectedSettlementAt *time.Time           `json:"expected_settlement_at,omitempty"`
}

// ToOrderItems converts DTO order items to domain order items.
func (r *CreatePaymentRequest) ToOrderItems() []domain.OrderItem {
	if len(r.Items) == 0 {
		return nil
	}
	items := make([]domain.OrderItem, len(r.Items))
	for i, it := range r.Items {
		items[i] = domain.OrderItem{
			Name:        it.Name,
			Quantity:    it.Quantity,
			Price:       it.Price,
			Category:    it.Category,
			ReferenceID: it.ReferenceID,
			URL:         it.URL,
		}
	}
	return items
}

// ToCustomer converts DTO customer to domain customer.
func (r *CreatePaymentRequest) ToCustomer() *domain.Customer {
	if r.Customer == nil {
		return nil
	}
	return &domain.Customer{
		GivenNames:   r.Customer.GivenNames,
		Surname:      r.Customer.Surname,
		Email:        r.Customer.Email,
		MobileNumber: r.Customer.MobileNumber,
	}
}

// PaymentResponseFromDomain converts a domain Payment to PaymentResponse.
func PaymentResponseFromDomain(p *domain.Payment) PaymentResponse {
	return PaymentResponse{
		ID:                   p.PublicID,
		PaymentID:            p.ID,
		Reference:            p.Reference(),
		OrderID:              p.OrderID,
		Status:               p.Status,
		Amount:               p.Amount,
		Currency:             p.Currency,
		InvoiceURL:           p.InvoiceURL,
		Items:                p.Items,
		Metadata:             p.Metadata,
		ExpiresAt:            utcPtr(p.ExpiresAt),
		PaidAt:               utcPtr(p.PaidAt),
		SettledAt:            utcPtr(p.SettledAt),
		ExpectedSettlementAt: utcPtr(p.ExpectedSettlementAt),
	}
}
