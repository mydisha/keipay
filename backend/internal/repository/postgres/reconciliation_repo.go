package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// itemBatch bounds the rows one statement writes or the references one
// lookup sends.
const itemBatch = 1000

// ReconciliationRepo is a pgx-backed domain.ReconciliationRepository.
type ReconciliationRepo struct {
	db *pgxpool.Pool
}

// NewReconciliationRepo constructs a ReconciliationRepo.
func NewReconciliationRepo(db *pgxpool.Pool) *ReconciliationRepo { return &ReconciliationRepo{db: db} }

const runSelect = `SELECT r.id, r.public_id, r.connection_id, c.public_id, c.name, c.provider, c.environment, c.status,
	r.range_start, r.range_end, r.status, r.run_count, r.summary, r.unresolved_count, r.error_code, r.error_message,
	r.created_by, u.public_id, u.name, r.created_at, r.updated_at, r.started_at, r.finished_at
	FROM reconciliation_runs r
	JOIN connections c ON c.id = r.connection_id
	LEFT JOIN users u ON u.id = r.created_by`

func scanRun(row pgx.Row) (*domain.ReconciliationRun, error) {
	var r domain.ReconciliationRun
	var connEnv, connStatus, status string
	var summary []byte
	var errCode, errMessage, userID, userName *string
	err := row.Scan(&r.ID, &r.PublicID, &r.ConnectionID, &r.Connection.PublicID, &r.Connection.Name, &r.Connection.Provider,
		&connEnv, &connStatus, &r.From, &r.To, &status, &r.RunCount, &summary, &r.UnresolvedCount, &errCode, &errMessage,
		&r.CreatedByID, &userID, &userName, &r.CreatedAt, &r.UpdatedAt, &r.StartedAt, &r.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan reconciliation run: %w", err)
	}
	r.Connection.Environment = domain.Environment(connEnv)
	r.Connection.Status = domain.ConnectionStatus(connStatus)
	r.Status = domain.ReconciliationRunStatus(status)
	if summary != nil {
		r.Summary = &domain.ReconciliationSummary{}
		if err := json.Unmarshal(summary, r.Summary); err != nil {
			return nil, fmt.Errorf("decode run summary: %w", err)
		}
	}
	if errCode != nil {
		r.Error = &domain.RunError{Code: *errCode, Message: deref(errMessage)}
	}
	if userID != nil {
		r.CreatedBy = &domain.UserRef{PublicID: *userID, Name: deref(userName)}
	}
	return &r, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// CreateRun inserts r and fills in its id and timestamps.
func (r *ReconciliationRepo) CreateRun(ctx context.Context, run *domain.ReconciliationRun) error {
	err := conn(ctx, r.db).QueryRow(ctx, `INSERT INTO reconciliation_runs
		(public_id, connection_id, range_start, range_end, status, run_count, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id, created_at, updated_at`,
		run.PublicID, run.ConnectionID, run.From, run.To, string(run.Status), run.RunCount, run.CreatedByID).
		Scan(&run.ID, &run.CreatedAt, &run.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert reconciliation run: %w", err)
	}
	return nil
}

// GetRun loads a run by rec_ id.
func (r *ReconciliationRepo) GetRun(ctx context.Context, publicID string) (*domain.ReconciliationRun, error) {
	return scanRun(conn(ctx, r.db).QueryRow(ctx, runSelect+` WHERE r.public_id = $1`, publicID))
}

// GetRunByID loads a run by internal id.
func (r *ReconciliationRepo) GetRunByID(ctx context.Context, id int64) (*domain.ReconciliationRun, error) {
	return scanRun(conn(ctx, r.db).QueryRow(ctx, runSelect+` WHERE r.id = $1`, id))
}

// GetRunForUpdate is GetRun with a row lock on the run.
func (r *ReconciliationRepo) GetRunForUpdate(ctx context.Context, publicID string) (*domain.ReconciliationRun, error) {
	return scanRun(conn(ctx, r.db).QueryRow(ctx, runSelect+` WHERE r.public_id = $1 FOR UPDATE OF r`, publicID))
}

// GetRunByIDForUpdate is GetRunByID with a row lock on the run.
func (r *ReconciliationRepo) GetRunByIDForUpdate(ctx context.Context, id int64) (*domain.ReconciliationRun, error) {
	return scanRun(conn(ctx, r.db).QueryRow(ctx, runSelect+` WHERE r.id = $1 FOR UPDATE OF r`, id))
}

// ListRuns returns one page of runs using a keyset on (created_at,
// public_id).
func (r *ReconciliationRepo) ListRuns(ctx context.Context, f domain.ReconciliationRunFilter, p domain.Page) (domain.PageResult[domain.ReconciliationRun], error) {
	var w filters
	if f.ConnectionPublicID != nil {
		w.add("r.connection_id = (SELECT id FROM connections WHERE public_id = ?)", *f.ConnectionPublicID)
	}
	if f.Status != nil {
		w.add("r.status = ?", string(*f.Status))
	}
	if f.Environment != nil {
		w.add("c.environment = ?", string(*f.Environment))
	}
	if p.After != nil {
		w.add("(r.created_at, r.public_id) < (?, ?)", p.After.Time, p.After.ID)
	}
	q := runSelect + w.where() + ` ORDER BY r.created_at DESC, r.public_id DESC LIMIT ` + w.arg(p.Limit+1)
	rows, err := conn(ctx, r.db).Query(ctx, q, w.args...)
	if err != nil {
		return domain.PageResult[domain.ReconciliationRun]{}, fmt.Errorf("list reconciliation runs: %w", err)
	}
	defer rows.Close()
	items := make([]domain.ReconciliationRun, 0, p.Limit+1)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return domain.PageResult[domain.ReconciliationRun]{}, err
		}
		items = append(items, *run)
	}
	if err := rows.Err(); err != nil {
		return domain.PageResult[domain.ReconciliationRun]{}, fmt.Errorf("list reconciliation runs: %w", err)
	}
	return keysetPage(items, p.Limit, func(run domain.ReconciliationRun) domain.Cursor {
		return domain.Cursor{Time: run.CreatedAt, ID: run.PublicID}
	}), nil
}

// UpdateRun writes the mutable columns of run.
func (r *ReconciliationRepo) UpdateRun(ctx context.Context, run *domain.ReconciliationRun) error {
	var summary []byte
	if run.Summary != nil {
		b, err := json.Marshal(run.Summary)
		if err != nil {
			return fmt.Errorf("encode run summary: %w", err)
		}
		summary = b
	}
	var errCode, errMessage *string
	if run.Error != nil {
		errCode, errMessage = &run.Error.Code, &run.Error.Message
	}
	err := conn(ctx, r.db).QueryRow(ctx, `UPDATE reconciliation_runs SET status=$2, run_count=$3, summary=$4,
		unresolved_count=$5, error_code=$6, error_message=$7, started_at=$8, finished_at=$9, updated_at=now()
		WHERE id=$1 RETURNING updated_at`,
		run.ID, string(run.Status), run.RunCount, summary, run.UnresolvedCount, errCode, errMessage,
		run.StartedAt, run.FinishedAt).Scan(&run.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("update reconciliation run: %w", err)
	}
	return nil
}

// ClaimRun moves a queued run of runCount to running.
func (r *ReconciliationRepo) ClaimRun(ctx context.Context, id int64, runCount int) (bool, error) {
	ct, err := conn(ctx, r.db).Exec(ctx, `UPDATE reconciliation_runs SET status='running', started_at=now(), updated_at=now()
		WHERE id=$1 AND run_count=$2 AND status='queued'`, id, runCount)
	if err != nil {
		return false, fmt.Errorf("claim reconciliation run: %w", err)
	}
	return ct.RowsAffected() == 1, nil
}

// TouchRun refreshes updated_at of a running run of runCount.
func (r *ReconciliationRepo) TouchRun(ctx context.Context, id int64, runCount int) (bool, error) {
	ct, err := conn(ctx, r.db).Exec(ctx, `UPDATE reconciliation_runs SET updated_at=now()
		WHERE id=$1 AND run_count=$2 AND status='running'`, id, runCount)
	if err != nil {
		return false, fmt.Errorf("touch reconciliation run: %w", err)
	}
	return ct.RowsAffected() == 1, nil
}

// FailRun ends a queued or running run of runCount as failed.
func (r *ReconciliationRepo) FailRun(ctx context.Context, id int64, runCount int, e domain.RunError) (bool, error) {
	ct, err := conn(ctx, r.db).Exec(ctx, `UPDATE reconciliation_runs SET status='failed', error_code=$3, error_message=$4,
		summary=NULL, finished_at=now(), updated_at=now()
		WHERE id=$1 AND run_count=$2 AND status IN ('queued', 'running')`, id, runCount, e.Code, e.Message)
	if err != nil {
		return false, fmt.Errorf("fail reconciliation run: %w", err)
	}
	return ct.RowsAffected() == 1, nil
}

// SaveItems upserts items by reference in batches, deletes the run's items
// whose reference is gone, and counts the unresolved differences. It must
// run inside the transaction that holds the run's lock.
func (r *ReconciliationRepo) SaveItems(ctx context.Context, runID int64, items []domain.ReconciliationItem) (int, error) {
	db := conn(ctx, r.db)
	refs := make([]string, 0, len(items))
	for start := 0; start < len(items); start += itemBatch {
		batch := items[start:min(start+itemBatch, len(items))]
		cols, err := itemColumns(batch)
		if err != nil {
			return 0, err
		}
		refs = append(refs, cols.references...)
		// A kept reference keeps its public id, and its resolution only while
		// the result stays the same.
		_, err = db.Exec(ctx, `INSERT INTO reconciliation_items
			(public_id, run_id, reference, result, payment_id, keipay, gateway, anchor_at)
			SELECT u.public_id, $1, u.reference, u.result, u.payment_id, u.keipay::jsonb, u.gateway::jsonb, u.anchor_at
			FROM unnest($2::text[], $3::text[], $4::text[], $5::bigint[], $6::text[], $7::text[], $8::timestamptz[])
				AS u(public_id, reference, result, payment_id, keipay, gateway, anchor_at)
			ON CONFLICT (run_id, reference) DO UPDATE SET
				result = EXCLUDED.result, payment_id = EXCLUDED.payment_id, keipay = EXCLUDED.keipay,
				gateway = EXCLUDED.gateway, anchor_at = EXCLUDED.anchor_at, updated_at = now(),
				resolved_at = CASE WHEN reconciliation_items.result = EXCLUDED.result
					THEN reconciliation_items.resolved_at END,
				resolved_by = CASE WHEN reconciliation_items.result = EXCLUDED.result
					THEN reconciliation_items.resolved_by END,
				resolution_note = CASE WHEN reconciliation_items.result = EXCLUDED.result
					THEN reconciliation_items.resolution_note END`,
			runID, cols.publicIDs, cols.references, cols.results, cols.paymentIDs, cols.keipay, cols.gateway, cols.anchors)
		if err != nil {
			return 0, fmt.Errorf("save reconciliation items: %w", err)
		}
	}
	if _, err := db.Exec(ctx, `DELETE FROM reconciliation_items i WHERE i.run_id = $1
		AND NOT EXISTS (SELECT 1 FROM unnest($2::text[]) AS kept(reference) WHERE kept.reference = i.reference)`,
		runID, refs); err != nil {
		return 0, fmt.Errorf("delete vanished reconciliation items: %w", err)
	}
	var unresolved int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM reconciliation_items
		WHERE run_id = $1 AND result <> 'matched' AND resolved_at IS NULL`, runID).Scan(&unresolved); err != nil {
		return 0, fmt.Errorf("count unresolved reconciliation items: %w", err)
	}
	return unresolved, nil
}

// itemColumnArrays are items split into one array per column, the shape
// unnest takes.
type itemColumnArrays struct {
	publicIDs, references, results []string
	paymentIDs                     []*int64
	keipay, gateway                []*string
	anchors                        []time.Time
}

func itemColumns(items []domain.ReconciliationItem) (itemColumnArrays, error) {
	n := len(items)
	c := itemColumnArrays{
		publicIDs: make([]string, n), references: make([]string, n), results: make([]string, n),
		paymentIDs: make([]*int64, n), keipay: make([]*string, n), gateway: make([]*string, n), anchors: make([]time.Time, n),
	}
	for i := range items {
		it := &items[i]
		c.publicIDs[i], c.references[i], c.results[i] = it.PublicID, it.Reference, string(it.Result)
		c.paymentIDs[i], c.anchors[i] = it.PaymentID, it.AnchorAt
		var err error
		if c.keipay[i], err = jsonText(it.Keipay); err != nil {
			return c, err
		}
		if c.gateway[i], err = jsonText(it.Gateway); err != nil {
			return c, err
		}
	}
	return c, nil
}

// jsonText encodes a snapshot; a nil pointer is SQL NULL.
func jsonText[T any](v *T) (*string, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode item snapshot: %w", err)
	}
	s := string(b)
	return &s, nil
}

// itemSelect joins the run's public id, the app of the payment and the user
// who resolved the item.
const itemSelect = `SELECT i.id, i.public_id, i.run_id, r.public_id, i.reference, i.result, i.payment_id,
	i.keipay, i.gateway, a.public_id, a.slug, a.name, a.environment,
	i.anchor_at, i.resolved_at, i.resolved_by, u.public_id, u.name, i.resolution_note, i.created_at, i.updated_at
	FROM reconciliation_items i
	JOIN reconciliation_runs r ON r.id = i.run_id
	LEFT JOIN payments p ON p.id = i.payment_id
	LEFT JOIN apps a ON a.id = p.app_id
	LEFT JOIN users u ON u.id = i.resolved_by`

func scanItem(row pgx.Row) (*domain.ReconciliationItem, error) {
	var it domain.ReconciliationItem
	var result string
	var keipay, gateway []byte
	var appID, appSlug, appName, appEnv, userID, userName, note *string
	var resolvedAt *time.Time
	var resolvedBy *int64
	err := row.Scan(&it.ID, &it.PublicID, &it.RunID, &it.RunPublicID, &it.Reference, &result, &it.PaymentID,
		&keipay, &gateway, &appID, &appSlug, &appName, &appEnv,
		&it.AnchorAt, &resolvedAt, &resolvedBy, &userID, &userName, &note, &it.CreatedAt, &it.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan reconciliation item: %w", err)
	}
	it.Result = domain.ReconciliationResult(result)
	if keipay != nil {
		it.Keipay = &domain.KeipaySide{}
		if err := json.Unmarshal(keipay, it.Keipay); err != nil {
			return nil, fmt.Errorf("decode keipay snapshot: %w", err)
		}
		if appID != nil {
			it.Keipay.App = domain.AppSummary{PublicID: *appID, Slug: *appSlug, Name: *appName, Environment: domain.Environment(*appEnv)}
		}
	}
	if gateway != nil {
		it.Gateway = &domain.GatewaySide{}
		if err := json.Unmarshal(gateway, it.Gateway); err != nil {
			return nil, fmt.Errorf("decode gateway snapshot: %w", err)
		}
	}
	if resolvedAt != nil {
		it.Resolution = &domain.Resolution{Note: deref(note), ResolvedByID: resolvedBy, ResolvedAt: *resolvedAt}
		if userID != nil {
			it.Resolution.ResolvedBy = &domain.UserRef{PublicID: *userID, Name: deref(userName)}
		}
	}
	return &it, nil
}

func itemFilters(runID int64, f domain.ReconciliationItemFilter) *filters {
	w := &filters{}
	w.add("i.run_id = ?", runID)
	if f.Result != nil {
		w.add("i.result = ?", string(*f.Result))
	}
	if f.Resolved != nil {
		if *f.Resolved {
			w.add("i.resolved_at IS NOT NULL")
		} else {
			w.add("i.resolved_at IS NULL")
		}
	}
	return w
}

const itemOrder = ` ORDER BY i.created_at DESC, i.public_id DESC`

// ListItems returns one page of a run's items using a keyset on
// (created_at, public_id).
func (r *ReconciliationRepo) ListItems(ctx context.Context, runID int64, f domain.ReconciliationItemFilter, p domain.Page) (domain.PageResult[domain.ReconciliationItem], error) {
	w := itemFilters(runID, f)
	if p.After != nil {
		w.add("(i.created_at, i.public_id) < (?, ?)", p.After.Time, p.After.ID)
	}
	items := make([]domain.ReconciliationItem, 0, p.Limit+1)
	err := r.eachItem(ctx, itemSelect+w.where()+itemOrder+` LIMIT `+w.arg(p.Limit+1), w.args, func(it *domain.ReconciliationItem) error {
		items = append(items, *it)
		return nil
	})
	if err != nil {
		return domain.PageResult[domain.ReconciliationItem]{}, err
	}
	return keysetPage(items, p.Limit, func(it domain.ReconciliationItem) domain.Cursor {
		return domain.Cursor{Time: it.CreatedAt, ID: it.PublicID}
	}), nil
}

// StreamItems calls fn with every matching item of the run, in ListItems'
// order.
func (r *ReconciliationRepo) StreamItems(ctx context.Context, runID int64, f domain.ReconciliationItemFilter, fn func(*domain.ReconciliationItem) error) error {
	w := itemFilters(runID, f)
	return r.eachItem(ctx, itemSelect+w.where()+itemOrder, w.args, fn)
}

func (r *ReconciliationRepo) eachItem(ctx context.Context, q string, args []any, fn func(*domain.ReconciliationItem) error) error {
	rows, err := conn(ctx, r.db).Query(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("query reconciliation items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return err
		}
		if err := fn(it); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("query reconciliation items: %w", err)
	}
	return nil
}

// GetItem loads an item by rci_ id.
func (r *ReconciliationRepo) GetItem(ctx context.Context, publicID string) (*domain.ReconciliationItem, error) {
	return scanItem(conn(ctx, r.db).QueryRow(ctx, itemSelect+` WHERE i.public_id = $1`, publicID))
}

// GetItemForUpdate is GetItem with a row lock on the item.
func (r *ReconciliationRepo) GetItemForUpdate(ctx context.Context, publicID string) (*domain.ReconciliationItem, error) {
	return scanItem(conn(ctx, r.db).QueryRow(ctx, itemSelect+` WHERE i.public_id = $1 FOR UPDATE OF i`, publicID))
}

// ResolveItem stores the resolution of it.
func (r *ReconciliationRepo) ResolveItem(ctx context.Context, it *domain.ReconciliationItem) error {
	if it.Resolution == nil {
		return fmt.Errorf("resolve reconciliation item: %w: no resolution", domain.ErrInvalidInput)
	}
	err := conn(ctx, r.db).QueryRow(ctx, `UPDATE reconciliation_items SET resolved_at=$2, resolved_by=$3,
		resolution_note=$4, updated_at=now() WHERE id=$1 RETURNING updated_at`,
		it.ID, it.Resolution.ResolvedAt, it.Resolution.ResolvedByID, it.Resolution.Note).Scan(&it.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("resolve reconciliation item: %w", err)
	}
	return nil
}

const keipayPaymentColumns = `p.id, p.public_id, a.public_id, a.slug, a.name, a.environment, p.external_id, p.status,
	COALESCE(r.gross, p.amount), p.currency, r.fee, COALESCE(r.paid_at, p.paid_at)`

// KeipayPayments loads the reports of the connection paid within [from, to),
// then the payments of the gateway's references that were not among them,
// in batches.
func (r *ReconciliationRepo) KeipayPayments(ctx context.Context, connectionID int64, from, to time.Time, refs []string) ([]domain.KeipayPayment, error) {
	seen := map[int64]bool{}
	var out []domain.KeipayPayment
	collect := func(q string, args ...any) error {
		rows, err := conn(ctx, r.db).Query(ctx, q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var k domain.KeipayPayment
			var appEnv, status string
			if err := rows.Scan(&k.PaymentID, &k.PaymentPublicID, &k.App.PublicID, &k.App.Slug, &k.App.Name, &appEnv,
				&k.Reference, &status, &k.Amount, &k.Currency, &k.Fee, &k.PaidAt); err != nil {
				return err
			}
			if seen[k.PaymentID] {
				continue
			}
			seen[k.PaymentID] = true
			k.App.Environment, k.Status = domain.Environment(appEnv), domain.PaymentStatus(status)
			out = append(out, k)
		}
		return rows.Err()
	}
	err := collect(`SELECT `+keipayPaymentColumns+` FROM payment_reports r
		JOIN payments p ON p.id = r.payment_id
		JOIN apps a ON a.id = p.app_id
		WHERE r.connection_id = $1 AND r.paid_at >= $2::timestamptz AND r.paid_at < $3::timestamptz`,
		connectionID, from, to)
	if err != nil {
		return nil, fmt.Errorf("load reconciled reports: %w", err)
	}
	for start := 0; start < len(refs); start += itemBatch {
		batch := refs[start:min(start+itemBatch, len(refs))]
		err := collect(`SELECT `+keipayPaymentColumns+` FROM payments p
			JOIN apps a ON a.id = p.app_id
			LEFT JOIN payment_reports r ON r.payment_id = p.id
			WHERE p.connection_id = $1 AND p.external_id = ANY($2::text[])`, connectionID, batch)
		if err != nil {
			return nil, fmt.Errorf("load reconciled payments: %w", err)
		}
	}
	return out, nil
}
