package memstore

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// Payments is an in-memory domain.PaymentRepository with the unique rules
// of the payments table. Reads fill Payment.App from apps.
type Payments struct {
	mu     sync.Mutex
	byID   map[int64]domain.Payment
	nextID int64
	apps   *Apps
	clock  func() time.Time

	// CreateErr, when set, is returned by Create instead of inserting.
	CreateErr error
	// BeforeCreate runs at the start of Create, outside the lock, e.g. to
	// insert a concurrent winner.
	BeforeCreate func()
}

// NewPayments returns an empty store whose payments belong to apps.
func NewPayments(apps *Apps, now func() time.Time) *Payments {
	return &Payments{byID: map[int64]domain.Payment{}, apps: apps, clock: now}
}

func (s *Payments) snapshot() func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	saved, next := maps.Clone(s.byID), s.nextID
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.byID, s.nextID = saved, next
	}
}

// Create inserts p, enforcing unique external_id, public_id, (app_id,
// order_id) and (app_id, idempotency_key).
func (s *Payments) Create(_ context.Context, p *domain.Payment) error {
	if s.BeforeCreate != nil {
		hook := s.BeforeCreate
		s.BeforeCreate = nil
		hook()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.CreateErr != nil {
		return s.CreateErr
	}
	for _, v := range s.byID {
		switch {
		case v.AppID == p.AppID && v.OrderID == p.OrderID:
			return domain.ErrDuplicateOrderID
		case p.IdempotencyKey != "" && v.AppID == p.AppID && v.IdempotencyKey == p.IdempotencyKey:
			return domain.ErrDuplicateIdempotencyKey
		case v.ExternalID == p.ExternalID || v.PublicID == p.PublicID:
			return domain.ErrConflict
		}
	}
	s.nextID++
	p.ID = s.nextID
	if p.CreatedAt.IsZero() {
		p.CreatedAt = s.clock().UTC()
	}
	p.UpdatedAt = p.CreatedAt
	if p.Metadata == nil {
		p.Metadata = domain.Metadata{}
	}
	s.byID[p.ID] = *p
	return nil
}

// UpdateStatus moves a payment to status, keeping earlier times when nil.
func (s *Payments) UpdateStatus(_ context.Context, id int64, status domain.PaymentStatus, paidAt, settledAt *time.Time, _ []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	p.Status = status
	if paidAt != nil {
		p.PaidAt = paidAt
	}
	if settledAt != nil {
		p.SettledAt = settledAt
	}
	s.byID[id] = p
	return nil
}

func (s *Payments) find(match func(domain.Payment) bool) (*domain.Payment, error) {
	s.mu.Lock()
	var found *domain.Payment
	for _, v := range s.byID {
		if match(v) {
			found = &v
			break
		}
	}
	s.mu.Unlock()
	if found == nil {
		return nil, domain.ErrNotFound
	}
	p := s.withApp(*found)
	return &p, nil
}

// withApp fills p.App from the apps store.
func (s *Payments) withApp(p domain.Payment) domain.Payment {
	if app, err := s.apps.GetByID(context.Background(), p.AppID); err == nil {
		p.App = app.Summary()
	}
	return p
}

// GetByReferenceForUpdate loads the payment whose external_id equals ref.
func (s *Payments) GetByReferenceForUpdate(_ context.Context, ref string) (*domain.Payment, error) {
	return s.find(func(p domain.Payment) bool { return p.ExternalID == ref })
}

// GetByPublicID loads one of the app's payments by pay_ id.
func (s *Payments) GetByPublicID(_ context.Context, appID int64, publicID string) (*domain.Payment, error) {
	return s.find(func(p domain.Payment) bool { return p.AppID == appID && p.PublicID == publicID })
}

// GetByID loads one of the app's payments by integer id.
func (s *Payments) GetByID(_ context.Context, appID, id int64) (*domain.Payment, error) {
	return s.find(func(p domain.Payment) bool { return p.AppID == appID && p.ID == id })
}

// GetByOrderID loads one of the app's payments by order_id.
func (s *Payments) GetByOrderID(_ context.Context, appID int64, orderID string) (*domain.Payment, error) {
	return s.find(func(p domain.Payment) bool { return p.AppID == appID && p.OrderID == orderID })
}

// GetByIdempotencyKey loads one of the app's payments by idempotency key.
func (s *Payments) GetByIdempotencyKey(_ context.Context, appID int64, key string) (*domain.Payment, error) {
	return s.find(func(p domain.Payment) bool { return p.AppID == appID && p.IdempotencyKey == key })
}

// List returns one page of payments, newest first.
func (s *Payments) List(_ context.Context, f domain.PaymentFilter, p domain.Page) (domain.PageResult[domain.Payment], error) {
	s.mu.Lock()
	all := slices.Collect(maps.Values(s.byID))
	s.mu.Unlock()
	var items []domain.Payment
	for _, v := range all {
		if app, err := s.apps.GetByID(context.Background(), v.AppID); err == nil {
			v.App = app.Summary()
		}
		if (f.AppPublicID != nil && v.App.PublicID != *f.AppPublicID) || (f.Status != nil && v.Status != *f.Status) ||
			(f.From != nil && v.CreatedAt.Before(*f.From)) || (f.To != nil && v.CreatedAt.After(*f.To)) {
			continue
		}
		items = append(items, v)
	}
	return page(items, p, func(v domain.Payment) domain.Cursor { return domain.Cursor{Time: v.CreatedAt, ID: v.PublicID} }), nil
}

// All returns every stored payment.
func (s *Payments) All() []domain.Payment {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Collect(maps.Values(s.byID))
}

// BackfillConnection assigns every payment without a connection to
// connectionID.
func (s *Payments) BackfillConnection(_ context.Context, connectionID int64, provider string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for id, p := range s.byID {
		if p.ConnectionID == nil {
			cid := connectionID
			p.ConnectionID, p.Provider = &cid, provider
			s.byID[id] = p
			n++
		}
	}
	return n, nil
}
