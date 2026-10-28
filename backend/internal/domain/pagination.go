package domain

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// Page size bounds for cursor-paginated lists.
const (
	DefaultPageLimit = 50
	MaxPageLimit     = 200
)

// PageRequest asks for one page of a cursor-paginated list. Cursor is the
// opaque NextCursor of the previous page, or "" for the first page.
type PageRequest struct {
	Limit  int
	Cursor string
}

// PageResult is one page of items, newest first. NextCursor is "" on the last page.
type PageResult[T any] struct {
	Items      []T
	NextCursor string
}

// Page is a validated PageRequest as repositories receive it: After is the
// decoded position to continue from, nil for the first page.
type Page struct {
	Limit int
	After *Cursor
}

// Page validates r, applies the default limit, and decodes the cursor.
func (r PageRequest) Page() (Page, error) {
	limit := r.Limit
	if limit == 0 {
		limit = DefaultPageLimit
	}
	if limit < 1 || limit > MaxPageLimit {
		return Page{}, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidInput, MaxPageLimit)
	}
	p := Page{Limit: limit}
	if r.Cursor != "" {
		c, err := DecodeCursor(r.Cursor)
		if err != nil {
			return Page{}, err
		}
		p.After = &c
	}
	return p, nil
}

// Cursor is a keyset position: the sort value of the last row on a page and
// its public id as the tie-breaker. Internal ids never go into a cursor.
type Cursor struct {
	Time time.Time
	ID   string
}

const cursorVersion = 1

type cursorWire struct {
	V  int    `json:"v"`
	T  string `json:"t"`
	ID string `json:"id"`
}

// EncodeCursor returns the opaque form of c: unpadded base64url of a small
// versioned JSON document.
func EncodeCursor(c Cursor) string {
	b, _ := json.Marshal(cursorWire{V: cursorVersion, T: c.Time.UTC().Format(time.RFC3339Nano), ID: c.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeCursor parses a cursor made by EncodeCursor. Any malformed input
// returns an error matching ErrInvalidCursor.
func DecodeCursor(s string) (Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	var w cursorWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	if w.V != cursorVersion || w.ID == "" {
		return Cursor{}, ErrInvalidCursor
	}
	t, err := time.Parse(time.RFC3339Nano, w.T)
	if err != nil || t.IsZero() {
		return Cursor{}, ErrInvalidCursor
	}
	return Cursor{Time: t.UTC(), ID: w.ID}, nil
}
