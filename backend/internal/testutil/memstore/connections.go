package memstore

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// DefaultMerchantID is the id of the one merchant the stores know.
const DefaultMerchantID int64 = 1

// Connections is an in-memory domain.ConnectionRepository with the unique
// (merchant, name) rule of the connections table. It also keeps the default
// merchant; see Merchants. Set Err to fail every call.
type Connections struct {
	mu       sync.Mutex
	byID     map[int64]domain.Connection
	channels map[int64][]domain.ConnectionChannel
	nextID   int64
	legacyID *int64
	apps     *Apps
	clock    func() time.Time

	Err error
	// Updates counts Update calls.
	Updates int
}

// NewConnections returns an empty store and links apps to it, so app reads
// carry their default connection's summary.
func NewConnections(apps *Apps, now func() time.Time) *Connections {
	c := &Connections{byID: map[int64]domain.Connection{}, channels: map[int64][]domain.ConnectionChannel{}, apps: apps, clock: now}
	if apps != nil {
		apps.mu.Lock()
		apps.conns = c
		apps.mu.Unlock()
	}
	return c
}

func (s *Connections) snapshot() func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	saved, chans, next, legacy := maps.Clone(s.byID), maps.Clone(s.channels), s.nextID, s.legacyID
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.byID, s.channels, s.nextID, s.legacyID = saved, chans, next, legacy
	}
}

func cloneConnection(c domain.Connection) domain.Connection {
	c.CredentialsLast4 = maps.Clone(c.CredentialsLast4)
	c.Settings = maps.Clone(c.Settings)
	return c
}

func (s *Connections) nameTaken(merchantID int64, name string, except int64) bool {
	for _, v := range s.byID {
		if v.MerchantID == merchantID && v.Name == name && v.ID != except {
			return true
		}
	}
	return false
}

// Create inserts a connection; a duplicate name is domain.ErrConflict.
func (s *Connections) Create(_ context.Context, c *domain.Connection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	if s.nameTaken(c.MerchantID, c.Name, 0) {
		return fmt.Errorf("%w: connection name already used", domain.ErrConflict)
	}
	s.nextID++
	c.ID = s.nextID
	if c.CreatedAt.IsZero() {
		c.CreatedAt = s.clock().UTC()
	}
	c.UpdatedAt = c.CreatedAt
	s.byID[c.ID] = cloneConnection(*c)
	return nil
}

// Update replaces the mutable fields of a stored connection.
func (s *Connections) Update(_ context.Context, c *domain.Connection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	cur, ok := s.byID[c.ID]
	if !ok {
		return domain.ErrNotFound
	}
	if s.nameTaken(cur.MerchantID, c.Name, c.ID) {
		return fmt.Errorf("%w: connection name already used", domain.ErrConflict)
	}
	s.Updates++
	c.UpdatedAt = s.clock().UTC()
	next := cloneConnection(*c)
	next.Provider, next.Environment, next.MerchantID, next.CreatedAt = cur.Provider, cur.Environment, cur.MerchantID, cur.CreatedAt
	s.byID[c.ID] = next
	return nil
}

func (s *Connections) find(match func(domain.Connection) bool) (*domain.Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return nil, s.Err
	}
	for _, v := range s.byID {
		if match(v) {
			c := cloneConnection(v)
			c.LegacyWebhookAlias = s.legacyID != nil && *s.legacyID == c.ID
			return &c, nil
		}
	}
	return nil, domain.ErrNotFound
}

// GetByID loads a connection by internal id.
func (s *Connections) GetByID(_ context.Context, id int64) (*domain.Connection, error) {
	return s.find(func(c domain.Connection) bool { return c.ID == id })
}

// GetByPublicID loads a connection by con_ id.
func (s *Connections) GetByPublicID(_ context.Context, publicID string) (*domain.Connection, error) {
	return s.find(func(c domain.Connection) bool { return c.PublicID == publicID })
}

// GetByPublicIDForUpdate is GetByPublicID; callers serialize through Tx.
func (s *Connections) GetByPublicIDForUpdate(ctx context.Context, publicID string) (*domain.Connection, error) {
	return s.GetByPublicID(ctx, publicID)
}

// GetByPublicIDForShare is GetByPublicID; callers serialize through Tx.
func (s *Connections) GetByPublicIDForShare(ctx context.Context, publicID string) (*domain.Connection, error) {
	return s.GetByPublicID(ctx, publicID)
}

// List returns one page of connections, newest first.
func (s *Connections) List(_ context.Context, f domain.ConnectionFilter, p domain.Page) (domain.PageResult[domain.Connection], error) {
	s.mu.Lock()
	all := slices.Collect(maps.Values(s.byID))
	err, legacy := s.Err, s.legacyID
	s.mu.Unlock()
	if err != nil {
		return domain.PageResult[domain.Connection]{}, err
	}
	var items []domain.Connection
	for _, c := range all {
		if (f.Environment != nil && c.Environment != *f.Environment) || (f.Status != nil && c.Status != *f.Status) ||
			(f.Provider != nil && c.Provider != *f.Provider) {
			continue
		}
		c = cloneConnection(c)
		c.LegacyWebhookAlias = legacy != nil && *legacy == c.ID
		items = append(items, c)
	}
	return page(items, p, func(c domain.Connection) domain.Cursor { return domain.Cursor{Time: c.CreatedAt, ID: c.PublicID} }), nil
}

// NameTaken reports whether the merchant has a connection named name.
func (s *Connections) NameTaken(_ context.Context, merchantID int64, name string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nameTaken(merchantID, name, 0), s.Err
}

// Channels returns the stored channel rows, sorted by code.
func (s *Connections) Channels(_ context.Context, connectionID int64) ([]domain.ConnectionChannel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := slices.Clone(s.channels[connectionID])
	slices.SortFunc(out, func(a, b domain.ConnectionChannel) int { return strings.Compare(string(a.Channel), string(b.Channel)) })
	return out, s.Err
}

// ReplaceChannels replaces the stored channel rows.
func (s *Connections) ReplaceChannels(_ context.Context, connectionID int64, chs []domain.ConnectionChannel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	if len(chs) == 0 {
		delete(s.channels, connectionID)
		return nil
	}
	s.channels[connectionID] = slices.Clone(chs)
	return nil
}

// AppsUsing returns up to limit apps whose default is connectionID, newest
// first, and their total count.
func (s *Connections) AppsUsing(_ context.Context, connectionID int64, limit int) ([]domain.AppSummary, int, error) {
	var using []domain.App
	for _, a := range s.apps.All() {
		if a.DefaultConnectionID != nil && *a.DefaultConnectionID == connectionID {
			using = append(using, a)
		}
	}
	slices.SortFunc(using, func(a, b domain.App) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.PublicID, a.PublicID)
	})
	out := []domain.AppSummary{}
	for i := 0; i < len(using) && i < limit; i++ {
		out = append(out, using[i].Summary())
	}
	return out, len(using), nil
}

// All returns every stored connection.
func (s *Connections) All() []domain.Connection {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Collect(maps.Values(s.byID))
}

// Merchants returns the domain.MerchantRepository view of the store.
func (s *Connections) Merchants() *Merchants { return &Merchants{c: s} }

// Merchants is the default merchant of a Connections store.
type Merchants struct {
	c *Connections
	// ForUpdate counts GetDefaultForUpdate calls.
	ForUpdate int
}

// GetDefault returns the default merchant.
func (m *Merchants) GetDefault(context.Context) (*domain.Merchant, error) {
	m.c.mu.Lock()
	defer m.c.mu.Unlock()
	var legacy *int64
	if m.c.legacyID != nil {
		v := *m.c.legacyID
		legacy = &v
	}
	return &domain.Merchant{ID: DefaultMerchantID, Name: "Default", IsDefault: true, LegacyConnectionID: legacy}, nil
}

// GetDefaultForUpdate is GetDefault; callers serialize through Tx.
func (m *Merchants) GetDefaultForUpdate(ctx context.Context) (*domain.Merchant, error) {
	m.ForUpdate++
	return m.GetDefault(ctx)
}

// SetLegacyConnection points the alias at connectionID.
func (m *Merchants) SetLegacyConnection(_ context.Context, merchantID, connectionID int64) error {
	if merchantID != DefaultMerchantID {
		return domain.ErrNotFound
	}
	m.c.mu.Lock()
	defer m.c.mu.Unlock()
	m.c.legacyID = &connectionID
	return nil
}
