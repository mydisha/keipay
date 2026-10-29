package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// DashboardRepo is a pgx-backed domain.DashboardRepository.
type DashboardRepo struct {
	db *pgxpool.Pool
}

// NewDashboardRepo constructs a DashboardRepo.
func NewDashboardRepo(db *pgxpool.Pool) *DashboardRepo { return &DashboardRepo{db: db} }

// successful is the status filter of a captured payment.
const successful = `p.status IN ('PAID', 'SETTLED')`

// windowFilters binds [from, to) on column and the optional environment of
// envColumn. The bounds are cast, or Postgres would infer them from the
// comparison and refuse a later interval expression.
func windowFilters(column, envColumn string, from, to any, env *domain.Environment) *filters {
	w := &filters{}
	w.add(column+" >= ?::timestamptz", from)
	w.add(column+" < ?::timestamptz", to)
	if env != nil {
		w.add(envColumn+" = ?", string(*env))
	}
	return w
}

// PaymentTotals counts both periods in one pass: the first bound is the
// previous period's start and a row is current when it was created on or
// after the current period's start.
func (r *DashboardRepo) PaymentTotals(ctx context.Context, w domain.DashboardWindow) ([]domain.DashboardPaymentTotalsRow, error) {
	f := windowFilters("p.created_at", "a.environment", w.PreviousFrom, w.To, w.Environment)
	q := `SELECT p.created_at >= ` + f.arg(w.From) + `::timestamptz AS current, p.currency, count(*),
			count(*) FILTER (WHERE p.status = 'PENDING'), count(*) FILTER (WHERE p.status = 'PAID'),
			count(*) FILTER (WHERE p.status = 'SETTLED'), count(*) FILTER (WHERE p.status = 'EXPIRED'),
			count(*) FILTER (WHERE p.status = 'FAILED'),
			COALESCE(sum(p.amount) FILTER (WHERE ` + successful + `), 0)::bigint
		FROM payments p JOIN apps a ON a.id = p.app_id` + f.where() + `
		GROUP BY 1, p.currency ORDER BY 1, p.currency`
	rows, err := conn(ctx, r.db).Query(ctx, q, f.args...)
	if err != nil {
		return nil, fmt.Errorf("count dashboard payments: %w", err)
	}
	defer rows.Close()
	out := []domain.DashboardPaymentTotalsRow{}
	for rows.Next() {
		var row domain.DashboardPaymentTotalsRow
		t := &row.Totals
		if err := rows.Scan(&row.Current, &row.Currency, &t.Created, &t.Pending, &t.Paid, &t.Settled, &t.Expired, &t.Failed, &t.Gross); err != nil {
			return nil, fmt.Errorf("scan dashboard payment totals: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("count dashboard payments: %w", err)
	}
	return out, nil
}

// PaymentSeries groups the current period's payments by WIB day and
// currency.
func (r *DashboardRepo) PaymentSeries(ctx context.Context, w domain.DashboardWindow) ([]domain.DashboardSeriesRow, error) {
	f := windowFilters("p.created_at", "a.environment", w.From, w.To, w.Environment)
	q := `SELECT date_trunc('day', p.created_at AT TIME ZONE 'Asia/Jakarta') AT TIME ZONE 'Asia/Jakarta' AS day_start,
			p.currency, count(*), count(*) FILTER (WHERE ` + successful + `),
			COALESCE(sum(p.amount) FILTER (WHERE ` + successful + `), 0)::bigint
		FROM payments p JOIN apps a ON a.id = p.app_id` + f.where() + `
		GROUP BY 1, p.currency ORDER BY 1, p.currency`
	rows, err := conn(ctx, r.db).Query(ctx, q, f.args...)
	if err != nil {
		return nil, fmt.Errorf("count dashboard payments per day: %w", err)
	}
	defer rows.Close()
	out := []domain.DashboardSeriesRow{}
	for rows.Next() {
		var row domain.DashboardSeriesRow
		if err := rows.Scan(&row.DayStart, &row.Currency, &row.Created, &row.Successful, &row.Gross); err != nil {
			return nil, fmt.Errorf("scan dashboard payment series: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("count dashboard payments per day: %w", err)
	}
	return out, nil
}

// DeliveryCounts counts the current period's deliveries by status.
func (r *DashboardRepo) DeliveryCounts(ctx context.Context, w domain.DashboardWindow) (domain.DashboardDeliveryCounts, error) {
	f := windowFilters("d.created_at", "a.environment", w.From, w.To, w.Environment)
	q := `SELECT count(*), count(*) FILTER (WHERE d.status = 'PENDING'), count(*) FILTER (WHERE d.status = 'DELIVERED'),
			count(*) FILTER (WHERE d.status = 'FAILED')
		FROM webhook_deliveries d JOIN apps a ON a.id = d.app_id` + f.where()
	var c domain.DashboardDeliveryCounts
	if err := conn(ctx, r.db).QueryRow(ctx, q, f.args...).Scan(&c.Total, &c.Pending, &c.Delivered, &c.Failed); err != nil {
		return domain.DashboardDeliveryCounts{}, fmt.Errorf("count dashboard deliveries: %w", err)
	}
	return c, nil
}

// InboundCounts counts the current period's callbacks by result.
func (r *DashboardRepo) InboundCounts(ctx context.Context, w domain.DashboardWindow) (map[domain.InboundResult]int64, error) {
	f := windowFilters("w.received_at", "c.environment", w.From, w.To, w.Environment)
	q := `SELECT w.result, count(*) FROM inbound_webhooks w JOIN connections c ON c.id = w.connection_id` +
		f.where() + ` GROUP BY w.result`
	rows, err := conn(ctx, r.db).Query(ctx, q, f.args...)
	if err != nil {
		return nil, fmt.Errorf("count dashboard inbound webhooks: %w", err)
	}
	defer rows.Close()
	out := map[domain.InboundResult]int64{}
	for rows.Next() {
		var result string
		var n int64
		if err := rows.Scan(&result, &n); err != nil {
			return nil, fmt.Errorf("scan dashboard inbound counts: %w", err)
		}
		out[domain.InboundResult(result)] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("count dashboard inbound webhooks: %w", err)
	}
	return out, nil
}

// UnresolvedReconciliationItems sums the unresolved counts the service
// keeps on succeeded runs.
func (r *DashboardRepo) UnresolvedReconciliationItems(ctx context.Context, env *domain.Environment) (int64, error) {
	f := &filters{}
	f.add("r.status = 'succeeded'")
	if env != nil {
		f.add("c.environment = ?", string(*env))
	}
	q := `SELECT COALESCE(sum(r.unresolved_count), 0)::bigint
		FROM reconciliation_runs r JOIN connections c ON c.id = r.connection_id` + f.where()
	var n int64
	if err := conn(ctx, r.db).QueryRow(ctx, q, f.args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("sum unresolved reconciliation items: %w", err)
	}
	return n, nil
}
