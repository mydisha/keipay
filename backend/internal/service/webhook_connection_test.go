package service

import (
	"context"
	"errors"
	"testing"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// T-18 / AC-20: a callback only updates payments of its own connection.
func TestProcessPaymentOfAnotherConnectionIsNotApplied(t *testing.T) {
	other := &domain.Connection{ID: 8, PublicID: "con_01JBKQ7Z8M4X2Y6V3N9T5R1W0D", Provider: "xendit"}
	for name, setup := range map[string]func(p *domain.Payment){
		"other connection": func(*domain.Payment) {},
		"no connection":    func(p *domain.Payment) { p.ConnectionID = nil },
	} {
		t.Run(name, func(t *testing.T) {
			svc, pr, lr, wr, eq := newSvc()
			setup(pr.byExternal["acme:order-1"])
			cb := xenditCallback{ID: "inv_1", ExternalID: "acme:order-1", Status: "PAID", Amount: 5000}
			conn := other
			if name == "no connection" {
				conn = testConn
			}
			err := svc.Process(context.Background(), conn, cb.event(), []byte(`{}`))
			var mismatch *ConnectionMismatchError
			if !errors.Is(err, ErrCallbackMismatch) || !errors.As(err, &mismatch) || mismatch.ConnectionID != conn.ID {
				t.Fatalf("err = %v, want a connection mismatch", err)
			}
			if len(pr.updated) != 0 || lr.created != 0 || len(wr.deliveries) != 0 || len(eq.enqueued) != 0 {
				t.Fatalf("applied: updated=%v ledger=%d deliveries=%d", pr.updated, lr.created, len(wr.deliveries))
			}
			if wr.events["inv_1:PAID"] || svc.idemp.(*memIdemp).marked["inv_1:PAID"] {
				t.Fatal("a mismatch consumed the dedupe key")
			}
			if err := svc.Process(context.Background(), conn, cb.event(), []byte(`{}`)); !errors.Is(err, ErrCallbackMismatch) {
				t.Fatalf("retry of a mismatch: %v", err)
			}
		})
	}
}

// Regression: a callback for B's payment verified at A must not make B's own
// callback for the same event look like a duplicate.
func TestProcessMismatchDoesNotBlockOwnConnection(t *testing.T) {
	other := &domain.Connection{ID: 8, PublicID: "con_01JBKQ7Z8M4X2Y6V3N9T5R1W0D", Provider: "xendit"}
	svc, pr, lr, _, eq := newSvc()
	cb := xenditCallback{ID: "inv_1", ExternalID: "acme:order-1", Status: "PAID", Amount: 5000}
	if err := svc.Process(context.Background(), other, cb.event(), []byte(`{}`)); !errors.Is(err, ErrCallbackMismatch) {
		t.Fatalf("foreign callback: %v", err)
	}
	if err := svc.Process(context.Background(), testConn, cb.event(), []byte(`{}`)); err != nil {
		t.Fatalf("own callback: %v", err)
	}
	if got := pr.byExternal["acme:order-1"].Status; got != domain.PaymentStatusPaid || lr.created != 1 || len(eq.enqueued) != 1 {
		t.Fatalf("own callback not applied: status=%s ledger=%d enqueued=%d", got, lr.created, len(eq.enqueued))
	}
	if err := svc.Process(context.Background(), testConn, cb.event(), []byte(`{}`)); err != nil || lr.created != 1 || len(eq.enqueued) != 1 {
		t.Fatalf("duplicate after apply: err=%v ledger=%d enqueued=%d", err, lr.created, len(eq.enqueued))
	}
}

// T-18 / AC-21: a disabled connection still applies its callbacks.
func TestProcessDisabledConnectionStillApplies(t *testing.T) {
	svc, pr, lr, _, _ := newSvc()
	disabled := *testConn
	disabled.Status = domain.ConnectionDisabled
	cb := xenditCallback{ID: "inv_1", ExternalID: "acme:order-1", Status: "PAID", Amount: 5000, FeesPaid: 100}
	if err := svc.Process(context.Background(), &disabled, cb.event(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if len(pr.updated) != 1 || lr.created != 1 || lr.entries[0].XenditFee != 100 {
		t.Fatalf("updated=%v ledger=%+v", pr.updated, lr.entries)
	}
}
