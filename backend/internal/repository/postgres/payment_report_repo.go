package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// PaymentReportRepo is a pgx-backed domain.PaymentReportRepository.
type PaymentReportRepo struct {
	db *pgxpool.Pool
}

// NewPaymentReportRepo constructs a PaymentReportRepo.
func NewPaymentReportRepo(db *pgxpool.Pool) *PaymentReportRepo { return &PaymentReportRepo{db: db} }

// reportSelect joins the payment, app and connection a report row shows, so
// no list loads them one by one.
const reportSelect = `SELECT r.payment_id, p.public_id, p.external_id, p.order_id,
	r.app_id, a.public_id, a.slug, a.name, a.environment,
	r.connection_id, c.public_id, c.name, c.provider, c.environment, c.status,
	r.currency, r.gross, r.fee, r.net, r.status, r.paid_at, r.settled_at
	FROM payment_reports r
	JOIN payments p ON p.id = r.payment_id
	JOIN apps a ON a.id = r.app_id
	LEFT JOIN connections c ON c.id = r.connection_id`

func scanReport(row pgx.Row) (*domain.PaymentReport, error) {
	var r domain.PaymentReport
	var appEnv, status string
	var conn connectionSummaryRow
	err := row.Scan(&r.PaymentID, &r.PaymentPublicID, &r.Reference, &r.OrderID,
		&r.AppID, &r.App.PublicID, &r.App.Slug, &r.App.Name, &appEnv,
		&r.ConnectionID, &conn.publicID, &conn.name, &conn.provider, &conn.environment, &conn.status,
		&r.Currency, &r.Gross, &r.Fee, &r.Net, &status, &r.PaidAt, &r.SettledAt)
	if err != nil {
		return nil, fmt.Errorf("scan payment report: %w", err)
	}
	r.App.Environment = domain.Environment(appEnv)
	r.Status = domain.PaymentStatus(status)
	r.Connection = conn.summary()
	return &r, nil
}

// connectionSummaryRow scans the columns of a LEFT JOINed connection.
type connectionSummaryRow struct {
	publicID, name, provider, environment, status *string
}

// summary is nil when the join found no connection.
func (c connectionSummaryRow) summary() *domain.ConnectionSummary {
	if c.publicID == nil {
		return nil
	}
	return &domain.ConnectionSummary{PublicID: *c.publicID, Name: *c.name, Provider: *c.provider,
		Environment: domain.Environment(*c.environment), Status: domain.ConnectionStatus(*c.status)}
}

// Upsert inserts the report of r.PaymentID or merges r into it. The first
// gross is kept, so a fee filled in later is netted against that gross, not
// the callback's, or payment_reports_net_check would reject the row. The
// WHERE clause skips a merge that would change nothing, so a repeated
// callback does not even touch updated_at.
func (r *PaymentReportRepo) Upsert(ctx context.Context, rep *domain.PaymentReport) error {
	const q = `INSERT INTO payment_reports (payment_id, app_id, connection_id, currency, gross, fee, net, status, paid_at, settled_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (payment_id) DO UPDATE SET
			status = CASE WHEN EXCLUDED.status = 'SETTLED' THEN 'SETTLED' ELSE payment_reports.status END,
			settled_at = COALESCE(payment_reports.settled_at, EXCLUDED.settled_at),
			fee = COALESCE(payment_reports.fee, EXCLUDED.fee),
			net = CASE WHEN payment_reports.fee IS NULL AND EXCLUDED.fee IS NOT NULL
				THEN payment_reports.gross - EXCLUDED.fee ELSE payment_reports.net END,
			updated_at = now()
		WHERE (EXCLUDED.status = 'SETTLED' AND payment_reports.status <> 'SETTLED')
			OR (payment_reports.settled_at IS NULL AND EXCLUDED.settled_at IS NOT NULL)
			OR (payment_reports.fee IS NULL AND EXCLUDED.fee IS NOT NULL)`
	_, err := conn(ctx, r.db).Exec(ctx, q, rep.PaymentID, rep.AppID, rep.ConnectionID, rep.Currency, rep.Gross,
		rep.Fee, rep.Net, string(rep.Status), rep.PaidAt, rep.SettledAt)
	if err != nil {
		return fmt.Errorf("upsert payment report: %w", err)
	}
	return nil
}

// FillFee only writes a report whose fee is still NULL, so a fee a callback
// stored meanwhile is never overwritten.
func (r *PaymentReportRepo) FillFee(ctx context.Context, paymentID, fee int64) (bool, error) {
	const q = `UPDATE payment_reports SET fee = $2, net = gross - $2, updated_at = now()
		WHERE payment_id = $1 AND fee IS NULL`
	ct, err := conn(ctx, r.db).Exec(ctx, q, paymentID, fee)
	if err != nil {
		return false, fmt.Errorf("fill payment report fee: %w", err)
	}
	return ct.RowsAffected() == 1, nil
}

// reportFilters builds the WHERE clause of f.
func reportFilters(f domain.PaymentReportFilter) *filters {
	w := &filters{}
	addReportFilters(w, f)
	return w
}

// addReportFilters adds the conditions of f to w. Id filters resolve through
// the unique public ids, so the per-app and per-connection indexes serve
// them.
func addReportFilters(w *filters, f domain.PaymentReportFilter) {
	if f.AppPublicID != nil {
		w.add("r.app_id = (SELECT id FROM apps WHERE public_id = ?)", *f.AppPublicID)
	}
	if f.ConnectionPublicID != nil {
		w.add("r.connection_id = (SELECT id FROM connections WHERE public_id = ?)", *f.ConnectionPublicID)
	}
	if f.Environment != nil {
		w.add("a.environment = ?", string(*f.Environment))
	}
	if f.Currency != nil {
		w.add("r.currency = ?", *f.Currency)
	}
	if f.From != nil {
		w.add("r.paid_at >= ?", *f.From)
	}
	if f.To != nil {
		w.add("r.paid_at < ?", *f.To)
	}
}

const reportOrder = ` ORDER BY r.paid_at DESC, p.public_id DESC`

// List returns one page of reports using a keyset on (paid_at, payment
// public id).
func (r *PaymentReportRepo) List(ctx context.Context, f domain.PaymentReportFilter, p domain.Page) (domain.PageResult[domain.PaymentReport], error) {
	w := reportFilters(f)
	if p.After != nil {
		w.add("(r.paid_at, p.public_id) < (?, ?)", p.After.Time, p.After.ID)
	}
	q := reportSelect + w.where() + reportOrder + ` LIMIT ` + w.arg(p.Limit+1)
	items := make([]domain.PaymentReport, 0, p.Limit+1)
	err := r.each(ctx, q, w.args, func(rep *domain.PaymentReport) error {
		items = append(items, *rep)
		return nil
	})
	if err != nil {
		return domain.PageResult[domain.PaymentReport]{}, fmt.Errorf("list payment reports: %w", err)
	}
	return keysetPage(items, p.Limit, func(rep domain.PaymentReport) domain.Cursor {
		return domain.Cursor{Time: rep.PaidAt, ID: rep.PaymentPublicID}
	}), nil
}

// Stream calls fn with every matching report, newest first.
func (r *PaymentReportRepo) Stream(ctx context.Context, f domain.PaymentReportFilter, fn func(*domain.PaymentReport) error) error {
	w := reportFilters(f)
	return r.each(ctx, reportSelect+w.where()+reportOrder, w.args, fn)
}

// each runs q and calls fn row by row, never holding more than one row.
func (r *PaymentReportRepo) each(ctx context.Context, q string, args []any, fn func(*domain.PaymentReport) error) error {
	rows, err := conn(ctx, r.db).Query(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("query payment reports: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		rep, err := scanReport(rows)
		if err != nil {
			return err
		}
		if err := fn(rep); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("query payment reports: %w", err)
	}
	return nil
}

// summaryPeriods are the period bounds per interval. Days and months are cut
// at midnight Asia/Jakarta and clipped to the requested range; $1 and $2 are
// its from and to.
var summaryPeriods = map[domain.ReportInterval][2]string{
	domain.ReportIntervalTotal: {`$1::timestamptz`, `$2::timestamptz`},
	domain.ReportIntervalDay: {
		`GREATEST(date_trunc('day', r.paid_at AT TIME ZONE 'Asia/Jakarta') AT TIME ZONE 'Asia/Jakarta', $1::timestamptz)`,
		`LEAST((date_trunc('day', r.paid_at AT TIME ZONE 'Asia/Jakarta') + interval '1 day') AT TIME ZONE 'Asia/Jakarta', $2::timestamptz)`,
	},
	domain.ReportIntervalMonth: {
		`GREATEST(date_trunc('month', r.paid_at AT TIME ZONE 'Asia/Jakarta') AT TIME ZONE 'Asia/Jakarta', $1::timestamptz)`,
		`LEAST((date_trunc('month', r.paid_at AT TIME ZONE 'Asia/Jakarta') + interval '1 month') AT TIME ZONE 'Asia/Jakarta', $2::timestamptz)`,
	},
}

// Summary totals the reports in one query: aggregate per period, app,
// connection and currency first, then join the names of the groups.
func (r *PaymentReportRepo) Summary(ctx context.Context, f domain.PaymentReportFilter, interval domain.ReportInterval) ([]domain.PaymentReportSummaryRow, error) {
	period, ok := summaryPeriods[interval]
	if !ok || f.From == nil || f.To == nil {
		return nil, fmt.Errorf("summarize payment reports: %w: interval and range required", domain.ErrInvalidInput)
	}
	// $1 and $2 are the range, which the period bounds use too; the other
	// filters bind from $3 on.
	w := &filters{args: []any{*f.From, *f.To}}
	w.add("r.paid_at >= $1::timestamptz")
	w.add("r.paid_at < $2::timestamptz")
	addReportFilters(w, domain.PaymentReportFilter{AppPublicID: f.AppPublicID, ConnectionPublicID: f.ConnectionPublicID,
		Environment: f.Environment, Currency: f.Currency})
	q := `WITH totals AS (
		SELECT ` + period[0] + ` AS period_start, ` + period[1] + ` AS period_end,
			r.app_id, r.connection_id, r.currency, count(*) AS n, sum(r.gross)::bigint AS gross,
			COALESCE(sum(r.fee), 0)::bigint AS fee, sum(COALESCE(r.net, r.gross))::bigint AS net,
			count(*) FILTER (WHERE r.fee IS NULL) AS fee_unknown
		FROM payment_reports r JOIN apps a ON a.id = r.app_id` + w.where() + `
		GROUP BY 1, 2, r.app_id, r.connection_id, r.currency)
	SELECT t.period_start, t.period_end, a.public_id, a.slug, a.name, a.environment,
		c.public_id, c.name, c.provider, c.environment, c.status,
		t.currency, t.n, t.gross, t.fee, t.net, t.fee_unknown
	FROM totals t
	JOIN apps a ON a.id = t.app_id
	LEFT JOIN connections c ON c.id = t.connection_id
	ORDER BY t.period_start, a.slug, a.environment, a.public_id, c.name NULLS LAST, c.public_id, t.currency`
	rows, err := conn(ctx, r.db).Query(ctx, q, w.args...)
	if err != nil {
		return nil, fmt.Errorf("summarize payment reports: %w", err)
	}
	defer rows.Close()
	out := []domain.PaymentReportSummaryRow{}
	for rows.Next() {
		var s domain.PaymentReportSummaryRow
		var appEnv string
		var c connectionSummaryRow
		if err := rows.Scan(&s.PeriodStart, &s.PeriodEnd, &s.App.PublicID, &s.App.Slug, &s.App.Name, &appEnv,
			&c.publicID, &c.name, &c.provider, &c.environment, &c.status,
			&s.Currency, &s.Count, &s.Gross, &s.Fee, &s.Net, &s.FeeUnknownCount); err != nil {
			return nil, fmt.Errorf("scan payment report summary: %w", err)
		}
		s.App.Environment = domain.Environment(appEnv)
		s.Connection = c.summary()
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("summarize payment reports: %w", err)
	}
	return out, nil
}
