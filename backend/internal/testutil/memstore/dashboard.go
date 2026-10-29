package memstore

import (
	"context"
	"maps"
	"sync"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// Dashboard is an in-memory domain.DashboardRepository that serves canned
// rows and records the windows it was asked for. Set Err to fail every
// call.
type Dashboard struct {
	mu sync.Mutex

	Totals     []domain.DashboardPaymentTotalsRow
	Series     []domain.DashboardSeriesRow
	Deliveries domain.DashboardDeliveryCounts
	Inbound    map[domain.InboundResult]int64
	Unresolved int64
	Err        error

	// Windows records the window of every count, in call order, and
	// Environments the environment of every unresolved-items call.
	Windows      []domain.DashboardWindow
	Environments []*domain.Environment
}

func (d *Dashboard) record(w domain.DashboardWindow) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Windows = append(d.Windows, w)
	return d.Err
}

// PaymentTotals returns Totals.
func (d *Dashboard) PaymentTotals(_ context.Context, w domain.DashboardWindow) ([]domain.DashboardPaymentTotalsRow, error) {
	if err := d.record(w); err != nil {
		return nil, err
	}
	return append([]domain.DashboardPaymentTotalsRow{}, d.Totals...), nil
}

// PaymentSeries returns Series.
func (d *Dashboard) PaymentSeries(_ context.Context, w domain.DashboardWindow) ([]domain.DashboardSeriesRow, error) {
	if err := d.record(w); err != nil {
		return nil, err
	}
	return append([]domain.DashboardSeriesRow{}, d.Series...), nil
}

// DeliveryCounts returns Deliveries.
func (d *Dashboard) DeliveryCounts(_ context.Context, w domain.DashboardWindow) (domain.DashboardDeliveryCounts, error) {
	if err := d.record(w); err != nil {
		return domain.DashboardDeliveryCounts{}, err
	}
	return d.Deliveries, nil
}

// InboundCounts returns a copy of Inbound.
func (d *Dashboard) InboundCounts(_ context.Context, w domain.DashboardWindow) (map[domain.InboundResult]int64, error) {
	if err := d.record(w); err != nil {
		return nil, err
	}
	return maps.Clone(d.Inbound), nil
}

// UnresolvedReconciliationItems returns Unresolved.
func (d *Dashboard) UnresolvedReconciliationItems(_ context.Context, env *domain.Environment) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Environments = append(d.Environments, env)
	if d.Err != nil {
		return 0, d.Err
	}
	return d.Unresolved, nil
}
