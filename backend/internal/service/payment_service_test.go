package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/provider/providertest"
	"github.com/mydisha/keipay/backend/internal/testutil/memstore"
)

type paymentFixture struct {
	svc      *PaymentService
	apps     *memstore.Apps
	conns    *memstore.Connections
	payments *memstore.Payments
	fake     *providertest.Fake
	acme     *domain.App
	globex   *domain.App
	// conn is the live connection both apps use by default.
	conn *domain.Connection
	logs *observer.ObservedLogs
}

func newPaymentFixture(t *testing.T) *paymentFixture {
	t.Helper()
	cf := newConnFixture(t)
	core, logs := observer.New(zap.DebugLevel)
	f := &paymentFixture{apps: cf.apps, conns: cf.conns, payments: cf.payments, fake: cf.fake, logs: logs}
	f.svc = NewPaymentService(f.payments, f.conns, cf.registry, cf.cipher, zap.New(core))
	f.conn = cf.addConnection(t, "fake-live", domain.EnvLive)
	add := func(slug string) *domain.App {
		a := &domain.App{PublicID: id.New(id.App), Slug: slug, Name: strings.ToUpper(slug), Environment: domain.EnvLive,
			Status: domain.AppActive, RedirectSuccessURL: "https://" + slug + ".example/ok"}
		if err := f.apps.Create(context.Background(), a); err != nil {
			t.Fatal(err)
		}
		a.DefaultConnectionID = &f.conn.ID
		if err := f.apps.Update(context.Background(), a); err != nil {
			t.Fatal(err)
		}
		got, _ := f.apps.GetByID(context.Background(), a.ID)
		return got
	}
	f.acme, f.globex = add("acme"), add("globex")
	return f
}

func (f *paymentFixture) create(t *testing.T, app *domain.App, in CreatePaymentInput, key string) (*domain.Payment, bool, error) {
	t.Helper()
	return f.svc.Create(context.Background(), app, in, key)
}

var order1 = CreatePaymentInput{OrderID: "ord-1", Amount: 150000}

// Regression: an app retrying the same order_id must get the original
// payment back instead of a second (orphan) Xendit invoice and a 409.
func TestCreateDuplicateOrderReturnsExisting(t *testing.T) {
	f := newPaymentFixture(t)
	first, replayed, err := f.create(t, f.acme, order1, "")
	if err != nil || replayed {
		t.Fatalf("first create: %v replayed=%v", err, replayed)
	}
	second, replayed, err := f.create(t, f.acme, order1, "")
	if err != nil || !replayed {
		t.Fatalf("retry: %v replayed=%v", err, replayed)
	}
	if second.ID != first.ID || second.InvoiceURL != first.InvoiceURL {
		t.Fatalf("retry returned a different payment: first=%+v second=%+v", first, second)
	}
	if len(f.fake.Creates) != 1 {
		t.Fatalf("expected 1 xendit invoice, got %d", len(f.fake.Creates))
	}
}

func TestCreateDuplicateOrderDifferentAmountConflicts(t *testing.T) {
	f := newPaymentFixture(t)
	if _, _, err := f.create(t, f.acme, order1, ""); err != nil {
		t.Fatal(err)
	}
	for _, in := range []CreatePaymentInput{
		{OrderID: "ord-1", Amount: 99000},
		{OrderID: "ord-1", Amount: 150000, Currency: "USD"},
	} {
		if _, _, err := f.create(t, f.acme, in, ""); !errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrIdempotencyKeyReused) {
			t.Fatalf("expected conflict for %+v, got %v", in, err)
		}
	}
	if len(f.fake.Creates) != 1 {
		t.Fatalf("conflicting retries created invoices: %d", len(f.fake.Creates))
	}
}

// T-12: the gateway reference is kp_ + the new pay_ id; redirects come from
// the app; metadata, key and fingerprint are stored.
func TestCreateStoresReferenceMetadataAndKey(t *testing.T) {
	f := newPaymentFixture(t)
	in := order1
	in.Metadata = json.RawMessage(`{"channel":"mobile","tier":12345678901234567890,"rate":0.1,"vip":true,"x":null}`)
	p, _, err := f.create(t, f.acme, in, "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if !id.Valid(p.PublicID, id.Payment) || p.ExternalID != "kp_"+p.PublicID || p.Reference() != p.ExternalID {
		t.Fatalf("ids = %s / %s", p.PublicID, p.ExternalID)
	}
	if got := f.fake.Creates[0].Input; got.Reference != p.ExternalID || got.SuccessRedirectURL != "https://acme.example/ok" || got.Currency != "IDR" {
		t.Fatalf("invoice input = %+v", got)
	}
	stored := f.payments.All()[0]
	if stored.AppID != f.acme.ID || stored.IdempotencyKey != "key-1" || len(stored.IdempotencyFingerprint) != 64 {
		t.Fatalf("stored = %+v", stored)
	}
	b, _ := json.Marshal(stored.Metadata)
	if string(b) != `{"channel":"mobile","rate":0.1,"tier":12345678901234567890,"vip":true,"x":null}` {
		t.Fatalf("metadata = %s", b)
	}

	// Without a key, neither key nor fingerprint is stored.
	q, _, err := f.create(t, f.acme, CreatePaymentInput{OrderID: "ord-2", Amount: 1}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := f.payments.GetByPublicID(context.Background(), f.acme.ID, q.PublicID); got.IdempotencyKey != "" || got.IdempotencyFingerprint != "" {
		t.Fatalf("keyless payment = %+v", got)
	}
}

// T-12 / AC-15: the idempotency rules.
func TestCreateIdempotencyKey(t *testing.T) {
	f := newPaymentFixture(t)
	first, _, err := f.create(t, f.acme, order1, "K1")
	if err != nil {
		t.Fatal(err)
	}

	// Same key, same request: replay.
	got, replayed, err := f.create(t, f.acme, order1, "K1")
	if err != nil || !replayed || got.ID != first.ID {
		t.Fatalf("same key replay: %+v %v %v", got, replayed, err)
	}

	// Same key, different request: reused.
	for name, in := range map[string]CreatePaymentInput{
		"amount":   {OrderID: "ord-1", Amount: 1},
		"metadata": {OrderID: "ord-1", Amount: 150000, Metadata: json.RawMessage(`{"a":1}`)},
		"order":    {OrderID: "ord-other", Amount: 150000},
		"items":    {OrderID: "ord-1", Amount: 150000, Items: []domain.OrderItem{{Name: "x", Quantity: 1}}},
	} {
		if _, _, err := f.create(t, f.acme, in, "K1"); !errors.Is(err, domain.ErrIdempotencyKeyReused) {
			t.Fatalf("%s: err = %v, want idempotency_key_reused", name, err)
		}
	}

	// Another key for the same order and amount replays the order; K2 is not attached.
	got, replayed, err = f.create(t, f.acme, order1, "K2")
	if err != nil || !replayed || got.ID != first.ID {
		t.Fatalf("K2 replay: %+v %v %v", got, replayed, err)
	}
	if _, err := f.payments.GetByIdempotencyKey(context.Background(), f.acme.ID, "K2"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("K2 was attached: %v", err)
	}
	// Another key for the same order with another amount is the order conflict.
	if _, _, err := f.create(t, f.acme, CreatePaymentInput{OrderID: "ord-1", Amount: 5}, "K3"); !errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrIdempotencyKeyReused) {
		t.Fatalf("K3 different amount: %v", err)
	}

	// The same order and key in another app is another payment.
	other, replayed, err := f.create(t, f.globex, order1, "K1")
	if err != nil || replayed || other.ID == first.ID || other.AppID != f.globex.ID {
		t.Fatalf("other app: %+v %v %v", other, replayed, err)
	}
	if len(f.fake.Creates) != 2 || len(f.payments.All()) != 2 {
		t.Fatalf("invoices=%d payments=%d", len(f.fake.Creates), len(f.payments.All()))
	}
}

// Two concurrent requests both miss the lookup; the loser of the insert race
// expires its own invoice and answers with the winner per the usual rules.
func TestCreateInsertRace(t *testing.T) {
	cases := []struct {
		name       string
		winnerKey  string
		winnerAmt  int64
		key        string
		wantErr    error
		wantWinner bool
	}{
		{"same order, no key", "", 150000, "", nil, true},
		{"same order, other amount", "", 99, "", domain.ErrConflict, false},
		{"same key, same request", "K", 150000, "K", nil, true},
		{"same key, other request", "K", 99, "K", domain.ErrIdempotencyKeyReused, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPaymentFixture(t)
			winner := &domain.Payment{PublicID: id.New(id.Payment), AppID: f.acme.ID, OrderID: "ord-1", Amount: tc.winnerAmt,
				Currency: "IDR", XenditInvoiceID: "inv_winner", IdempotencyKey: tc.winnerKey}
			winner.ExternalID = "kp_" + winner.PublicID
			if tc.winnerKey != "" {
				norm, _ := normalizePayment(CreatePaymentInput{OrderID: "ord-1", Amount: tc.winnerAmt}, domain.Metadata{})
				winner.IdempotencyFingerprint = paymentFingerprint(norm)
			}
			f.payments.BeforeCreate = func() { _ = f.payments.Create(context.Background(), winner) }

			got, replayed, err := f.create(t, f.acme, order1, tc.key)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
			} else if err != nil || !replayed || got.XenditInvoiceID != "inv_winner" {
				t.Fatalf("got %+v replayed=%v err=%v", got, replayed, err)
			}
			if len(f.fake.Cancels) != 1 || f.fake.Cancels[0].ProviderPaymentID != "inv_1" {
				t.Fatalf("expected loser invoice inv_1 expired, got %v", f.fake.Cancels)
			}
		})
	}
}

func TestCreateInsertFailureExpiresInvoice(t *testing.T) {
	f := newPaymentFixture(t)
	f.payments.CreateErr = errors.New("db down")
	_, _, err := f.create(t, f.acme, order1, "")
	if !errors.Is(err, f.payments.CreateErr) {
		t.Fatalf("expected insert error, got %v", err)
	}
	if len(f.fake.Cancels) != 1 || f.fake.Cancels[0].ProviderPaymentID != "inv_1" {
		t.Fatalf("expected orphan invoice expired, got %v", f.fake.Cancels)
	}
}

// If the orphan cannot be expired either, the error says so instead of
// silently leaving a payable, unrecorded invoice.
func TestCreateInsertFailureReportsExpireFailure(t *testing.T) {
	f := newPaymentFixture(t)
	f.payments.CreateErr = errors.New("db down")
	f.fake.CancelErr = errors.New("xendit 503")
	_, _, err := f.create(t, f.acme, order1, "")
	if !errors.Is(err, f.payments.CreateErr) || !errors.Is(err, f.fake.CancelErr) {
		t.Fatalf("expected both insert and expire errors, got %v", err)
	}
}

func TestCreateXenditErrorStoresNothing(t *testing.T) {
	f := newPaymentFixture(t)
	f.fake.CreateErr = errors.New("xendit 500")
	if _, _, err := f.create(t, f.acme, order1, ""); err == nil {
		t.Fatal("expected error")
	}
	if n := len(f.payments.All()); n != 0 {
		t.Fatalf("payment stored despite xendit failure: %d", n)
	}
}

func TestCreateValidation(t *testing.T) {
	f := newPaymentFixture(t)
	for name, in := range map[string]CreatePaymentInput{
		"blank order_id": {OrderID: "  ", Amount: 1000},
		"zero amount":    {OrderID: "ord-1", Amount: 0},
		"negative":       {OrderID: "ord-1", Amount: -1},
		"item no name":   {OrderID: "ord-1", Amount: 1000, Items: []domain.OrderItem{{Quantity: 1}}},
		"item zero qty":  {OrderID: "ord-1", Amount: 1000, Items: []domain.OrderItem{{Name: "Paket", Quantity: 0}}},
	} {
		if _, _, err := f.create(t, f.acme, in, ""); !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("%s: expected ErrInvalidInput, got %v", name, err)
		}
	}
	// Field rules come back as invalid_request with the field named.
	cases := []struct {
		in   CreatePaymentInput
		key  string
		want []domain.FieldError
	}{
		{CreatePaymentInput{OrderID: "o", Amount: 1, Metadata: json.RawMessage(`[1]`)}, "", []domain.FieldError{{Field: "metadata", Rule: "object"}}},
		{order1, strings.Repeat("k", 256), []domain.FieldError{{Field: "Idempotency-Key", Rule: "max"}}},
		{CreatePaymentInput{OrderID: "o", Amount: 1, Metadata: json.RawMessage(`{"a":{}}`)}, "bad key",
			[]domain.FieldError{{Field: "metadata.a", Rule: "type"}, {Field: "Idempotency-Key", Rule: "format"}}},
	}
	for _, tc := range cases {
		_, _, err := f.create(t, f.acme, tc.in, tc.key)
		assertFields(t, err, tc.want...)
	}
	if len(f.fake.Creates) != 0 {
		t.Fatalf("invalid requests created invoices: %d", len(f.fake.Creates))
	}
}

// Unicode order ids are kept verbatim.
func TestCreateUnicodeOrderID(t *testing.T) {
	f := newPaymentFixture(t)
	p, _, err := f.create(t, f.acme, CreatePaymentInput{OrderID: " pesanan-ü-1 ", Amount: 1000}, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.OrderID != "pesanan-ü-1" || !strings.HasPrefix(p.ExternalID, "kp_pay_") {
		t.Fatalf("payment = %+v", p)
	}
}

// T-13: Get by pay_ id or integer, scoped to the app.
func TestGetPaymentScopedToApp(t *testing.T) {
	f := newPaymentFixture(t)
	p, _, err := f.create(t, f.acme, order1, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, ref := range []PaymentRef{{PublicID: p.PublicID}, {ID: p.ID}} {
		got, err := f.svc.Get(ctx, f.acme, ref)
		if err != nil || got.ID != p.ID || got.App.PublicID != f.acme.PublicID {
			t.Fatalf("get %+v = %+v, %v", ref, got, err)
		}
		if _, err := f.svc.Get(ctx, f.globex, ref); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("other app get %+v: %v", ref, err)
		}
	}
	if _, err := f.svc.Get(ctx, f.acme, PaymentRef{ID: 0}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("id 0: %v", err)
	}
}

// T-13: List filters by app and status, pages newest first, and rows carry
// the app summary.
func TestListPayments(t *testing.T) {
	f := newPaymentFixture(t)
	ctx := context.Background()
	for i := range 3 {
		if _, _, err := f.create(t, f.acme, CreatePaymentInput{OrderID: "a" + strconv.Itoa(i), Amount: 1}, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := f.create(t, f.globex, order1, ""); err != nil {
		t.Fatal(err)
	}
	acme := f.acme.PublicID
	res, err := f.svc.List(ctx, domain.PaymentFilter{AppPublicID: &acme}, domain.PageRequest{Limit: 2})
	if err != nil || len(res.Items) != 2 || res.NextCursor == "" {
		t.Fatalf("page 1 = %+v, %v", res, err)
	}
	for _, p := range res.Items {
		if p.App.PublicID != acme || p.App.Slug != "acme" || p.App.Environment != domain.EnvLive {
			t.Fatalf("row app = %+v", p.App)
		}
	}
	next, err := f.svc.List(ctx, domain.PaymentFilter{AppPublicID: &acme}, domain.PageRequest{Limit: 2, Cursor: res.NextCursor})
	if err != nil || len(next.Items) != 1 || next.NextCursor != "" {
		t.Fatalf("page 2 = %+v, %v", next, err)
	}
	paid := domain.PaymentStatusPaid
	if res, _ := f.svc.List(ctx, domain.PaymentFilter{Status: &paid}, domain.PageRequest{}); len(res.Items) != 0 {
		t.Fatalf("status filter = %+v", res.Items)
	}
	if _, err := f.svc.List(ctx, domain.PaymentFilter{}, domain.PageRequest{Limit: 201}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("limit 201: %v", err)
	}
}

// assertFields checks err is invalid_request with exactly want, in order.
func assertFields(t *testing.T, err error, want ...domain.FieldError) {
	t.Helper()
	var coded *domain.Error
	if !errors.As(err, &coded) || coded.Code != domain.CodeInvalidRequest {
		t.Fatalf("err = %v, want invalid_request %v", err, want)
	}
	got, _ := coded.Details["fields"].([]domain.FieldError)
	if len(got) != len(want) {
		t.Fatalf("fields = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("fields = %v, want %v", got, want)
		}
	}
}
