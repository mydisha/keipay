package domain

import (
	"context"
	"time"
)

// DashboardRange is how many days the console dashboard covers.
type DashboardRange string

// Dashboard ranges.
const (
	DashboardRange7d  DashboardRange = "7d"
	DashboardRange30d DashboardRange = "30d"
)

// OrDefault is r, or seven days when r is unset.
func (r DashboardRange) OrDefault() DashboardRange {
	if r == "" {
		return DashboardRange7d
	}
	return r
}

// Valid reports whether r is a known range.
func (r DashboardRange) Valid() bool { return r == DashboardRange7d || r == DashboardRange30d }

// Days is the length of r in days, 0 when r is not valid.
func (r DashboardRange) Days() int {
	switch r {
	case DashboardRange7d:
		return 7
	case DashboardRange30d:
		return 30
	default:
		return 0
	}
}

// wib is Asia/Jakarta, the zone dashboard days are cut in. Indonesia has no
// daylight saving time, so a fixed zone is exact.
var wib = time.FixedZone("WIB", 7*60*60)

// WindowAt is the window r covers at now: To is the next WIB midnight (so
// today counts), From is Days before it and PreviousFrom Days before that.
// Times are returned in UTC.
func (r DashboardRange) WindowAt(now time.Time) DashboardWindow {
	local := now.In(wib)
	to := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, wib)
	from := to.AddDate(0, 0, -r.Days())
	return DashboardWindow{PreviousFrom: from.AddDate(0, 0, -r.Days()).UTC(), From: from.UTC(), To: to.UTC()}
}

// DashboardQuery asks for the dashboard of one environment (nil: every
// environment) over one range.
type DashboardQuery struct {
	Environment *Environment
	Range       DashboardRange
}

// DashboardWindow is the period a dashboard covers, [From, To), and the
// period of the same length before it, [PreviousFrom, From). Environment
// narrows every count to the apps and connections of that environment; nil
// counts everything.
type DashboardWindow struct {
	Environment  *Environment
	PreviousFrom time.Time
	From         time.Time
	To           time.Time
}

// DashboardPaymentTotals counts the payments created in one period and one
// currency by status. Gross is the amount of the successful ones in that
// currency's minor unit.
type DashboardPaymentTotals struct {
	Created int64
	Pending int64
	Paid    int64
	Settled int64
	Expired int64
	Failed  int64
	Gross   int64
}

// Successful is how many payments were paid or settled.
func (t DashboardPaymentTotals) Successful() int64 { return t.Paid + t.Settled }

// Finished is how many payments reached an outcome: successful, expired or
// failed.
func (t DashboardPaymentTotals) Finished() int64 { return t.Successful() + t.Expired + t.Failed }

// SuccessRate is successful over finished payments, nil when none finished.
func (t DashboardPaymentTotals) SuccessRate() *float64 {
	return rate(t.Successful(), t.Finished())
}

// Plus adds the counts of o, for totals across currencies. Gross stays 0
// because amounts in different currencies do not add up.
func (t DashboardPaymentTotals) Plus(o DashboardPaymentTotals) DashboardPaymentTotals {
	return DashboardPaymentTotals{Created: t.Created + o.Created, Pending: t.Pending + o.Pending, Paid: t.Paid + o.Paid,
		Settled: t.Settled + o.Settled, Expired: t.Expired + o.Expired, Failed: t.Failed + o.Failed}
}

// DashboardPeriodTotals compares the current period with the previous one.
type DashboardPeriodTotals struct {
	Current  DashboardPaymentTotals
	Previous DashboardPaymentTotals
}

// rate is n over total, nil when total is 0.
func rate(n, total int64) *float64 {
	if total == 0 {
		return nil
	}
	v := float64(n) / float64(total)
	return &v
}

// DashboardPaymentTotalsRow totals one currency in the current period
// (Current true) or the previous one.
type DashboardPaymentTotalsRow struct {
	Current  bool
	Currency string
	Totals   DashboardPaymentTotals
}

// DashboardSeriesRow counts the payments of one currency created on one WIB
// day; DayStart is that day's midnight.
type DashboardSeriesRow struct {
	DayStart   time.Time
	Currency   string
	Created    int64
	Successful int64
	Gross      int64
}

// DashboardDeliveryCounts counts the webhook deliveries created in a period
// by status.
type DashboardDeliveryCounts struct {
	Total     int64
	Pending   int64
	Delivered int64
	Failed    int64
}

// Finished is how many deliveries reached an outcome: delivered or failed.
func (c DashboardDeliveryCounts) Finished() int64 { return c.Delivered + c.Failed }

// SuccessRate is delivered over finished deliveries, nil when none finished.
func (c DashboardDeliveryCounts) SuccessRate() *float64 {
	return rate(c.Delivered, c.Finished())
}

// DashboardInbound counts the gateway callbacks received in a period.
// Problems are the callbacks that were neither applied nor harmlessly
// ignored (stale, duplicate). ByResult has every result, 0 included.
type DashboardInbound struct {
	Total    int64
	Applied  int64
	Problems int64
	ByResult map[InboundResult]int64
}

// InboundResults returns every result in the order the console shows them.
func InboundResults() []InboundResult {
	return []InboundResult{InboundApplied, InboundStale, InboundDuplicate, InboundMismatch, InboundUnmapped,
		InboundInvalidSignature, InboundInvalidPayload, InboundError}
}

// NewDashboardInbound totals counts per result, listing every result.
func NewDashboardInbound(counts map[InboundResult]int64) DashboardInbound {
	results := InboundResults()
	in := DashboardInbound{ByResult: make(map[InboundResult]int64, len(results))}
	for _, r := range results {
		n := counts[r]
		in.ByResult[r] = n
		in.Total += n
		switch r {
		case InboundApplied:
			in.Applied += n
		case InboundStale, InboundDuplicate:
		default:
			in.Problems += n
		}
	}
	return in
}

// DashboardCurrencyTotals compares one currency's current period with the
// previous one.
type DashboardCurrencyTotals struct {
	Currency string
	Current  DashboardPaymentTotals
	Previous DashboardPaymentTotals
}

// DashboardCurrencyAmount is an amount in one currency's minor unit.
type DashboardCurrencyAmount struct {
	Currency string
	Amount   int64
}

// DashboardSeriesPoint is one WIB day of the current period: how many
// payments were created and succeeded, and the successful amount per
// currency.
type DashboardSeriesPoint struct {
	PeriodStart time.Time
	PeriodEnd   time.Time
	Created     int64
	Successful  int64
	Gross       []DashboardCurrencyAmount
}

// DashboardPayments is the payment part of the dashboard. Totals counts
// every currency together (its Gross is 0); ByCurrency is sorted by
// currency; Series has one point per day of the window.
type DashboardPayments struct {
	Totals     DashboardPeriodTotals
	ByCurrency []DashboardCurrencyTotals
	Series     []DashboardSeriesPoint
}

// DashboardReconciliation is what reconciliation still needs attention:
// the unresolved items of every succeeded run and the newest run, nil when
// there is none.
type DashboardReconciliation struct {
	UnresolvedItems int64
	LatestRun       *ReconciliationRun
}

// Dashboard is the console's overview of one environment over one range.
type Dashboard struct {
	Range          DashboardRange
	Window         DashboardWindow
	Payments       DashboardPayments
	Deliveries     DashboardDeliveryCounts
	Inbound        DashboardInbound
	Connections    []Connection
	Reconciliation DashboardReconciliation
	RecentPayments []Payment
}

// DashboardRepository counts what the dashboard shows. Every window filter
// is half-open on the row's creation time; an environment filter joins the
// owning app or connection.
type DashboardRepository interface {
	// PaymentTotals counts the payments created in [w.PreviousFrom, w.To)
	// per period and currency; rows with no payments are absent.
	PaymentTotals(ctx context.Context, w DashboardWindow) ([]DashboardPaymentTotalsRow, error)
	// PaymentSeries counts the payments created in [w.From, w.To) per WIB
	// day and currency, ordered by day; days with no payments are absent.
	PaymentSeries(ctx context.Context, w DashboardWindow) ([]DashboardSeriesRow, error)
	// DeliveryCounts counts the deliveries created in [w.From, w.To).
	DeliveryCounts(ctx context.Context, w DashboardWindow) (DashboardDeliveryCounts, error)
	// InboundCounts counts the callbacks received in [w.From, w.To) per
	// result; results with no callbacks are absent.
	InboundCounts(ctx context.Context, w DashboardWindow) (map[InboundResult]int64, error)
	// UnresolvedReconciliationItems sums the unresolved items of every
	// succeeded run of a connection in env (nil: every connection).
	UnresolvedReconciliationItems(ctx context.Context, env *Environment) (int64, error)
}
