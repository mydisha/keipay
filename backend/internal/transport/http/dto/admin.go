package dto

import (
	"encoding/json"
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

// LedgerEntry is a ledger entry with its app and the payment's public id.
type LedgerEntry struct {
	ID        int64                  `json:"id"`
	App       AppSummary             `json:"app"`
	PaymentID string                 `json:"payment_id"`
	Amount    int64                  `json:"amount"`
	XenditFee int64                  `json:"xendit_fee"`
	NetAmount int64                  `json:"net_amount"`
	Type      domain.LedgerEntryType `json:"type"`
	SettledAt *time.Time             `json:"settled_at,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
}

// LedgerSummary is the per-app ledger total.
type LedgerSummary struct {
	App         AppSummary `json:"app"`
	Count       int64      `json:"count"`
	GrossAmount int64      `json:"gross_amount"`
	TotalFee    int64      `json:"total_fee"`
	NetAmount   int64      `json:"net_amount"`
}

// LedgerListResponse is the response for GET /admin/ledger.
type LedgerListResponse struct {
	Entries []LedgerEntry   `json:"entries"`
	Summary []LedgerSummary `json:"summary"`
}

// LedgerListFromDomain converts ledger rows; both lists are [] when empty.
func LedgerListFromDomain(entries []domain.LedgerEntry, summary []domain.LedgerSummary) LedgerListResponse {
	out := LedgerListResponse{Entries: make([]LedgerEntry, 0, len(entries)), Summary: make([]LedgerSummary, 0, len(summary))}
	for _, e := range entries {
		out.Entries = append(out.Entries, LedgerEntry{
			ID: e.ID, App: AppSummaryFromDomain(e.App), PaymentID: e.PaymentPublicID, Amount: e.Amount,
			XenditFee: e.XenditFee, NetAmount: e.NetAmount, Type: e.Type, SettledAt: utcPtr(e.SettledAt),
			CreatedAt: e.CreatedAt.UTC(),
		})
	}
	for _, s := range summary {
		out.Summary = append(out.Summary, LedgerSummary{
			App: AppSummaryFromDomain(s.App), Count: s.Count, GrossAmount: s.GrossAmount,
			TotalFee: s.TotalFee, NetAmount: s.NetAmount,
		})
	}
	return out
}

// WebhookDelivery is an outbound delivery with its app and the payment's
// public id.
type WebhookDelivery struct {
	ID        int64                        `json:"id"`
	PaymentID string                       `json:"payment_id"`
	App       AppSummary                   `json:"app"`
	EventType string                       `json:"event_type"`
	Payload   json.RawMessage              `json:"payload"`
	Status    domain.WebhookDeliveryStatus `json:"status"`
	Attempts  int                          `json:"attempts"`
	LastError string                       `json:"last_error"`
	CreatedAt time.Time                    `json:"created_at"`
	UpdatedAt time.Time                    `json:"updated_at"`
}

// DeliveryListResponse is the response for GET /admin/webhook-deliveries.
type DeliveryListResponse struct {
	Deliveries []WebhookDelivery `json:"deliveries"`
}

// DeliveryListFromDomain converts deliveries; the list is [] when empty.
func DeliveryListFromDomain(ds []domain.WebhookDelivery) DeliveryListResponse {
	out := DeliveryListResponse{Deliveries: make([]WebhookDelivery, 0, len(ds))}
	for _, d := range ds {
		out.Deliveries = append(out.Deliveries, WebhookDelivery{
			ID: d.ID, PaymentID: d.PaymentPublicID, App: AppSummaryFromDomain(d.App), EventType: d.EventType,
			Payload: d.Payload, Status: d.Status, Attempts: d.Attempts, LastError: d.LastError,
			CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
		})
	}
	return out
}

// ReplayResponse is the response for POST /admin/webhook-deliveries/:id/replay.
type ReplayResponse struct {
	Requeued bool `json:"requeued"`
}

// MessageResponse is a generic message response.
type MessageResponse struct {
	Message string `json:"message"`
}

// ReceivedResponse is the standard webhook acknowledgment.
type ReceivedResponse struct {
	Received bool `json:"received"`
}
