package dto

import (
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// DashboardPaymentTotals counts the payments created in one period and one
// currency. SuccessRate is null when no payment finished.
type DashboardPaymentTotals struct {
	Created     int64    `json:"created"`
	Pending     int64    `json:"pending"`
	Paid        int64    `json:"paid"`
	Settled     int64    `json:"settled"`
	Expired     int64    `json:"expired"`
	Failed      int64    `json:"failed"`
	Successful  int64    `json:"successful"`
	Gross       int64    `json:"gross"`
	SuccessRate *float64 `json:"success_rate"`
}

func dashboardTotalsFromDomain(t domain.DashboardPaymentTotals) DashboardPaymentTotals {
	return DashboardPaymentTotals{Created: t.Created, Pending: t.Pending, Paid: t.Paid, Settled: t.Settled,
		Expired: t.Expired, Failed: t.Failed, Successful: t.Successful(), Gross: t.Gross, SuccessRate: t.SuccessRate()}
}

// DashboardCurrencyTotals compares one currency's current period with the
// previous one.
type DashboardCurrencyTotals struct {
	Currency string                 `json:"currency"`
	Current  DashboardPaymentTotals `json:"current"`
	Previous DashboardPaymentTotals `json:"previous"`
}

// DashboardCurrencyAmount is an amount in one currency's minor unit.
type DashboardCurrencyAmount struct {
	Currency string `json:"currency"`
	Amount   int64  `json:"amount"`
}

// DashboardSeriesPoint is one day of the current period.
type DashboardSeriesPoint struct {
	PeriodStart time.Time                 `json:"period_start"`
	PeriodEnd   time.Time                 `json:"period_end"`
	Created     int64                     `json:"created"`
	Successful  int64                     `json:"successful"`
	Gross       []DashboardCurrencyAmount `json:"gross"`
}

// DashboardPayments is the payment part of the dashboard.
type DashboardPayments struct {
	ByCurrency []DashboardCurrencyTotals `json:"by_currency"`
	Series     []DashboardSeriesPoint    `json:"series"`
}

// DashboardDeliveries counts the deliveries created in the period.
// SuccessRate is null when none finished.
type DashboardDeliveries struct {
	Total       int64    `json:"total"`
	Pending     int64    `json:"pending"`
	Delivered   int64    `json:"delivered"`
	Failed      int64    `json:"failed"`
	SuccessRate *float64 `json:"success_rate"`
}

// DashboardInboundResults counts callbacks per result; every result is
// present.
type DashboardInboundResults struct {
	Applied          int64 `json:"applied"`
	Stale            int64 `json:"stale"`
	Duplicate        int64 `json:"duplicate"`
	Mismatch         int64 `json:"mismatch"`
	Unmapped         int64 `json:"unmapped"`
	InvalidSignature int64 `json:"invalid_signature"`
	InvalidPayload   int64 `json:"invalid_payload"`
	Error            int64 `json:"error"`
}

// DashboardInbound counts the callbacks received in the period.
type DashboardInbound struct {
	Total    int64                   `json:"total"`
	Applied  int64                   `json:"applied"`
	Problems int64                   `json:"problems"`
	ByResult DashboardInboundResults `json:"by_result"`
}

// DashboardReconciliation is what reconciliation still needs attention.
type DashboardReconciliation struct {
	UnresolvedItems int64              `json:"unresolved_items"`
	LatestRun       *ReconciliationRun `json:"latest_run"`
}

// DashboardResponse is the response for GET /admin/dashboard.
type DashboardResponse struct {
	Environment    *domain.Environment     `json:"environment"`
	Range          domain.DashboardRange   `json:"range"`
	From           time.Time               `json:"from"`
	To             time.Time               `json:"to"`
	PreviousFrom   time.Time               `json:"previous_from"`
	Payments       DashboardPayments       `json:"payments"`
	Deliveries     DashboardDeliveries     `json:"deliveries"`
	Inbound        DashboardInbound        `json:"inbound"`
	Connections    []ConnectionListItem    `json:"connections"`
	Reconciliation DashboardReconciliation `json:"reconciliation"`
	RecentPayments []Payment               `json:"recent_payments"`
}

// DashboardFromDomain converts a dashboard; every list is [] when empty and
// timestamps are UTC.
func DashboardFromDomain(d *domain.Dashboard) DashboardResponse {
	out := DashboardResponse{
		Environment: d.Environment, Range: d.Range,
		From: d.Window.From.UTC(), To: d.Window.To.UTC(), PreviousFrom: d.Window.PreviousFrom.UTC(),
		Payments: DashboardPayments{
			ByCurrency: make([]DashboardCurrencyTotals, 0, len(d.Payments.ByCurrency)),
			Series:     make([]DashboardSeriesPoint, 0, len(d.Payments.Series)),
		},
		Deliveries: DashboardDeliveries{Total: d.Deliveries.Total, Pending: d.Deliveries.Pending, Delivered: d.Deliveries.Delivered,
			Failed: d.Deliveries.Failed, SuccessRate: d.Deliveries.SuccessRate()},
		Inbound: DashboardInbound{Total: d.Inbound.Total, Applied: d.Inbound.Applied, Problems: d.Inbound.Problems,
			ByResult: DashboardInboundResults{
				Applied: d.Inbound.ByResult[domain.InboundApplied], Stale: d.Inbound.ByResult[domain.InboundStale],
				Duplicate: d.Inbound.ByResult[domain.InboundDuplicate], Mismatch: d.Inbound.ByResult[domain.InboundMismatch],
				Unmapped: d.Inbound.ByResult[domain.InboundUnmapped], InvalidSignature: d.Inbound.ByResult[domain.InboundInvalidSignature],
				InvalidPayload: d.Inbound.ByResult[domain.InboundInvalidPayload], Error: d.Inbound.ByResult[domain.InboundError],
			}},
		Connections:    make([]ConnectionListItem, 0, len(d.Connections)),
		Reconciliation: DashboardReconciliation{UnresolvedItems: d.Reconciliation.UnresolvedItems},
		RecentPayments: make([]Payment, 0, len(d.RecentPayments)),
	}
	for _, t := range d.Payments.ByCurrency {
		out.Payments.ByCurrency = append(out.Payments.ByCurrency, DashboardCurrencyTotals{Currency: t.Currency,
			Current: dashboardTotalsFromDomain(t.Current), Previous: dashboardTotalsFromDomain(t.Previous)})
	}
	for _, p := range d.Payments.Series {
		point := DashboardSeriesPoint{PeriodStart: p.PeriodStart.UTC(), PeriodEnd: p.PeriodEnd.UTC(), Created: p.Created,
			Successful: p.Successful, Gross: make([]DashboardCurrencyAmount, 0, len(p.Gross))}
		for _, g := range p.Gross {
			point.Gross = append(point.Gross, DashboardCurrencyAmount{Currency: g.Currency, Amount: g.Amount})
		}
		out.Payments.Series = append(out.Payments.Series, point)
	}
	for i := range d.Connections {
		out.Connections = append(out.Connections, ConnectionListItemFromDomain(&d.Connections[i]))
	}
	if d.Reconciliation.LatestRun != nil {
		run := ReconciliationRunFromDomain(d.Reconciliation.LatestRun)
		out.Reconciliation.LatestRun = &run
	}
	for i := range d.RecentPayments {
		out.RecentPayments = append(out.RecentPayments, PaymentFromDomain(&d.RecentPayments[i]))
	}
	return out
}
