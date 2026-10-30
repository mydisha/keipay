package memstore

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"sync"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// Reports is an in-memory domain.PaymentReportRepository with the upsert
// rules of payment_reports. List and Stream return every report newest
// first without filtering, and Summary returns nothing: tests of filters and
// totals run against Postgres. Set UpsertErr to fail upserts and StreamErr
// to fail Stream after its last row.
type Reports struct {
	mu        sync.Mutex
	byPayment map[int64]domain.PaymentReport

	UpsertErr error
	StreamErr error
}

// NewReports returns an empty store.
func NewReports() *Reports { return &Reports{byPayment: map[int64]domain.PaymentReport{}} }

func (s *Reports) snapshot() func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	saved := maps.Clone(s.byPayment)
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.byPayment = saved
	}
}

// Upsert stores r, or merges it into the payment's report: the status only
// moves to SETTLED, the first settled time and the first known fee stay.
func (s *Reports) Upsert(_ context.Context, r *domain.PaymentReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.UpsertErr != nil {
		return s.UpsertErr
	}
	old, ok := s.byPayment[r.PaymentID]
	if !ok {
		s.byPayment[r.PaymentID] = *r
		return nil
	}
	if r.Status == domain.PaymentStatusSettled {
		old.Status = domain.PaymentStatusSettled
	}
	if old.SettledAt == nil {
		old.SettledAt = r.SettledAt
	}
	if old.Fee == nil {
		old.Fee, old.Net = r.Fee, r.Net
	}
	s.byPayment[r.PaymentID] = old
	return nil
}

// FillFee sets fee and net of the payment's report while its fee is unknown.
func (s *Reports) FillFee(_ context.Context, paymentID, fee int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.byPayment[paymentID]
	if !ok || r.Fee != nil {
		return false, nil
	}
	net := r.Gross - fee
	r.Fee, r.Net = &fee, &net
	s.byPayment[paymentID] = r
	return true, nil
}

// List returns one page of every report, newest first.
func (s *Reports) List(_ context.Context, _ domain.PaymentReportFilter, p domain.Page) (domain.PageResult[domain.PaymentReport], error) {
	all := s.All()
	slices.Reverse(all)
	if len(all) > p.Limit {
		all = all[:p.Limit]
	}
	return domain.PageResult[domain.PaymentReport]{Items: all}, nil
}

// Summary returns no rows.
func (s *Reports) Summary(context.Context, domain.PaymentReportFilter, domain.ReportInterval) ([]domain.PaymentReportSummaryRow, error) {
	return []domain.PaymentReportSummaryRow{}, nil
}

// Stream calls fn with every report, newest first.
func (s *Reports) Stream(_ context.Context, _ domain.PaymentReportFilter, fn func(*domain.PaymentReport) error) error {
	all := s.All()
	slices.Reverse(all)
	for i := range all {
		if err := fn(&all[i]); err != nil {
			return err
		}
	}
	return s.StreamErr
}

// All returns every report, oldest payment first.
func (s *Reports) All() []domain.PaymentReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := slices.Collect(maps.Values(s.byPayment))
	slices.SortFunc(out, func(a, b domain.PaymentReport) int { return cmp.Compare(a.PaymentID, b.PaymentID) })
	return out
}
