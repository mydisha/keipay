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

// Apps is an in-memory domain.AppRepository with the same unique
// (environment, slug) rule as the apps table. Set Err to fail every call.
type Apps struct {
	mu     sync.Mutex
	byID   map[int64]domain.App
	nextID int64
	clock  func() time.Time

	Err error
	// Updates counts Update calls.
	Updates int
}

// NewApps returns an empty store whose timestamps come from now.
func NewApps(now func() time.Time) *Apps {
	return &Apps{byID: map[int64]domain.App{}, clock: now}
}

func (s *Apps) snapshot() func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	saved, next := maps.Clone(s.byID), s.nextID
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.byID, s.nextID = saved, next
	}
}

// Create inserts an app; a duplicate (environment, slug) is domain.ErrConflict.
func (s *Apps) Create(_ context.Context, a *domain.App) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	for _, v := range s.byID {
		if v.Environment == a.Environment && v.Slug == a.Slug {
			return fmt.Errorf("%w: slug exists", domain.ErrConflict)
		}
	}
	s.nextID++
	a.ID = s.nextID
	if a.CreatedAt.IsZero() {
		a.CreatedAt = s.clock().UTC()
	}
	a.UpdatedAt = a.CreatedAt
	if a.Metadata == nil {
		a.Metadata = domain.Metadata{}
	}
	s.byID[a.ID] = cloneApp(*a)
	return nil
}

// Update replaces the mutable fields of a stored app.
func (s *Apps) Update(_ context.Context, a *domain.App) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	cur, ok := s.byID[a.ID]
	if !ok {
		return domain.ErrNotFound
	}
	s.Updates++
	a.UpdatedAt = s.clock().UTC()
	cur.Name, cur.Status, cur.Metadata = a.Name, a.Status, maps.Clone(a.Metadata)
	cur.WebhookURL, cur.RedirectSuccessURL, cur.RedirectFailureURL = a.WebhookURL, a.RedirectSuccessURL, a.RedirectFailureURL
	cur.UpdatedAt = a.UpdatedAt
	s.byID[a.ID] = cur
	return nil
}

func cloneApp(a domain.App) domain.App {
	a.Metadata = maps.Clone(a.Metadata)
	return a
}

func (s *Apps) find(match func(domain.App) bool) (*domain.App, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return nil, s.Err
	}
	for _, v := range s.byID {
		if match(v) {
			c := cloneApp(v)
			return &c, nil
		}
	}
	return nil, domain.ErrNotFound
}

// GetByID loads an app by internal id.
func (s *Apps) GetByID(_ context.Context, id int64) (*domain.App, error) {
	return s.find(func(a domain.App) bool { return a.ID == id })
}

// GetByPublicID loads an app by app_ id.
func (s *Apps) GetByPublicID(_ context.Context, publicID string) (*domain.App, error) {
	return s.find(func(a domain.App) bool { return a.PublicID == publicID })
}

// GetByPublicIDForUpdate is GetByPublicID; callers serialize through Tx.
func (s *Apps) GetByPublicIDForUpdate(ctx context.Context, publicID string) (*domain.App, error) {
	return s.GetByPublicID(ctx, publicID)
}

// GetBySlug loads an app by environment and slug.
func (s *Apps) GetBySlug(_ context.Context, env domain.Environment, slug string) (*domain.App, error) {
	return s.find(func(a domain.App) bool { return a.Environment == env && a.Slug == slug })
}

// List returns one page of apps, newest first.
func (s *Apps) List(_ context.Context, f domain.AppFilter, p domain.Page) (domain.PageResult[domain.App], error) {
	s.mu.Lock()
	all := slices.Collect(maps.Values(s.byID))
	err := s.Err
	s.mu.Unlock()
	if err != nil {
		return domain.PageResult[domain.App]{}, err
	}
	var items []domain.App
	for _, a := range all {
		if (f.Environment != nil && a.Environment != *f.Environment) || (f.Status != nil && a.Status != *f.Status) {
			continue
		}
		items = append(items, cloneApp(a))
	}
	return page(items, p, func(a domain.App) domain.Cursor { return domain.Cursor{Time: a.CreatedAt, ID: a.PublicID} }), nil
}

// page sorts items newest first by cursor and cuts the page after p.After.
func page[T any](items []T, p domain.Page, key func(T) domain.Cursor) domain.PageResult[T] {
	slices.SortFunc(items, func(a, b T) int {
		ka, kb := key(a), key(b)
		if c := kb.Time.Compare(ka.Time); c != 0 {
			return c
		}
		return strings.Compare(kb.ID, ka.ID)
	})
	out := []T{}
	for _, v := range items {
		k := key(v)
		if p.After != nil {
			c := k.Time.Compare(p.After.Time)
			if c > 0 || (c == 0 && k.ID >= p.After.ID) {
				continue
			}
		}
		out = append(out, v)
	}
	res := domain.PageResult[T]{Items: out}
	if len(out) > p.Limit {
		res.Items = out[:p.Limit]
		res.NextCursor = domain.EncodeCursor(key(res.Items[p.Limit-1]))
	}
	return res
}

// All returns every stored app.
func (s *Apps) All() []domain.App {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Collect(maps.Values(s.byID))
}

// APIKeys is an in-memory domain.APIKeyRepository. Like the database, it
// refuses a key whose environment differs from its app's. Set Err to fail
// every call or TouchErr to fail TouchLastUsed only.
type APIKeys struct {
	mu     sync.Mutex
	byID   map[int64]domain.APIKey
	nextID int64
	apps   *Apps
	clock  func() time.Time

	Err      error
	TouchErr error
	// Touches counts TouchLastUsed calls that wrote.
	Touches int
}

// NewAPIKeys returns an empty store whose keys belong to apps.
func NewAPIKeys(apps *Apps, now func() time.Time) *APIKeys {
	return &APIKeys{byID: map[int64]domain.APIKey{}, apps: apps, clock: now}
}

func (s *APIKeys) snapshot() func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	saved, next := maps.Clone(s.byID), s.nextID
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.byID, s.nextID = saved, next
	}
}

// Create inserts a key. Its app must exist with the key's environment.
func (s *APIKeys) Create(ctx context.Context, k *domain.APIKey) error {
	app, err := s.apps.GetByID(ctx, k.AppID)
	if err != nil {
		return fmt.Errorf("api key app: %w", err)
	}
	if app.Environment != k.Environment {
		return fmt.Errorf("api key environment %s does not match app %s", k.Environment, app.Environment)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	for _, v := range s.byID {
		if v.Hash == k.Hash {
			return domain.ErrConflict
		}
	}
	s.nextID++
	k.ID = s.nextID
	if k.CreatedAt.IsZero() {
		k.CreatedAt = s.clock().UTC()
	}
	k.UpdatedAt = k.CreatedAt
	s.byID[k.ID] = *k
	return nil
}

// Put stores k as is, bypassing the environment check, so tests can model
// rows the database would refuse.
func (s *APIKeys) Put(k domain.APIKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if k.ID == 0 {
		s.nextID++
		k.ID = s.nextID
	}
	s.byID[k.ID] = k
}

func (s *APIKeys) find(match func(domain.APIKey) bool) (*domain.APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return nil, s.Err
	}
	for _, v := range s.byID {
		if match(v) {
			return &v, nil
		}
	}
	return nil, domain.ErrNotFound
}

// GetByHashWithApp loads a key by hash and its app.
func (s *APIKeys) GetByHashWithApp(ctx context.Context, hash string) (*domain.APIKey, *domain.App, error) {
	k, err := s.find(func(v domain.APIKey) bool { return v.Hash == hash })
	if err != nil {
		return nil, nil, err
	}
	app, err := s.apps.GetByID(ctx, k.AppID)
	if err != nil {
		return nil, nil, err
	}
	return k, app, nil
}

// GetByPublicID loads one of the app's keys.
func (s *APIKeys) GetByPublicID(_ context.Context, appID int64, publicID string) (*domain.APIKey, error) {
	return s.find(func(v domain.APIKey) bool { return v.AppID == appID && v.PublicID == publicID })
}

// GetByPublicIDForUpdate is GetByPublicID.
func (s *APIKeys) GetByPublicIDForUpdate(ctx context.Context, appID int64, publicID string) (*domain.APIKey, error) {
	return s.GetByPublicID(ctx, appID, publicID)
}

// List returns one page of the app's keys, newest first.
func (s *APIKeys) List(_ context.Context, appID int64, p domain.Page) (domain.PageResult[domain.APIKey], error) {
	s.mu.Lock()
	var items []domain.APIKey
	for _, v := range s.byID {
		if v.AppID == appID {
			items = append(items, v)
		}
	}
	err := s.Err
	s.mu.Unlock()
	if err != nil {
		return domain.PageResult[domain.APIKey]{}, err
	}
	return page(items, p, func(k domain.APIKey) domain.Cursor { return domain.Cursor{Time: k.CreatedAt, ID: k.PublicID} }), nil
}

// CountUsable counts the app's keys that are active at now.
func (s *APIKeys) CountUsable(_ context.Context, appID int64, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return 0, s.Err
	}
	n := 0
	for _, v := range s.byID {
		if v.AppID == appID && v.Status(now) == domain.APIKeyActive {
			n++
		}
	}
	return n, nil
}

// Revoke stores k.RevokedAt.
func (s *APIKeys) Revoke(_ context.Context, k *domain.APIKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	cur, ok := s.byID[k.ID]
	if !ok {
		return domain.ErrNotFound
	}
	cur.RevokedAt = k.RevokedAt
	cur.UpdatedAt = s.clock().UTC()
	k.UpdatedAt = cur.UpdatedAt
	s.byID[k.ID] = cur
	return nil
}

// TouchLastUsed sets last_used_at unless it was set less than a minute before.
func (s *APIKeys) TouchLastUsed(_ context.Context, id int64, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.TouchErr != nil {
		return s.TouchErr
	}
	cur, ok := s.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	if cur.LastUsedAt != nil && now.Sub(*cur.LastUsedAt) < time.Minute {
		return nil
	}
	cur.LastUsedAt = &now
	s.byID[id] = cur
	s.Touches++
	return nil
}

// Get returns a stored key by public id, or nil.
func (s *APIKeys) Get(publicID string) *domain.APIKey {
	k, err := s.find(func(v domain.APIKey) bool { return v.PublicID == publicID })
	if err != nil {
		return nil
	}
	return k
}

// All returns every stored key.
func (s *APIKeys) All() []domain.APIKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Collect(maps.Values(s.byID))
}
