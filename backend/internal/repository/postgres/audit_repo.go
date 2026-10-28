package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// AuditRepo is a pgx-backed domain.AuditRepository.
type AuditRepo struct {
	db *pgxpool.Pool
}

// NewAuditRepo constructs an AuditRepo.
func NewAuditRepo(db *pgxpool.Pool) *AuditRepo { return &AuditRepo{db: db} }

// Create inserts an audit entry inside the transaction carried by ctx, if any.
func (r *AuditRepo) Create(ctx context.Context, l *domain.AuditLog) error {
	const q = `INSERT INTO audit_logs (public_id, occurred_at, actor_type, actor_id, action,
		target_type, target_id, before, after, ip, user_agent, request_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING id`
	err := conn(ctx, r.db).QueryRow(ctx, q, l.PublicID, l.OccurredAt, l.Actor.Type, l.Actor.ID, l.Action,
		l.Target.Type, l.Target.ID, jsonArg(l.Before), jsonArg(l.After),
		l.Actor.IP, l.Actor.UserAgent, l.Actor.RequestID).Scan(&l.ID)
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}

// List returns one page of entries, newest first, using a keyset on
// (occurred_at, public_id).
func (r *AuditRepo) List(ctx context.Context, p domain.Page) (domain.PageResult[domain.AuditLog], error) {
	const cols = `id, public_id, occurred_at, actor_type, actor_id, action, target_type, target_id,
		before, after, ip, user_agent, request_id`
	var (
		rows pgx.Rows
		err  error
	)
	// One extra row tells whether another page exists.
	if p.After == nil {
		rows, err = conn(ctx, r.db).Query(ctx, `SELECT `+cols+` FROM audit_logs
			ORDER BY occurred_at DESC, public_id DESC LIMIT $1`, p.Limit+1)
	} else {
		rows, err = conn(ctx, r.db).Query(ctx, `SELECT `+cols+` FROM audit_logs
			WHERE (occurred_at, public_id) < ($1, $2)
			ORDER BY occurred_at DESC, public_id DESC LIMIT $3`, p.After.Time, p.After.ID, p.Limit+1)
	}
	if err != nil {
		return domain.PageResult[domain.AuditLog]{}, fmt.Errorf("list audit logs: %w", err)
	}
	defer rows.Close()

	items := make([]domain.AuditLog, 0, p.Limit+1)
	for rows.Next() {
		var l domain.AuditLog
		var before, after []byte
		if err := rows.Scan(&l.ID, &l.PublicID, &l.OccurredAt, &l.Actor.Type, &l.Actor.ID, &l.Action,
			&l.Target.Type, &l.Target.ID, &before, &after, &l.Actor.IP, &l.Actor.UserAgent, &l.Actor.RequestID); err != nil {
			return domain.PageResult[domain.AuditLog]{}, fmt.Errorf("scan audit log: %w", err)
		}
		l.Before, l.After = before, after
		items = append(items, l)
	}
	if err := rows.Err(); err != nil {
		return domain.PageResult[domain.AuditLog]{}, fmt.Errorf("list audit logs: %w", err)
	}

	res := domain.PageResult[domain.AuditLog]{Items: items}
	if len(items) > p.Limit {
		res.Items = items[:p.Limit]
		last := res.Items[p.Limit-1]
		res.NextCursor = domain.EncodeCursor(domain.Cursor{Time: last.OccurredAt, ID: last.PublicID})
	}
	return res, nil
}

// jsonArg passes a nil snapshot as SQL NULL rather than JSON null.
func jsonArg(b json.RawMessage) any {
	if b == nil {
		return nil
	}
	return []byte(b)
}
