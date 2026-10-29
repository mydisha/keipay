package dto

import (
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// PaymentReport is the report of one successful payment.
type PaymentReport struct {
	PaymentID  string               `json:"payment_id"`
	Reference  string               `json:"reference"`
	OrderID    string               `json:"order_id"`
	App        AppSummary           `json:"app"`
	Connection *ConnectionSummary   `json:"connection"`
	Provider   *string              `json:"provider"`
	Currency   string               `json:"currency"`
	Gross      int64                `json:"gross"`
	Fee        *int64               `json:"fee"`
	Net        *int64               `json:"net"`
	Status     domain.PaymentStatus `json:"status"`
	PaidAt     time.Time            `json:"paid_at"`
	SettledAt  *time.Time           `json:"settled_at"`
}

// PaymentReportFromDomain converts a report; provider is null when the
// payment has no connection.
func PaymentReportFromDomain(r *domain.PaymentReport) PaymentReport {
	out := PaymentReport{
		PaymentID: r.PaymentPublicID, Reference: r.Reference, OrderID: r.OrderID, App: AppSummaryFromDomain(r.App),
		Connection: ConnectionSummaryFromDomain(r.Connection), Currency: r.Currency, Gross: r.Gross, Fee: r.Fee, Net: r.Net,
		Status: r.Status, PaidAt: r.PaidAt.UTC(), SettledAt: utcPtr(r.SettledAt),
	}
	if p := r.Provider(); p != "" {
		out.Provider = &p
	}
	return out
}

// PaymentReportListResponse is the response for GET /admin/payment-reports.
type PaymentReportListResponse struct {
	Data       []PaymentReport `json:"data"`
	NextCursor *string         `json:"next_cursor"`
}

// PaymentReportListFromDomain converts a page of reports.
func PaymentReportListFromDomain(page domain.PageResult[domain.PaymentReport]) PaymentReportListResponse {
	out := PaymentReportListResponse{Data: make([]PaymentReport, 0, len(page.Items)), NextCursor: nextCursor(page.NextCursor)}
	for i := range page.Items {
		out.Data = append(out.Data, PaymentReportFromDomain(&page.Items[i]))
	}
	return out
}

// PaymentReportSummaryRow totals one period, app, connection and currency.
type PaymentReportSummaryRow struct {
	PeriodStart     time.Time          `json:"period_start"`
	PeriodEnd       time.Time          `json:"period_end"`
	App             AppSummary         `json:"app"`
	Connection      *ConnectionSummary `json:"connection"`
	Currency        string             `json:"currency"`
	Count           int64              `json:"count"`
	Gross           int64              `json:"gross"`
	Fee             int64              `json:"fee"`
	Net             int64              `json:"net"`
	FeeUnknownCount int64              `json:"fee_unknown_count"`
}

// PaymentReportSummaryResponse is the response for
// GET /admin/payment-reports/summary.
type PaymentReportSummaryResponse struct {
	Interval domain.ReportInterval     `json:"interval"`
	From     time.Time                 `json:"from"`
	To       time.Time                 `json:"to"`
	Data     []PaymentReportSummaryRow `json:"data"`
}

// PaymentReportSummaryFromDomain converts the totals; data is [] when empty.
func PaymentReportSummaryFromDomain(interval domain.ReportInterval, from, to time.Time, rows []domain.PaymentReportSummaryRow) PaymentReportSummaryResponse {
	out := PaymentReportSummaryResponse{Interval: interval, From: from.UTC(), To: to.UTC(),
		Data: make([]PaymentReportSummaryRow, 0, len(rows))}
	for i := range rows {
		r := &rows[i]
		out.Data = append(out.Data, PaymentReportSummaryRow{
			PeriodStart: r.PeriodStart.UTC(), PeriodEnd: r.PeriodEnd.UTC(), App: AppSummaryFromDomain(r.App),
			Connection: ConnectionSummaryFromDomain(r.Connection), Currency: r.Currency, Count: r.Count, Gross: r.Gross,
			Fee: r.Fee, Net: r.Net, FeeUnknownCount: r.FeeUnknownCount,
		})
	}
	return out
}
