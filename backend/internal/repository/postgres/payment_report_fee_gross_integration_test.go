package postgres_test

import (
	"testing"
)

// A PAID callback without a fee writes the report with the callback's
// paid_amount as gross; a later SETTLED callback that brings the fee but a
// different paid_amount must still be applied. The upsert keeps the first
// gross, so the fee it fills in has to be netted against that gross:
// net = gross - fee must hold (payment_reports_net_check), otherwise the
// whole callback transaction fails and the gateway retries it forever.
func TestIntegrationReportFeeFilledAgainstKeptGross(t *testing.T) {
	f := newFixture(t)
	svc := f.svc(&stubEnqueuer{})

	paid := callback(f.payment, "PAID")
	paid.Fee = nil
	if err := f.process(svc, f.conn, paid); err != nil {
		t.Fatal(err)
	}

	settled := callback(f.payment, "SETTLED")
	settled.PaidAmount = f.payment.Amount + 100
	settled.SettlementDate = "2026-10-02T00:00:00Z"
	if err := f.process(svc, f.conn, settled); err != nil {
		t.Fatalf("SETTLED with a fee and another paid_amount was refused: %v", err)
	}
	n, r, _, _ := f.report(t, f.payment)
	if n != 1 || r.status != "SETTLED" || r.fee == nil || r.net == nil || *r.net != r.gross-*r.fee {
		t.Fatalf("report = rows %d %+v (fee %v net %v)", n, r, r.fee, r.net)
	}
}
