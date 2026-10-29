package domain

import (
	"context"
	"time"
)

// PaymentReport is the report of one successful payment: what was paid, the
// gateway fee and the net amount, in the currency's minor unit. Reports are
// identified by their payment.
type PaymentReport struct {
	PaymentID       int64
	PaymentPublicID string
	Reference       string
	OrderID         string
	AppID           int64
	App             AppSummary
	// ConnectionID and Connection are nil for payments made before
	// connections existed.
	ConnectionID *int64
	Connection   *ConnectionSummary
	Currency     string
	Gross        int64
	// Fee and Net are nil when the gateway did not report a fee.
	Fee       *int64
	Net       *int64
	Status    PaymentStatus
	PaidAt    time.Time
	SettledAt *time.Time
}

// Provider is the provider of the report's connection, "" when it has none.
func (r *PaymentReport) Provider() string {
	if r.Connection == nil {
		return ""
	}
	return r.Connection.Provider
}

// PaymentReportFilter narrows report lists and summaries; nil fields match
// everything. From is inclusive and To exclusive, both on paid_at. Ids are
// canonical public ids.
type PaymentReportFilter struct {
	AppPublicID        *string
	ConnectionPublicID *string
	Environment        *Environment
	Currency           *string
	From               *time.Time
	To                 *time.Time
}

// ReportInterval is the period a payment report summary groups by.
type ReportInterval string

// Report intervals. Days and months start at midnight Asia/Jakarta.
const (
	ReportIntervalTotal ReportInterval = "total"
	ReportIntervalDay   ReportInterval = "day"
	ReportIntervalMonth ReportInterval = "month"
)

// OrTotal is i, or the total interval when i is unset.
func (i ReportInterval) OrTotal() ReportInterval {
	if i == "" {
		return ReportIntervalTotal
	}
	return i
}

// Valid reports whether i is a known interval.
func (i ReportInterval) Valid() bool {
	return i == ReportIntervalTotal || i == ReportIntervalDay || i == ReportIntervalMonth
}

// PaymentReportSummaryRow totals the reports of one period, app, connection
// and currency. Fee is the sum of the known fees and Net counts a report
// with an unknown fee with its full gross.
type PaymentReportSummaryRow struct {
	PeriodStart     time.Time
	PeriodEnd       time.Time
	App             AppSummary
	Connection      *ConnectionSummary
	Currency        string
	Count           int64
	Gross           int64
	Fee             int64
	Net             int64
	FeeUnknownCount int64
}

// PaymentReportRepository persists payment reports. Every method joins the
// transaction carried by ctx; reads fill App and Connection.
type PaymentReportRepository interface {
	// Upsert stores the report of r.PaymentID. An existing report only moves
	// from PAID to SETTLED, keeps its first settled_at, and takes a fee only
	// while its own is unknown.
	Upsert(ctx context.Context, r *PaymentReport) error
	// List returns reports newest first (paid_at, then payment public id).
	List(ctx context.Context, f PaymentReportFilter, p Page) (PageResult[PaymentReport], error)
	// Summary totals the reports with From <= paid_at < To, which are both
	// set, per period of interval, app, connection and currency.
	Summary(ctx context.Context, f PaymentReportFilter, interval ReportInterval) ([]PaymentReportSummaryRow, error)
	// Stream calls fn with every report matching f in List's order, one row
	// at a time; an error from fn stops it and is returned.
	Stream(ctx context.Context, f PaymentReportFilter, fn func(*PaymentReport) error) error
}
