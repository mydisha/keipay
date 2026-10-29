package memstore

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// Reconciliations is an in-memory domain.ReconciliationRepository. Reads
// fill the connection from conns, users from users (when set) and item apps
// from payments. KeipayPayments reads payments and reports. Set Err to fail
// every call.
type Reconciliations struct {
	mu       sync.Mutex
	runs     map[int64]domain.ReconciliationRun
	items    map[int64]domain.ReconciliationItem
	nextRun  int64
	nextItem int64
	conns    *Connections
	payments *Payments
	reports  *Reports
	users    *Users
	clock    func() time.Time

	Err error
}

// NewReconciliations returns an empty store. users may be nil.
func NewReconciliations(conns *Connections, payments *Payments, reports *Reports, users *Users, now func() time.Time) *Reconciliations {
	return &Reconciliations{runs: map[int64]domain.ReconciliationRun{}, items: map[int64]domain.ReconciliationItem{},
		conns: conns, payments: payments, reports: reports, users: users, clock: now}
}

func (s *Reconciliations) snapshot() func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	runs, items, nextRun, nextItem := maps.Clone(s.runs), maps.Clone(s.items), s.nextRun, s.nextItem
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.runs, s.items, s.nextRun, s.nextItem = runs, items, nextRun, nextItem
	}
}

func (s *Reconciliations) now() time.Time { return s.clock().UTC() }

// CreateRun stores r and fills in its id and timestamps.
func (s *Reconciliations) CreateRun(_ context.Context, r *domain.ReconciliationRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	s.nextRun++
	r.ID, r.CreatedAt, r.UpdatedAt = s.nextRun, s.now(), s.now()
	s.runs[r.ID] = *r
	return nil
}

func (s *Reconciliations) fillRun(r domain.ReconciliationRun) *domain.ReconciliationRun {
	if c, err := s.conns.GetByID(context.Background(), r.ConnectionID); err == nil {
		r.Connection = c.Summary()
	}
	r.CreatedBy = s.userRef(r.CreatedByID)
	return &r
}

func (s *Reconciliations) userRef(userID *int64) *domain.UserRef {
	if userID == nil || s.users == nil {
		return nil
	}
	u, err := s.users.GetByID(context.Background(), *userID)
	if err != nil {
		return nil
	}
	return &domain.UserRef{PublicID: u.PublicID, Name: u.Name}
}

func (s *Reconciliations) findRun(match func(domain.ReconciliationRun) bool) (*domain.ReconciliationRun, error) {
	s.mu.Lock()
	if s.Err != nil {
		s.mu.Unlock()
		return nil, s.Err
	}
	var found *domain.ReconciliationRun
	for _, r := range s.runs {
		if match(r) {
			found = &r
			break
		}
	}
	s.mu.Unlock()
	if found == nil {
		return nil, domain.ErrNotFound
	}
	return s.fillRun(*found), nil
}

// GetRun loads a run by public id.
func (s *Reconciliations) GetRun(_ context.Context, publicID string) (*domain.ReconciliationRun, error) {
	return s.findRun(func(r domain.ReconciliationRun) bool { return r.PublicID == publicID })
}

// GetRunByID loads a run by id.
func (s *Reconciliations) GetRunByID(_ context.Context, id int64) (*domain.ReconciliationRun, error) {
	return s.findRun(func(r domain.ReconciliationRun) bool { return r.ID == id })
}

// GetRunForUpdate is GetRun; tests run one request at a time.
func (s *Reconciliations) GetRunForUpdate(ctx context.Context, publicID string) (*domain.ReconciliationRun, error) {
	return s.GetRun(ctx, publicID)
}

// GetRunByIDForUpdate is GetRunByID.
func (s *Reconciliations) GetRunByIDForUpdate(ctx context.Context, id int64) (*domain.ReconciliationRun, error) {
	return s.GetRunByID(ctx, id)
}

// ListRuns returns one page of runs, newest first.
func (s *Reconciliations) ListRuns(_ context.Context, f domain.ReconciliationRunFilter, p domain.Page) (domain.PageResult[domain.ReconciliationRun], error) {
	s.mu.Lock()
	all := slices.Collect(maps.Values(s.runs))
	err := s.Err
	s.mu.Unlock()
	if err != nil {
		return domain.PageResult[domain.ReconciliationRun]{}, err
	}
	var items []domain.ReconciliationRun
	for _, r := range all {
		run := s.fillRun(r)
		if (f.ConnectionPublicID != nil && run.Connection.PublicID != *f.ConnectionPublicID) ||
			(f.Status != nil && run.Status != *f.Status) ||
			(f.Environment != nil && run.Connection.Environment != *f.Environment) {
			continue
		}
		items = append(items, *run)
	}
	return page(items, p, func(r domain.ReconciliationRun) domain.Cursor {
		return domain.Cursor{Time: r.CreatedAt, ID: r.PublicID}
	}), nil
}

// UpdateRun writes the mutable fields of r.
func (s *Reconciliations) UpdateRun(_ context.Context, r *domain.ReconciliationRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	old, ok := s.runs[r.ID]
	if !ok {
		return domain.ErrNotFound
	}
	old.Status, old.RunCount, old.Summary, old.UnresolvedCount, old.Error = r.Status, r.RunCount, r.Summary, r.UnresolvedCount, r.Error
	old.StartedAt, old.FinishedAt, old.UpdatedAt = r.StartedAt, r.FinishedAt, s.now()
	r.UpdatedAt = old.UpdatedAt
	s.runs[r.ID] = old
	return nil
}

// ClaimRun moves a queued run of runCount to running.
func (s *Reconciliations) ClaimRun(_ context.Context, id int64, runCount int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return false, s.Err
	}
	r, ok := s.runs[id]
	if !ok || r.RunCount != runCount || r.Status != domain.RunQueued {
		return false, nil
	}
	now := s.now()
	r.Status, r.StartedAt, r.UpdatedAt = domain.RunRunning, &now, now
	s.runs[id] = r
	return true, nil
}

// TouchRun refreshes UpdatedAt of a running run of runCount.
func (s *Reconciliations) TouchRun(_ context.Context, id int64, runCount int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return false, s.Err
	}
	r, ok := s.runs[id]
	if !ok || r.RunCount != runCount || r.Status != domain.RunRunning {
		return false, nil
	}
	r.UpdatedAt = s.now()
	s.runs[id] = r
	return true, nil
}

// FailRun ends a queued or running run of runCount.
func (s *Reconciliations) FailRun(_ context.Context, id int64, runCount int, e domain.RunError) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return false, s.Err
	}
	r, ok := s.runs[id]
	if !ok || r.RunCount != runCount || !r.Status.Active() {
		return false, nil
	}
	now := s.now()
	r.Status, r.Error, r.Summary, r.FinishedAt, r.UpdatedAt = domain.RunFailed, &e, nil, &now, now
	s.runs[id] = r
	return true, nil
}

// SaveItems upserts items by reference and deletes the run's others.
func (s *Reconciliations) SaveItems(_ context.Context, runID int64, items []domain.ReconciliationItem) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return 0, s.Err
	}
	byRef := map[string]int64{}
	for id, it := range s.items {
		if it.RunID == runID {
			byRef[it.Reference] = id
		}
	}
	keep := map[int64]bool{}
	now := s.now()
	for _, in := range items {
		it := in
		it.RunID = runID
		if id, ok := byRef[in.Reference]; ok {
			old := s.items[id]
			it.ID, it.PublicID, it.CreatedAt = old.ID, old.PublicID, old.CreatedAt
			if old.Result == in.Result {
				it.Resolution = old.Resolution
			}
		} else {
			s.nextItem++
			it.ID, it.CreatedAt = s.nextItem, now
		}
		it.UpdatedAt = now
		s.items[it.ID] = it
		keep[it.ID] = true
	}
	unresolved := 0
	for id, it := range s.items {
		if it.RunID != runID {
			continue
		}
		if !keep[id] {
			delete(s.items, id)
			continue
		}
		if it.Result != domain.ResultMatched && it.Resolution == nil {
			unresolved++
		}
	}
	return unresolved, nil
}

func (s *Reconciliations) fillItem(it domain.ReconciliationItem) domain.ReconciliationItem {
	if r, ok := s.runs[it.RunID]; ok {
		it.RunPublicID = r.PublicID
	}
	if it.Keipay != nil {
		k := *it.Keipay
		for _, p := range s.payments.All() {
			if it.PaymentID != nil && p.ID == *it.PaymentID {
				k.App = s.payments.withApp(p).App
			}
		}
		it.Keipay = &k
	}
	if it.Resolution != nil {
		res := *it.Resolution
		res.ResolvedBy = s.userRef(res.ResolvedByID)
		it.Resolution = &res
	}
	return it
}

func (s *Reconciliations) runItems(runID int64, f domain.ReconciliationItemFilter) []domain.ReconciliationItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.ReconciliationItem
	for _, it := range s.items {
		if it.RunID != runID || (f.Result != nil && it.Result != *f.Result) ||
			(f.Resolved != nil && (it.Resolution != nil) != *f.Resolved) {
			continue
		}
		out = append(out, s.fillItem(it))
	}
	return out
}

// ListItems returns one page of a run's items, newest first.
func (s *Reconciliations) ListItems(_ context.Context, runID int64, f domain.ReconciliationItemFilter, p domain.Page) (domain.PageResult[domain.ReconciliationItem], error) {
	if s.Err != nil {
		return domain.PageResult[domain.ReconciliationItem]{}, s.Err
	}
	return page(s.runItems(runID, f), p, func(it domain.ReconciliationItem) domain.Cursor {
		return domain.Cursor{Time: it.CreatedAt, ID: it.PublicID}
	}), nil
}

// StreamItems calls fn with every matching item, newest first.
func (s *Reconciliations) StreamItems(ctx context.Context, runID int64, f domain.ReconciliationItemFilter, fn func(*domain.ReconciliationItem) error) error {
	res, err := s.ListItems(ctx, runID, f, domain.Page{Limit: len(s.items) + 1})
	if err != nil {
		return err
	}
	for i := range res.Items {
		if err := fn(&res.Items[i]); err != nil {
			return err
		}
	}
	return nil
}

// GetItem loads an item by public id.
func (s *Reconciliations) GetItem(_ context.Context, publicID string) (*domain.ReconciliationItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return nil, s.Err
	}
	for _, it := range s.items {
		if it.PublicID == publicID {
			out := s.fillItem(it)
			return &out, nil
		}
	}
	return nil, domain.ErrNotFound
}

// GetItemForUpdate is GetItem.
func (s *Reconciliations) GetItemForUpdate(ctx context.Context, publicID string) (*domain.ReconciliationItem, error) {
	return s.GetItem(ctx, publicID)
}

// ResolveItem stores the resolution of it.
func (s *Reconciliations) ResolveItem(_ context.Context, it *domain.ReconciliationItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	old, ok := s.items[it.ID]
	if !ok {
		return domain.ErrNotFound
	}
	res := *it.Resolution
	old.Resolution, old.UpdatedAt = &res, s.now()
	s.items[it.ID] = old
	return nil
}

// KeipayPayments returns the connection's reports paid within [from, to)
// and its payments whose reference is in refs.
func (s *Reconciliations) KeipayPayments(_ context.Context, connectionID int64, from, to time.Time, refs []string) ([]domain.KeipayPayment, error) {
	if s.Err != nil {
		return nil, s.Err
	}
	reports := map[int64]domain.PaymentReport{}
	for _, r := range s.reports.All() {
		reports[r.PaymentID] = r
	}
	var out []domain.KeipayPayment
	for _, p := range s.payments.All() {
		if p.ConnectionID == nil || *p.ConnectionID != connectionID {
			continue
		}
		r, hasReport := reports[p.ID]
		inWindow := hasReport && !r.PaidAt.Before(from) && r.PaidAt.Before(to)
		if !inWindow && !slices.Contains(refs, p.ExternalID) {
			continue
		}
		k := domain.KeipayPayment{PaymentID: p.ID, PaymentPublicID: p.PublicID, App: s.payments.withApp(p).App,
			Reference: p.ExternalID, Status: p.Status, Amount: p.Amount, Currency: p.Currency, PaidAt: p.PaidAt}
		if hasReport {
			paidAt := r.PaidAt
			k.Amount, k.Fee, k.PaidAt = r.Gross, r.Fee, &paidAt
		}
		out = append(out, k)
	}
	slices.SortFunc(out, func(a, b domain.KeipayPayment) int { return cmp.Compare(a.PaymentID, b.PaymentID) })
	return out, nil
}

// Runs returns every run, oldest first.
func (s *Reconciliations) Runs() []domain.ReconciliationRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := slices.Collect(maps.Values(s.runs))
	slices.SortFunc(out, func(a, b domain.ReconciliationRun) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

// Items returns every item of the run, by reference.
func (s *Reconciliations) Items(runID int64) []domain.ReconciliationItem {
	out := s.runItems(runID, domain.ReconciliationItemFilter{})
	slices.SortFunc(out, func(a, b domain.ReconciliationItem) int { return cmp.Compare(a.Reference, b.Reference) })
	return out
}

// SetRunUpdatedAt backdates a run, as if it had not changed since at.
func (s *Reconciliations) SetRunUpdatedAt(id int64, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.runs[id]
	r.UpdatedAt = at
	s.runs[id] = r
}
