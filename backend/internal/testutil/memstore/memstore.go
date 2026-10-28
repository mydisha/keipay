// Package memstore holds in-memory implementations of the user, session,
// failure-counter, audit, app, API key and payment ports for tests. Tx gives them transaction
// semantics: every store registered with it is restored when fn fails.
package memstore

import (
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// Clock is a manual clock shared by the stores and the code under test.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// NewClock returns a clock set to start.
func NewClock(start time.Time) *Clock { return &Clock{now: start} }

// Now returns the current fake time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// snapshotter is a store that Tx can restore.
type snapshotter interface {
	snapshot() (restore func())
}

// Tx is a domain.TxManager over in-memory stores: when fn fails, every store
// it was built with is put back as it was before fn ran.
type Tx struct {
	stores    []snapshotter
	Commits   int
	Rollbacks int
}

// NewTx returns a Tx that restores the given stores on rollback.
func NewTx(stores ...snapshotter) *Tx { return &Tx{stores: stores} }

// WithinTx runs fn and restores the stores if it fails.
func (t *Tx) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	restores := make([]func(), 0, len(t.stores))
	for _, s := range t.stores {
		restores = append(restores, s.snapshot())
	}
	if err := fn(ctx); err != nil {
		for _, r := range restores {
			r()
		}
		t.Rollbacks++
		return err
	}
	t.Commits++
	return nil
}

// Users is an in-memory domain.UserRepository. Set Err to make every call
// fail; set GetErr to fail lookups only.
type Users struct {
	mu     sync.Mutex
	byID   map[int64]domain.User
	nextID int64
	clock  func() time.Time

	Err    error
	GetErr error
	// Locks counts LockUsers calls.
	Locks int
}

// NewUsers returns an empty store whose timestamps come from now.
func NewUsers(now func() time.Time) *Users {
	return &Users{byID: map[int64]domain.User{}, clock: now}
}

func (u *Users) snapshot() func() {
	u.mu.Lock()
	defer u.mu.Unlock()
	saved, next := maps.Clone(u.byID), u.nextID
	return func() {
		u.mu.Lock()
		defer u.mu.Unlock()
		u.byID, u.nextID = saved, next
	}
}

// Add stores a user directly, filling id and timestamps when unset.
func (u *Users) Add(user *domain.User) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.insert(user)
}

func (u *Users) insert(user *domain.User) {
	u.nextID++
	user.ID = u.nextID
	if user.CreatedAt.IsZero() {
		user.CreatedAt = u.clock().UTC()
	}
	user.UpdatedAt = user.CreatedAt
	if user.SessionVersion == 0 {
		user.SessionVersion = 1
	}
	u.byID[user.ID] = *user
}

func (u *Users) emailTaken(email string, except int64) bool {
	for _, v := range u.byID {
		if v.Email == email && v.ID != except {
			return true
		}
	}
	return false
}

// Create inserts a user; a duplicate email is domain.ErrConflict.
func (u *Users) Create(_ context.Context, user *domain.User) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.Err != nil {
		return u.Err
	}
	if u.emailTaken(user.Email, 0) {
		return domain.ErrConflict
	}
	u.insert(user)
	return nil
}

// Update replaces a stored user.
func (u *Users) Update(_ context.Context, user *domain.User) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.Err != nil {
		return u.Err
	}
	if _, ok := u.byID[user.ID]; !ok {
		return domain.ErrNotFound
	}
	if u.emailTaken(user.Email, user.ID) {
		return domain.ErrConflict
	}
	user.UpdatedAt = u.clock().UTC()
	u.byID[user.ID] = *user
	return nil
}

// SetLastLogin stores the sign-in time.
func (u *Users) SetLastLogin(_ context.Context, id int64, at time.Time) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.Err != nil {
		return u.Err
	}
	v, ok := u.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	v.LastLoginAt = &at
	u.byID[id] = v
	return nil
}

func (u *Users) find(match func(domain.User) bool) (*domain.User, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.Err != nil {
		return nil, u.Err
	}
	if u.GetErr != nil {
		return nil, u.GetErr
	}
	for _, v := range u.byID {
		if match(v) {
			return &v, nil
		}
	}
	return nil, domain.ErrNotFound
}

// GetByID loads a user by internal id.
func (u *Users) GetByID(_ context.Context, id int64) (*domain.User, error) {
	return u.find(func(v domain.User) bool { return v.ID == id })
}

// GetByIDForUpdate is GetByID; tests run one request at a time.
func (u *Users) GetByIDForUpdate(ctx context.Context, id int64) (*domain.User, error) {
	return u.GetByID(ctx, id)
}

// GetByPublicID loads a user by usr_ id.
func (u *Users) GetByPublicID(_ context.Context, publicID string) (*domain.User, error) {
	return u.find(func(v domain.User) bool { return v.PublicID == publicID })
}

// GetByPublicIDForUpdate is GetByPublicID.
func (u *Users) GetByPublicIDForUpdate(ctx context.Context, publicID string) (*domain.User, error) {
	return u.GetByPublicID(ctx, publicID)
}

// GetByEmail loads a user by canonical email.
func (u *Users) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	return u.find(func(v domain.User) bool { return v.Email == email })
}

// OldestOwner returns the first owner created.
func (u *Users) OldestOwner(_ context.Context) (*domain.User, error) {
	all := u.sorted(false)
	for _, v := range all {
		if v.Role == domain.RoleOwner {
			return &v, nil
		}
	}
	return nil, domain.ErrNotFound
}

// Count returns the number of users.
func (u *Users) Count(_ context.Context) (int, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.Err != nil {
		return 0, u.Err
	}
	return len(u.byID), nil
}

// CountActiveOwners returns the number of enabled owners.
func (u *Users) CountActiveOwners(_ context.Context) (int, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.Err != nil {
		return 0, u.Err
	}
	n := 0
	for _, v := range u.byID {
		if v.IsActiveOwner() {
			n++
		}
	}
	return n, nil
}

// sorted returns users by (created_at, public id), newest first when desc.
func (u *Users) sorted(desc bool) []domain.User {
	u.mu.Lock()
	defer u.mu.Unlock()
	all := slices.Collect(maps.Values(u.byID))
	slices.SortFunc(all, func(a, b domain.User) int {
		c := a.CreatedAt.Compare(b.CreatedAt)
		if c == 0 {
			c = strings.Compare(a.PublicID, b.PublicID)
		}
		if desc {
			return -c
		}
		return c
	})
	return all
}

// List returns one page, newest first.
func (u *Users) List(_ context.Context, p domain.Page) (domain.PageResult[domain.User], error) {
	if u.Err != nil {
		return domain.PageResult[domain.User]{}, u.Err
	}
	var items []domain.User
	for _, v := range u.sorted(true) {
		if p.After != nil {
			c := v.CreatedAt.Compare(p.After.Time)
			if c > 0 || (c == 0 && v.PublicID >= p.After.ID) {
				continue
			}
		}
		items = append(items, v)
	}
	res := domain.PageResult[domain.User]{Items: items}
	if len(items) > p.Limit {
		res.Items = items[:p.Limit]
		last := res.Items[p.Limit-1]
		res.NextCursor = domain.EncodeCursor(domain.Cursor{Time: last.CreatedAt, ID: last.PublicID})
	}
	return res, nil
}

// LockUsers counts the call; tests run one request at a time.
func (u *Users) LockUsers(context.Context) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.Err != nil {
		return u.Err
	}
	u.Locks++
	return nil
}

// Remove deletes a user, as if the row were gone.
func (u *Users) Remove(publicID string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	for id, v := range u.byID {
		if v.PublicID == publicID {
			delete(u.byID, id)
		}
	}
}

// All returns every stored user, oldest first.
func (u *Users) All() []domain.User { return u.sorted(false) }

// Get returns a stored user by public id, or nil.
func (u *Users) Get(publicID string) *domain.User {
	v, err := u.GetByPublicID(context.Background(), publicID)
	if err != nil {
		return nil
	}
	return v
}

type sessionEntry struct {
	session   domain.Session
	expiresAt time.Time
	ttl       time.Duration
}

// Sessions is an in-memory domain.SessionStore that expires entries by the
// shared clock. Set Err to make every call fail.
type Sessions struct {
	mu    sync.Mutex
	items map[string]sessionEntry
	clock func() time.Time

	Err error
	// Touches counts Touch calls.
	Touches int
}

// NewSessions returns an empty store.
func NewSessions(now func() time.Time) *Sessions {
	return &Sessions{items: map[string]sessionEntry{}, clock: now}
}

// Create stores a copy of s for ttl.
func (s *Sessions) Create(_ context.Context, token string, sess *domain.Session, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	s.items[token] = sessionEntry{session: *sess, expiresAt: s.clock().Add(ttl), ttl: ttl}
	return nil
}

func (s *Sessions) live(token string) (sessionEntry, bool) {
	e, ok := s.items[token]
	if !ok {
		return e, false
	}
	if !s.clock().Before(e.expiresAt) {
		delete(s.items, token)
		return e, false
	}
	return e, true
}

// Get returns a copy of a live session or domain.ErrNotFound.
func (s *Sessions) Get(_ context.Context, token string) (*domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return nil, s.Err
	}
	e, ok := s.live(token)
	if !ok {
		return nil, domain.ErrNotFound
	}
	sess := e.session
	return &sess, nil
}

// Touch rewrites a live session and its TTL.
func (s *Sessions) Touch(_ context.Context, token string, sess *domain.Session, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	s.Touches++
	if _, ok := s.live(token); ok {
		s.items[token] = sessionEntry{session: *sess, expiresAt: s.clock().Add(ttl), ttl: ttl}
	}
	return nil
}

// Delete removes a session.
func (s *Sessions) Delete(_ context.Context, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	delete(s.items, token)
	return nil
}

// Has reports whether token names a live session.
func (s *Sessions) Has(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.live(token)
	return ok
}

// TTL returns the TTL last set for token.
func (s *Sessions) TTL(token string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.items[token].ttl
}

// Len returns the number of stored sessions, expired ones included.
func (s *Sessions) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

type counter struct {
	n         int
	expiresAt time.Time
}

// Failures is an in-memory fixed-window domain.FailureCounter.
type Failures struct {
	mu    sync.Mutex
	items map[string]counter
	clock func() time.Time

	Err error
}

// NewFailures returns an empty counter.
func NewFailures(now func() time.Time) *Failures {
	return &Failures{items: map[string]counter{}, clock: now}
}

func (f *Failures) live(key string) (counter, bool) {
	c, ok := f.items[key]
	if ok && !f.clock().Before(c.expiresAt) {
		delete(f.items, key)
		return counter{}, false
	}
	return c, ok
}

// Reserve counts one attempt under every limit, or none when a limit is
// already reached, under one lock like the Redis script.
func (f *Failures) Reserve(_ context.Context, window time.Duration, limits ...domain.FailureLimit) (bool, time.Duration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return false, 0, f.Err
	}
	exceeded := false
	var wait time.Duration
	for _, l := range limits {
		if c, ok := f.live(l.Key); ok && c.n >= l.Max {
			exceeded = true
			wait = max(wait, c.expiresAt.Sub(f.clock()))
		}
	}
	if exceeded {
		return false, wait, nil
	}
	for _, l := range limits {
		f.inc(l.Key, window)
	}
	return true, 0, nil
}

// Release takes back one reserved attempt per key.
func (f *Failures) Release(_ context.Context, keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	for _, key := range keys {
		c, ok := f.live(key)
		if !ok {
			continue
		}
		c.n--
		if c.n <= 0 {
			delete(f.items, key)
			continue
		}
		f.items[key] = c
	}
	return nil
}

// Inc adds one failure, starting a window on the first. Tests use it to
// seed counters.
func (f *Failures) Inc(key string, window time.Duration) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inc(key, window)
}

func (f *Failures) inc(key string, window time.Duration) int {
	c, ok := f.live(key)
	if !ok {
		c = counter{expiresAt: f.clock().Add(window)}
	}
	c.n++
	f.items[key] = c
	return c.n
}

// Reset clears a counter.
func (f *Failures) Reset(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	delete(f.items, key)
	return nil
}

// Value returns the live count for key.
func (f *Failures) Value(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, _ := f.live(key)
	return c.n
}

// Audit is an in-memory domain.AuditRepository. Set Err to fail inserts.
type Audit struct {
	mu   sync.Mutex
	logs []domain.AuditLog

	Err error
}

func (a *Audit) snapshot() func() {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := len(a.logs)
	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.logs = a.logs[:n]
	}
}

// Create appends an entry.
func (a *Audit) Create(_ context.Context, l *domain.AuditLog) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.Err != nil {
		return a.Err
	}
	a.logs = append(a.logs, *l)
	return nil
}

// List returns every entry, newest first.
func (a *Audit) List(context.Context, domain.Page) (domain.PageResult[domain.AuditLog], error) {
	logs := a.Logs()
	slices.Reverse(logs)
	return domain.PageResult[domain.AuditLog]{Items: logs}, nil
}

// Logs returns a copy of the entries in insertion order.
func (a *Audit) Logs() []domain.AuditLog {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.logs)
}

// Reset drops every entry.
func (a *Audit) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.logs = nil
}
