package postgres

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// itoa is a small helper for building positional placeholders ($1, $2, ...).
func itoa(i int) string { return strconv.Itoa(i) }

// uniqueConstraint returns the name of the unique constraint or index err
// violated, or "" when err is not a unique violation.
func uniqueConstraint(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		if pgErr.ConstraintName == "" {
			return "?"
		}
		return pgErr.ConstraintName
	}
	return ""
}

// decodeMetadata reads a JSONB metadata column. Numbers stay json.Number so
// their digits survive; an empty column decodes as {}.
func decodeMetadata(raw []byte) (domain.Metadata, error) {
	m := domain.Metadata{}
	if len(raw) == 0 {
		return m, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("decode metadata: %w", err)
	}
	if m == nil {
		m = domain.Metadata{}
	}
	return m, nil
}

// encodeMetadata is the JSONB argument for a metadata column.
func encodeMetadata(m domain.Metadata) ([]byte, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("encode metadata: %w", err)
	}
	return b, nil
}

// keysetPage trims the extra row a keyset query fetched and turns the last
// kept row into the next cursor.
func keysetPage[T any](items []T, limit int, cursor func(T) domain.Cursor) domain.PageResult[T] {
	if items == nil {
		items = []T{}
	}
	res := domain.PageResult[T]{Items: items}
	if len(items) > limit {
		res.Items = items[:limit]
		res.NextCursor = domain.EncodeCursor(cursor(res.Items[limit-1]))
	}
	return res
}

// filters builds a WHERE clause. Each "?" in a condition becomes the next
// numbered placeholder, bound to the matching argument.
type filters struct {
	conds []string
	args  []any
}

func (f *filters) add(cond string, args ...any) {
	for _, v := range args {
		cond = strings.Replace(cond, "?", f.arg(v), 1)
	}
	f.conds = append(f.conds, cond)
}

// arg binds v and returns its placeholder.
func (f *filters) arg(v any) string {
	f.args = append(f.args, v)
	return "$" + itoa(len(f.args))
}

func (f *filters) where() string {
	if len(f.conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(f.conds, " AND ")
}
