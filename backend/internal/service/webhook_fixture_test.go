package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/provider"
	"github.com/mydisha/keipay/backend/internal/provider/xendit"
	"github.com/mydisha/keipay/backend/internal/testutil/memstore"
)

type enqueued struct {
	id      int64
	attempt int
	manual  bool
	at      time.Time
}

// recordingEnqueuer records every attempt it is asked to queue; set err to
// make the queue unavailable.
type recordingEnqueuer struct {
	calls []enqueued
	err   error
}

func (e *recordingEnqueuer) EnqueueDelivery(_ context.Context, id int64, attempt int, manual bool, at time.Time) error {
	if e.err != nil {
		return e.err
	}
	e.calls = append(e.calls, enqueued{id: id, attempt: attempt, manual: manual, at: at})
	return nil
}

// webhookFixture wires the webhook, endpoint and delivery services to
// in-memory stores that roll back together. conn is the connection payment
// was created through; payment belongs to app.
type webhookFixture struct {
	clock      *memstore.Clock
	apps       *memstore.Apps
	payments   *memstore.Payments
	conns      *memstore.Connections
	hooks      *memstore.Webhooks
	reports    *memstore.Reports
	audit      *memstore.Audit
	tx         *memstore.Tx
	enq        *recordingEnqueuer
	cipher     *recordingCipher
	svc        *WebhookService
	endpoints  *WebhookEndpointService
	deliveries *DeliveryService
	conn       *domain.Connection
	app        *domain.App
	payment    *domain.Payment
}

var fixtureStart = time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)

func newWebhookFixture(t *testing.T) *webhookFixture {
	t.Helper()
	f := &webhookFixture{clock: memstore.NewClock(fixtureStart), reports: memstore.NewReports(), audit: &memstore.Audit{},
		enq: &recordingEnqueuer{}, cipher: &recordingCipher{}}
	f.apps = memstore.NewApps(f.clock.Now)
	f.payments = memstore.NewPayments(f.apps, f.clock.Now)
	f.conns = memstore.NewConnections(f.apps, f.clock.Now)
	f.hooks = memstore.NewWebhooks(f.apps, f.payments, f.conns, f.clock.Now)
	f.tx = memstore.NewTx(f.apps, f.payments, f.conns, f.hooks, f.reports, f.audit)
	auditSvc := NewAuditService(f.audit)
	auditSvc.now = f.clock.Now
	f.svc = NewWebhookService(WebhookDeps{
		Payments: f.payments, Reports: f.reports, Inbound: f.hooks.Inbound(), Events: f.hooks.Events(),
		Endpoints: f.hooks.Endpoints(), Deliveries: f.hooks.Deliveries(), Tx: f.tx, Enqueuer: f.enq,
	})
	f.svc.now = f.clock.Now
	f.endpoints = NewWebhookEndpointService(WebhookEndpointDeps{
		Apps: f.apps, Endpoints: f.hooks.Endpoints(), Events: f.hooks.Events(), Deliveries: f.hooks.Deliveries(),
		Cipher: f.cipher, Tx: f.tx, Audit: auditSvc, Enqueuer: f.enq,
	})
	f.endpoints.now = f.clock.Now
	f.deliveries = NewDeliveryService(DeliveryDeps{
		Deliveries: f.hooks.Deliveries(), Endpoints: f.hooks.Endpoints(), Tx: f.tx, Enqueuer: f.enq, Audit: auditSvc,
	})
	f.deliveries.now = f.clock.Now
	f.conn = f.addConnection(t, "xendit-a")
	f.app = f.addApp(t, "acme", domain.EnvTest)
	f.payment = f.addPayment(t, f.app, f.conn, "acme:order-1", 5000)
	return f
}

func (f *webhookFixture) addConnection(t *testing.T, name string) *domain.Connection {
	t.Helper()
	c := &domain.Connection{PublicID: id.New(id.Connection), MerchantID: memstore.DefaultMerchantID, Provider: "xendit",
		Name: name, Environment: domain.EnvTest, Status: domain.ConnectionActive}
	if err := f.conns.Create(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	return c
}

func (f *webhookFixture) addApp(t *testing.T, slug string, env domain.Environment) *domain.App {
	t.Helper()
	a := &domain.App{PublicID: id.New(id.App), Slug: slug, Name: "App " + slug, Environment: env, Status: domain.AppActive}
	if err := f.apps.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a
}

// addPayment stores a pending IDR payment of app made through conn (nil for
// one made before connections) with the gateway reference ref.
func (f *webhookFixture) addPayment(t *testing.T, app *domain.App, conn *domain.Connection, ref string, amount int64) *domain.Payment {
	t.Helper()
	p := &domain.Payment{PublicID: id.New(id.Payment), AppID: app.ID, OrderID: "order-" + ref, ExternalID: ref,
		Amount: amount, Currency: "IDR", Status: domain.PaymentStatusPending}
	if conn != nil {
		p.ConnectionID, p.Provider = &conn.ID, conn.Provider
	}
	if err := f.payments.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

// addEndpoint creates an endpoint of app through the service.
func (f *webhookFixture) addEndpoint(t *testing.T, app *domain.App, in CreateWebhookEndpointInput) *EndpointWithSecret {
	t.Helper()
	if in.URL == "" {
		in.URL = "https://hooks.example.com/" + strings.ToLower(id.New(id.WebhookEndpoint)[20:])
	}
	if in.Events == nil {
		in.Events = []string{"*"}
	}
	e, err := f.endpoints.Create(adminCtx(), app.PublicID, in)
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	return e
}

func (f *webhookFixture) paymentStatus(t *testing.T, p *domain.Payment) domain.PaymentStatus {
	t.Helper()
	got, err := f.payments.GetByPublicID(context.Background(), p.AppID, p.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	return got.Status
}

func (f *webhookFixture) process(t *testing.T, conn *domain.Connection, cb xenditCallback) error {
	t.Helper()
	return f.svc.Process(context.Background(), conn, cb.event(), cb.raw())
}

// lastInbound is the newest inbound row.
func (f *webhookFixture) lastInbound(t *testing.T) domain.InboundWebhook {
	t.Helper()
	rows := f.hooks.AllInbound()
	if len(rows) == 0 {
		t.Fatal("no inbound webhook recorded")
	}
	return rows[len(rows)-1]
}

// xenditCallback is a Xendit invoice callback as the tests write it; event
// turns it into what the adapter hands the service.
type xenditCallback struct {
	ID, ExternalID, Status string
	Amount                 int64
	Currency               string
	PaidAmount             int64
	// Fee is the merchant fee, sent as adjusted_received_amount; the body
	// has no such field when Fee is nil.
	Fee            *int64
	PaidAt         string
	SettlementDate string
}

func (cb xenditCallback) raw() []byte {
	body := map[string]any{"id": cb.ID, "external_id": cb.ExternalID, "status": cb.Status, "amount": cb.Amount,
		"currency": cb.Currency, "paid_amount": cb.PaidAmount, "paid_at": cb.PaidAt, "settlement_date": cb.SettlementDate,
		"fees_paid_amount": 0}
	if cb.Fee != nil {
		gross := cb.PaidAmount
		if gross == 0 {
			gross = cb.Amount
		}
		body["adjusted_received_amount"] = gross - *cb.Fee
	}
	raw, _ := json.Marshal(body)
	return raw
}

func (cb xenditCallback) event() provider.InboundEvent {
	ev, err := xendit.New().ParseWebhook(cb.raw())
	if err != nil {
		// Callbacks the adapter refuses still reach Process in TestProcessMissingKeyIsInvalidPayload.
		status, _ := xendit.MapStatus(cb.Status)
		return provider.InboundEvent{EventKey: cb.ID, Reference: cb.ExternalID, ProviderStatus: cb.Status, Status: status}
	}
	return *ev
}

func jsonNumber(s string) json.Number { return json.Number(s) }

func i64(v int64) *int64 { return &v }
