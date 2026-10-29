package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/testutil/memstore"
)

func tptr(t time.Time) *time.Time { return &t }

// Range rules of the list, summary and export.
func TestPaymentReportRanges(t *testing.T) {
	svc := NewPaymentReportService(memstore.NewReports())
	ctx := context.Background()
	from := time.Date(2026, 9, 30, 17, 0, 0, 0, time.UTC)
	f := func(from, to *time.Time) domain.PaymentReportFilter {
		return domain.PaymentReportFilter{From: from, To: to}
	}

	expect := func(t *testing.T, err error, want ...domain.FieldError) {
		t.Helper()
		got := fieldsOf(t, err)
		if len(got) != len(want) {
			t.Fatalf("fields = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("fields = %v, want %v", got, want)
			}
		}
	}
	after := domain.FieldError{Field: "to", Rule: "after"}
	maxRange := domain.FieldError{Field: "to", Rule: "max_range"}

	_, err := svc.List(ctx, f(&from, &from), domain.PageRequest{})
	expect(t, err, after)
	if _, err := svc.List(ctx, f(&from, nil), domain.PageRequest{}); err != nil {
		t.Fatalf("open range: %v", err)
	}
	if _, err := svc.List(ctx, f(nil, nil), domain.PageRequest{Cursor: "nope"}); !errors.Is(err, domain.ErrInvalidCursor) {
		t.Fatalf("cursor: %v", err)
	}
	called := false
	err = svc.Export(ctx, f(&from, tptr(from.Add(-time.Hour))), func(*domain.PaymentReport) error { called = true; return nil })
	expect(t, err, after)
	if called {
		t.Fatal("export wrote rows for an invalid range")
	}

	_, err = svc.Summary(ctx, f(nil, nil), "")
	expect(t, err, domain.FieldError{Field: "from", Rule: "required"}, domain.FieldError{Field: "to", Rule: "required"})
	_, err = svc.Summary(ctx, f(&from, tptr(from.Add(time.Hour))), "week")
	expect(t, err, domain.FieldError{Field: "interval", Rule: "oneof"})
	_, err = svc.Summary(ctx, f(&from, &from), domain.ReportIntervalDay)
	expect(t, err, after)
	day := 24 * time.Hour
	for _, tc := range []struct {
		interval domain.ReportInterval
		span     time.Duration
		ok       bool
	}{
		{domain.ReportIntervalDay, 92 * day, true},
		{domain.ReportIntervalDay, 93 * day, false},
		{domain.ReportIntervalTotal, 366 * day, true},
		{domain.ReportIntervalTotal, 367 * day, false},
		{"", 367 * day, false},
		{domain.ReportIntervalMonth, 366 * day, true},
		{domain.ReportIntervalMonth, 366*day + time.Second, false},
	} {
		_, err := svc.Summary(ctx, f(&from, tptr(from.Add(tc.span))), tc.interval)
		if tc.ok && err != nil {
			t.Fatalf("%s over %v: %v", tc.interval, tc.span, err)
		}
		if !tc.ok {
			expect(t, err, maxRange)
		}
	}
}
