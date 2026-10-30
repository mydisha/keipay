package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/provider"
	"github.com/mydisha/keipay/backend/internal/testutil/memstore"
)

// checkFixture wires the checkout check to the real webhook service over
// in-memory stores and the fake provider, whose Checkouts the tests set.
type checkFixture struct {
	*connFixture
	hooks   *memstore.Webhooks
	reports *memstore.Reports
	checks  *memstore.CheckoutChecks
	svc     *CheckoutCheckService
	conn    *domain.Connection
	app     *domain.App
}

func newCheckFixture(t *testing.T) *checkFixture {
	t.Helper()
	f := &checkFixture{connFixture: newConnFixture(t), reports: memstore.NewReports()}
	f.hooks = memstore.NewWebhooks(f.apps, f.payments, f.conns, f.clock.Now)
	f.checks = memstore.NewCheckoutChecks(f.payments)
	tx := memstore.NewTx(f.apps, f.payments, f.conns, f.hooks, f.reports, f.audit)
	webhooks := NewWebhookService(WebhookDeps{
		Payments: f.payments, Reports: f.reports, Inbound: f.hooks.Inbound(), Events: f.hooks.Events(),
		Endpoints: f.hooks.Endpoints(), Deliveries: f.hooks.Deliveries(), Tx: tx, Enqueuer: &recordingEnqueuer{},
	})
	webhooks.now = f.clock.Now
	f.svc = NewCheckoutCheckService(CheckoutCheckDeps{
		Checks: f.checks, Connections: f.conns, Registry: f.registry, Sealer: f.cipher, Callbacks: webhooks,
		Log: f.connFixture.svc.log,
	})
	f.svc.now = f.clock.Now
	f.conn = f.addConnection(t, "fake-test", domain.EnvTest)
	f.app = &domain.App{PublicID: id.New(id.App), Slug: "acme", Name: "Acme", Environment: domain.EnvTest, Status: domain.AppActive}
	if err := f.apps.Create(context.Background(), f.app); err != nil {
		t.Fatal(err)
	}
	f.fake.Checkouts = map[string]provider.InboundEvent{}
	return f
}

// pending stores a pending payment of the fixture's app whose checkout
// (provider id inv-<ref>) expired ago before now.
func (f *checkFixture) pending(t *testing.T, ref string, ago time.Duration) *domain.Payment {
	t.Helper()
	exp := f.clock.Now().Add(-ago)
	p := &domain.Payment{PublicID: id.New(id.Payment), AppID: f.app.ID, OrderID: "order-" + ref, ExternalID: ref,
		ConnectionID: &f.conn.ID, Provider: f.conn.Provider, XenditInvoiceID: "inv-" + ref, Amount: 30000, Currency: "IDR",
		Status: domain.PaymentStatusPending, ExpiresAt: &exp}
	if err := f.payments.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

// gateway makes the fake answer the payment's checkout with status.
func (f *checkFixture) gateway(p *domain.Payment, status domain.PaymentStatus) {
	f.fake.Checkouts[p.XenditInvoiceID] = provider.InboundEvent{EventKey: p.XenditInvoiceID, Reference: p.ExternalID,
		ProviderPaymentID: p.XenditInvoiceID, ProviderStatus: string(status), Status: status, Amount: p.Amount, Currency: p.Currency}
}

func (f *checkFixture) status(t *testing.T, p *domain.Payment) domain.PaymentStatus {
	t.Helper()
	got, err := f.payments.GetByID(context.Background(), p.AppID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got.Status
}

func (f *checkFixture) run(t *testing.T) int {
	t.Helper()
	n, err := f.svc.CheckOverdue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// Regression: an invoice that expired without its callback stayed PENDING
// for good. Reading it back applies EXPIRED through the callback path: the
// inbound row says it was read back, and the app gets payment.expired.
func TestCheckOverdueAppliesTheGatewayStatus(t *testing.T) {
	f := newCheckFixture(t)
	p := f.pending(t, "kp_pay_a", 5*time.Minute)
	f.gateway(p, domain.PaymentStatusExpired)

	if n := f.run(t); n != 1 {
		t.Fatalf("checked %d, want 1", n)
	}
	if got := f.status(t, p); got != domain.PaymentStatusExpired {
		t.Fatalf("status = %s", got)
	}
	inbound := f.hooks.AllInbound()
	if len(inbound) != 1 || inbound[0].Source != domain.InboundSourcePoll || inbound[0].Result != domain.InboundApplied ||
		!inbound[0].SignatureValid || inbound[0].PaymentID == nil || *inbound[0].PaymentID != p.ID {
		t.Fatalf("inbound = %+v", inbound)
	}
	events := f.hooks.AllEvents()
	if len(events) != 1 || events[0].Type != domain.EventPaymentExpired {
		t.Fatalf("events = %+v", events)
	}
	if at, ok := f.checks.CheckedAt(p.ID); !ok || !at.Equal(f.clock.Now().UTC()) {
		t.Fatalf("checked at %v, %v", at, ok)
	}
	if calls := f.fake.GetCalls(); len(calls) != 1 || calls[0] != "inv-kp_pay_a" {
		t.Fatalf("gateway calls = %v", calls)
	}
}

// A PAID callback that was lost is recovered the same way, with its report.
func TestCheckOverdueRecoversAPayment(t *testing.T) {
	f := newCheckFixture(t)
	p := f.pending(t, "kp_pay_paid", 5*time.Minute)
	f.gateway(p, domain.PaymentStatusPaid)

	f.run(t)
	if got := f.status(t, p); got != domain.PaymentStatusPaid {
		t.Fatalf("status = %s", got)
	}
	if reports := f.reports.All(); len(reports) != 1 || reports[0].PaymentID != p.ID || reports[0].Gross != p.Amount {
		t.Fatalf("reports = %+v", reports)
	}
	if events := f.hooks.AllEvents(); len(events) != 1 || events[0].Type != domain.EventPaymentPaid {
		t.Fatalf("events = %+v", events)
	}
}

// The gateway gets its grace period to call back first, and a checkout
// expired longer ago than the window is no longer asked about.
func TestCheckOverdueSkipsRecentAndOldExpiries(t *testing.T) {
	f := newCheckFixture(t)
	recent := f.pending(t, "kp_pay_recent", time.Minute)
	old := f.pending(t, "kp_pay_old", 73*time.Hour)
	edge := f.pending(t, "kp_pay_edge", checkoutGrace)
	for _, p := range []*domain.Payment{recent, old, edge} {
		f.gateway(p, domain.PaymentStatusExpired)
	}

	if n := f.run(t); n != 1 {
		t.Fatalf("checked %d, want only the one past the grace period", n)
	}
	if calls := f.fake.GetCalls(); len(calls) != 1 || calls[0] != edge.XenditInvoiceID {
		t.Fatalf("gateway calls = %v", calls)
	}
	if f.status(t, recent) != domain.PaymentStatusPending || f.status(t, old) != domain.PaymentStatusPending {
		t.Fatal("a skipped payment changed")
	}
}

// A checkout the gateway still shows as pending writes nothing and is asked
// again only after the recheck interval.
func TestCheckOverdueWaitsWhileTheGatewayIsPending(t *testing.T) {
	f := newCheckFixture(t)
	p := f.pending(t, "kp_pay_slow", 5*time.Minute)
	f.gateway(p, domain.PaymentStatusPending)

	f.run(t)
	if len(f.hooks.AllInbound()) != 0 || f.status(t, p) != domain.PaymentStatusPending {
		t.Fatal("a pending answer was recorded")
	}
	f.clock.Advance(checkoutRecheck - time.Second)
	if n := f.run(t); n != 0 {
		t.Fatalf("asked again after %v", checkoutRecheck-time.Second)
	}
	f.clock.Advance(time.Second)
	f.gateway(p, domain.PaymentStatusExpired)
	if n := f.run(t); n != 1 || f.status(t, p) != domain.PaymentStatusExpired {
		t.Fatalf("recheck: checked %d, status %s", n, f.status(t, p))
	}
	if calls := len(f.fake.GetCalls()); calls != 2 {
		t.Fatalf("gateway calls = %d", calls)
	}
}

// A failing gateway is logged and asked again later; the other payments of
// the batch are still checked.
func TestCheckOverdueKeepsGoingAfterAFailure(t *testing.T) {
	f := newCheckFixture(t)
	missing := f.pending(t, "kp_pay_missing", 10*time.Minute)
	p := f.pending(t, "kp_pay_ok", 5*time.Minute)
	f.gateway(p, domain.PaymentStatusExpired)

	if n := f.run(t); n != 2 {
		t.Fatalf("checked %d, want 2", n)
	}
	if f.status(t, p) != domain.PaymentStatusExpired || f.status(t, missing) != domain.PaymentStatusPending {
		t.Fatal("statuses after a partial failure")
	}
	if _, ok := f.checks.CheckedAt(missing.ID); !ok {
		t.Fatal("the failed payment was not marked checked, so it would be asked every minute")
	}
	warned := f.logs.FilterMessage("checkout check failed").All()
	if len(warned) != 1 || !strings.Contains(warned[0].ContextMap()["error"].(string), provider.ErrNotFound.Error()) {
		t.Fatalf("logs = %+v", warned)
	}

	f.fake.GetErr = provider.NewStatusError(provider.ErrRetryable, 503, "down")
	f.clock.Advance(checkoutRecheck)
	if n := f.run(t); n != 1 {
		t.Fatalf("recheck after the failure: checked %d", n)
	}
}

// An answer for another reference is not applied to this payment.
func TestCheckOverdueRefusesAnotherReference(t *testing.T) {
	f := newCheckFixture(t)
	p := f.pending(t, "kp_pay_mine", 5*time.Minute)
	f.fake.Checkouts[p.XenditInvoiceID] = provider.InboundEvent{EventKey: p.XenditInvoiceID, Reference: "kp_pay_other",
		ProviderStatus: "EXPIRED", Status: domain.PaymentStatusExpired}

	f.run(t)
	if f.status(t, p) != domain.PaymentStatusPending || len(f.hooks.AllInbound()) != 0 {
		t.Fatal("an answer for another reference was applied")
	}
	if len(f.logs.FilterMessage("checkout check failed").All()) != 1 {
		t.Fatal("the mismatch was not logged")
	}
}

// A callback that arrives after the read back is a duplicate of it.
func TestCheckOverdueThenCallbackIsDuplicate(t *testing.T) {
	f := newCheckFixture(t)
	p := f.pending(t, "kp_pay_dup", 5*time.Minute)
	f.gateway(p, domain.PaymentStatusExpired)
	f.run(t)

	ev := f.fake.Checkouts[p.XenditInvoiceID]
	ev.Source = domain.InboundSourceWebhook
	if err := f.svc.callbacks.Process(context.Background(), f.conn, ev, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	inbound := f.hooks.AllInbound()
	if len(inbound) != 2 || inbound[1].Result != domain.InboundDuplicate || inbound[1].Source != domain.InboundSourceWebhook {
		t.Fatalf("inbound = %+v", inbound)
	}
	if len(f.hooks.AllEvents()) != 1 {
		t.Fatal("the late callback sent a second event")
	}
}

// Connections whose provider cannot read checkouts are skipped, still
// marked so they are not listed every minute.
func TestCheckOverdueSkipsProvidersWithoutTheCapability(t *testing.T) {
	f := newCheckFixture(t)
	f.fake.Caps.ReadCheckout = false
	p := f.pending(t, "kp_pay_nocap", 5*time.Minute)
	f.gateway(p, domain.PaymentStatusExpired)

	f.run(t)
	if len(f.fake.GetCalls()) != 0 || f.status(t, p) != domain.PaymentStatusPending {
		t.Fatal("a provider without read_checkout was asked")
	}
	if _, ok := f.checks.CheckedAt(p.ID); !ok {
		t.Fatal("not marked checked")
	}
}

// Listing failures are returned; there is nothing to mark.
func TestCheckOverdueListFailure(t *testing.T) {
	f := newCheckFixture(t)
	f.svc.checks = failingChecks{}
	if _, err := f.svc.CheckOverdue(context.Background()); err == nil {
		t.Fatal("a listing failure was swallowed")
	}
}

type failingChecks struct{ domain.CheckoutCheckRepository }

func (failingChecks) ListOverdue(context.Context, domain.OverdueCheckoutQuery) ([]domain.OverdueCheckout, error) {
	return nil, errors.New("pool closed")
}
