package domain

import (
	"testing"
	"time"
)

func TestDashboardRange(t *testing.T) {
	cases := []struct {
		in    DashboardRange
		def   DashboardRange
		valid bool
		days  int
	}{
		{"", DashboardRange7d, true, 7},
		{"7d", DashboardRange7d, true, 7},
		{"30d", DashboardRange30d, true, 30},
		{"3d", "3d", false, 0},
		{"7D", "7D", false, 0},
		{" 7d", " 7d", false, 0},
	}
	for _, tc := range cases {
		r := tc.in.OrDefault()
		if r != tc.def || r.Valid() != tc.valid || r.Days() != tc.days {
			t.Errorf("%q: default %q valid %v days %d", tc.in, r, r.Valid(), r.Days())
		}
	}
}

// The window ends at the next WIB midnight, so today counts, and the
// previous period has the same length.
func TestDashboardWindowAt(t *testing.T) {
	utc := func(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, time.UTC) }
	cases := []struct {
		name                   string
		r                      DashboardRange
		now                    time.Time
		previousFrom, from, to time.Time
	}{
		{"afternoon WIB", DashboardRange7d, utc(2026, 10, 29, 10), utc(2026, 10, 15, 17), utc(2026, 10, 22, 17), utc(2026, 10, 29, 17)},
		{"30 days", DashboardRange30d, utc(2026, 10, 29, 10), utc(2026, 8, 30, 17), utc(2026, 9, 29, 17), utc(2026, 10, 29, 17)},
		{"just before WIB midnight", DashboardRange7d, utc(2026, 10, 29, 16).Add(59*time.Minute + 59*time.Second),
			utc(2026, 10, 15, 17), utc(2026, 10, 22, 17), utc(2026, 10, 29, 17)},
		{"at WIB midnight", DashboardRange7d, utc(2026, 10, 29, 17), utc(2026, 10, 16, 17), utc(2026, 10, 23, 17), utc(2026, 10, 30, 17)},
		{"month boundary", DashboardRange7d, utc(2026, 11, 1, 2), utc(2026, 10, 18, 17), utc(2026, 10, 25, 17), utc(2026, 11, 1, 17)},
	}
	for _, tc := range cases {
		w := tc.r.WindowAt(tc.now)
		if !w.PreviousFrom.Equal(tc.previousFrom) || !w.From.Equal(tc.from) || !w.To.Equal(tc.to) {
			t.Errorf("%s: window %v..%v..%v, want %v..%v..%v", tc.name, w.PreviousFrom, w.From, w.To, tc.previousFrom, tc.from, tc.to)
		}
		if w.From.Location() != time.UTC || w.Environment != nil {
			t.Errorf("%s: window %+v not UTC or carries an environment", tc.name, w)
		}
	}
}

func TestDashboardRates(t *testing.T) {
	if r := (DashboardPaymentTotals{}).SuccessRate(); r != nil {
		t.Fatalf("empty totals rate = %v, want nil", *r)
	}
	if r := (DashboardPaymentTotals{Created: 3, Pending: 3}).SuccessRate(); r != nil {
		t.Fatalf("only pending rate = %v, want nil", *r)
	}
	tot := DashboardPaymentTotals{Paid: 1, Settled: 1, Expired: 1, Failed: 1, Pending: 4}
	if tot.Successful() != 2 || *tot.SuccessRate() != 0.5 {
		t.Fatalf("successful %d rate %v", tot.Successful(), *tot.SuccessRate())
	}
	if r := (DashboardDeliveryCounts{Total: 2, Pending: 2}).SuccessRate(); r != nil {
		t.Fatalf("pending deliveries rate = %v, want nil", *r)
	}
	if r := (DashboardDeliveryCounts{Delivered: 3, Failed: 1}).SuccessRate(); *r != 0.75 {
		t.Fatalf("delivery rate = %v", *r)
	}
}

// Every result is listed; stale and duplicate are neither applied nor
// problems.
func TestNewDashboardInbound(t *testing.T) {
	in := NewDashboardInbound(nil)
	if len(in.ByResult) != len(InboundResults()) || in.Total != 0 || in.Applied != 0 || in.Problems != 0 {
		t.Fatalf("empty inbound = %+v", in)
	}
	in = NewDashboardInbound(map[InboundResult]int64{
		InboundApplied: 5, InboundStale: 1, InboundDuplicate: 2, InboundMismatch: 1, InboundUnmapped: 1,
		InboundInvalidSignature: 1, InboundInvalidPayload: 1, InboundError: 1, "other": 9,
	})
	if in.Total != 13 || in.Applied != 5 || in.Problems != 5 || len(in.ByResult) != len(InboundResults()) {
		t.Fatalf("inbound = %+v", in)
	}
	for _, r := range InboundResults() {
		if !r.Valid() {
			t.Fatalf("%s is not a valid result", r)
		}
	}
}
