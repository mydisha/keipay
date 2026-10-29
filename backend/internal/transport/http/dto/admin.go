package dto

import (
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// Payment is a payment as the admin API shows it.
type Payment struct {
	ID                   string               `json:"id"`
	Reference            string               `json:"reference"`
	App                  AppSummary           `json:"app"`
	OrderID              string               `json:"order_id"`
	XenditInvoiceID      string               `json:"xendit_invoice_id"`
	Amount               int64                `json:"amount"`
	Currency             string               `json:"currency"`
	Status               domain.PaymentStatus `json:"status"`
	Description          string               `json:"description"`
	InvoiceURL           string               `json:"invoice_url"`
	PayerEmail           string               `json:"payer_email"`
	Items                []domain.OrderItem   `json:"items"`
	Metadata             domain.Metadata      `json:"metadata"`
	ExpiresAt            *time.Time           `json:"expires_at"`
	PaidAt               *time.Time           `json:"paid_at"`
	SettledAt            *time.Time           `json:"settled_at"`
	ExpectedSettlementAt *time.Time           `json:"expected_settlement_at"`
	CreatedAt            time.Time            `json:"created_at"`
	UpdatedAt            time.Time            `json:"updated_at"`
}

// PaymentFromDomain converts a payment for the admin API; items is [] when
// the order had none.
func PaymentFromDomain(p *domain.Payment) Payment {
	items := p.Items
	if items == nil {
		items = []domain.OrderItem{}
	}
	return Payment{
		ID:                   p.PublicID,
		Reference:            p.Reference(),
		App:                  AppSummaryFromDomain(p.App),
		OrderID:              p.OrderID,
		XenditInvoiceID:      p.XenditInvoiceID,
		Amount:               p.Amount,
		Currency:             p.Currency,
		Status:               p.Status,
		Description:          p.Description,
		InvoiceURL:           p.InvoiceURL,
		PayerEmail:           p.PayerEmail,
		Items:                items,
		Metadata:             p.Metadata,
		ExpiresAt:            utcPtr(p.ExpiresAt),
		PaidAt:               utcPtr(p.PaidAt),
		SettledAt:            utcPtr(p.SettledAt),
		ExpectedSettlementAt: utcPtr(p.ExpectedSettlementAt),
		CreatedAt:            p.CreatedAt.UTC(),
		UpdatedAt:            p.UpdatedAt.UTC(),
	}
}

// PaymentListResponse is the response for GET /admin/payments.
type PaymentListResponse struct {
	Data       []Payment `json:"data"`
	NextCursor *string   `json:"next_cursor"`
}

// PaymentListFromDomain converts a page of payments.
func PaymentListFromDomain(page domain.PageResult[domain.Payment]) PaymentListResponse {
	out := PaymentListResponse{Data: make([]Payment, 0, len(page.Items)), NextCursor: nextCursor(page.NextCursor)}
	for i := range page.Items {
		out.Data = append(out.Data, PaymentFromDomain(&page.Items[i]))
	}
	return out
}

// MessageResponse is a generic message response.
type MessageResponse struct {
	Message string `json:"message"`
}

// ReceivedResponse is the standard webhook acknowledgment.
type ReceivedResponse struct {
	Received bool `json:"received"`
}
