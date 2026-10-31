package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// recentPaymentsLimit is how many of the newest payments the dashboard lists.
const recentPaymentsLimit = 8

// DashboardDeps are the ports DashboardService uses. Now is the clock the
// window is cut from; nil uses time.Now.
type DashboardDeps struct {
	Dashboard   domain.DashboardRepository
	Connections domain.ConnectionRepository
	Payments    domain.PaymentRepository
	Runs        domain.ReconciliationRepository
	Now         func() time.Time
}

// DashboardService assembles the console dashboard.
type DashboardService struct {
	deps DashboardDeps
}

// NewDashboardService constructs a DashboardService.
func NewDashboardService(d DashboardDeps) *DashboardService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &DashboardService{deps: d}
}

// Get builds the dashboard of q. An unknown range is an invalid_request on
// field range.
func (s *DashboardService) Get(ctx context.Context, q domain.DashboardQuery) (*domain.Dashboard, error) {
	r := q.Range.OrDefault()
	if !r.Valid() {
		return nil, invalidQuery(domain.FieldError{Field: "range", Rule: RuleOneOf})
	}
	w := r.WindowAt(s.deps.Now())
	w.Environment = q.Environment
	d := &domain.Dashboard{Range: r, Window: w}

	totals, err := s.deps.Dashboard.PaymentTotals(ctx, w)
	if err != nil {
		return nil, fmt.Errorf("dashboard payment totals: %w", err)
	}
	d.Payments.ByCurrency = foldTotals(totals)
	d.Payments.Totals = sumTotals(d.Payments.ByCurrency)
	series, err := s.deps.Dashboard.PaymentSeries(ctx, w)
	if err != nil {
		return nil, fmt.Errorf("dashboard payment series: %w", err)
	}
	d.Payments.Series = foldSeries(w, r.Days(), series)
	if d.Deliveries, err = s.deps.Dashboard.DeliveryCounts(ctx, w); err != nil {
		return nil, fmt.Errorf("dashboard delivery counts: %w", err)
	}
	inbound, err := s.deps.Dashboard.InboundCounts(ctx, w)
	if err != nil {
		return nil, fmt.Errorf("dashboard inbound counts: %w", err)
	}
	d.Inbound = domain.NewDashboardInbound(inbound)
	if d.Reconciliation.UnresolvedItems, err = s.deps.Dashboard.UnresolvedReconciliationItems(ctx, q.Environment); err != nil {
		return nil, fmt.Errorf("dashboard unresolved items: %w", err)
	}
	runs, err := s.deps.Runs.ListRuns(ctx, domain.ReconciliationRunFilter{Environment: q.Environment}, domain.Page{Limit: 1})
	if err != nil {
		return nil, fmt.Errorf("dashboard latest run: %w", err)
	}
	if len(runs.Items) > 0 {
		d.Reconciliation.LatestRun = &runs.Items[0]
	}
	conns, err := s.deps.Connections.List(ctx, domain.ConnectionFilter{Environment: q.Environment}, domain.Page{Limit: domain.MaxPageLimit})
	if err != nil {
		return nil, fmt.Errorf("dashboard connections: %w", err)
	}
	d.Connections = sortConnections(conns.Items)
	recent, err := s.deps.Payments.List(ctx, domain.PaymentFilter{Environment: q.Environment}, domain.Page{Limit: recentPaymentsLimit})
	if err != nil {
		return nil, fmt.Errorf("dashboard recent payments: %w", err)
	}
	d.RecentPayments = recent.Items
	return d, nil
}

// foldTotals pairs the current and previous period of each currency, sorted
// by currency. A currency seen in one period only has zero totals in the
// other.
func foldTotals(rows []domain.DashboardPaymentTotalsRow) []domain.DashboardCurrencyTotals {
	byCurrency := map[string]*domain.DashboardCurrencyTotals{}
	for _, r := range rows {
		t, ok := byCurrency[r.Currency]
		if !ok {
			t = &domain.DashboardCurrencyTotals{Currency: r.Currency}
			byCurrency[r.Currency] = t
		}
		if r.Current {
			t.Current = r.Totals
		} else {
			t.Previous = r.Totals
		}
	}
	out := make([]domain.DashboardCurrencyTotals, 0, len(byCurrency))
	for _, t := range byCurrency {
		out = append(out, *t)
	}
	slices.SortFunc(out, func(a, b domain.DashboardCurrencyTotals) int { return strings.Compare(a.Currency, b.Currency) })
	return out
}

// sumTotals adds the counts of every currency, per period.
func sumTotals(byCurrency []domain.DashboardCurrencyTotals) domain.DashboardPeriodTotals {
	var sum domain.DashboardPeriodTotals
	for _, t := range byCurrency {
		sum.Current = sum.Current.Plus(t.Current)
		sum.Previous = sum.Previous.Plus(t.Previous)
	}
	return sum
}

// foldSeries spreads rows over one point per day of the window, so days
// without payments are zero. Every point lists the same currencies, those
// seen anywhere in the window, so a chart can pick one across the series.
func foldSeries(w domain.DashboardWindow, days int, rows []domain.DashboardSeriesRow) []domain.DashboardSeriesPoint {
	points := make([]domain.DashboardSeriesPoint, days)
	gross := make([]map[string]int64, days)
	for i := range points {
		points[i] = domain.DashboardSeriesPoint{PeriodStart: w.From.AddDate(0, 0, i), PeriodEnd: w.From.AddDate(0, 0, i+1)}
		gross[i] = map[string]int64{}
	}
	var currencies []string
	for _, r := range rows {
		i := dayIndex(w.From, r.DayStart)
		if i < 0 || i >= days {
			continue
		}
		points[i].Created += r.Created
		points[i].Successful += r.Successful
		gross[i][r.Currency] += r.Gross
		if !slices.Contains(currencies, r.Currency) {
			currencies = append(currencies, r.Currency)
		}
	}
	slices.Sort(currencies)
	for i := range points {
		points[i].Gross = make([]domain.DashboardCurrencyAmount, 0, len(currencies))
		for _, c := range currencies {
			points[i].Gross = append(points[i].Gross, domain.DashboardCurrencyAmount{Currency: c, Amount: gross[i][c]})
		}
	}
	return points
}

// dayIndex is how many whole days dayStart is after from; a day before
// from is negative.
func dayIndex(from, dayStart time.Time) int {
	d := dayStart.Sub(from)
	if d < 0 {
		return -1
	}
	return int(d / (24 * time.Hour))
}

// sortConnections puts active connections first, then orders by name and
// public id so the list is stable.
func sortConnections(conns []domain.Connection) []domain.Connection {
	out := slices.Clone(conns)
	slices.SortStableFunc(out, func(a, b domain.Connection) int {
		if (a.Status == domain.ConnectionActive) != (b.Status == domain.ConnectionActive) {
			if a.Status == domain.ConnectionActive {
				return -1
			}
			return 1
		}
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return strings.Compare(a.PublicID, b.PublicID)
	})
	return out
}
