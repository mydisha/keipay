package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/xendit"
)

// memPaymentRepo is an in-memory payment store that enforces the same
// UNIQUE(external_id) rule as the payments table.
type memPaymentRepo struct {
	byExternal map[string]*domain.Payment
	nextID     int64
	// createErr, when set, is returned by Create instead of inserting.
	createErr error
}

func newMemPaymentRepo() *memPaymentRepo {
	return &memPaymentRepo{byExternal: map[string]*domain.Payment{}}
}

func (m *memPaymentRepo) Create(_ context.Context, p *domain.Payment) error {
	if m.createErr != nil {
		return m.createErr
	}
	if _, ok := m.byExternal[p.ExternalID]; ok {
		return domain.ErrConflict
	}
	m.nextID++
	p.ID = m.nextID
	cp := *p
	m.byExternal[p.ExternalID] = &cp
	return nil
}
func (m *memPaymentRepo) UpdateStatus(context.Context, int64, domain.PaymentStatus, *time.Time, *time.Time, []byte) error {
	return nil
}
func (m *memPaymentRepo) GetByID(context.Context, int64) (*domain.Payment, error) {
	return nil, domain.ErrNotFound
}
func (m *memPaymentRepo) GetByExternalID(_ context.Context, ext string) (*domain.Payment, error) {
	if p, ok := m.byExternal[ext]; ok {
		cp := *p
		return &cp, nil
	}
	return nil, domain.ErrNotFound
}
func (m *memPaymentRepo) GetByExternalIDForUpdate(ctx context.Context, ext string) (*domain.Payment, error) {
	return m.GetByExternalID(ctx, ext)
}
func (m *memPaymentRepo) List(context.Context, domain.PaymentFilter) ([]domain.Payment, int64, error) {
	return nil, 0, nil
}

type fakeXendit struct {
	created []xendit.CreateInvoiceInput
	expired []string
	err     error
	// expireErr, when set, is returned by ExpireInvoice.
	expireErr error
	// onCreate runs after an invoice is created, before Create returns.
	onCreate func()
}

func (f *fakeXendit) CreateInvoice(_ context.Context, in xendit.CreateInvoiceInput) (*xendit.CreateInvoiceOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.created = append(f.created, in)
	n := len(f.created)
	if f.onCreate != nil {
		f.onCreate()
	}
	return &xendit.CreateInvoiceOutput{
		InvoiceID:  "inv_" + string(rune('0'+n)),
		InvoiceURL: "https://checkout.xendit.co/web/inv_" + string(rune('0'+n)),
		Status:     domain.PaymentStatusPending,
	}, nil
}

func (f *fakeXendit) ExpireInvoice(_ context.Context, id string) error {
	if f.expireErr != nil {
		return f.expireErr
	}
	f.expired = append(f.expired, id)
	return nil
}

var testTenant = &domain.Tenant{ID: 1, Slug: "etala"}

// Regression: a tenant retrying the same order_id must get the original
// payment back instead of a second (orphan) Xendit invoice and a 409.
func TestCreateDuplicateOrderReturnsExisting(t *testing.T) {
	repo, xnd := newMemPaymentRepo(), &fakeXendit{}
	svc := NewPaymentService(repo, xnd)
	in := CreatePaymentInput{OrderID: "ord-1", Amount: 150000}

	first, err := svc.Create(context.Background(), testTenant, in)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	second, err := svc.Create(context.Background(), testTenant, in)
	if err != nil {
		t.Fatalf("retry create: %v", err)
	}
	if second.ID != first.ID || second.InvoiceURL != first.InvoiceURL {
		t.Fatalf("retry returned a different payment: first=%+v second=%+v", first, second)
	}
	if len(xnd.created) != 1 {
		t.Fatalf("expected 1 xendit invoice, got %d", len(xnd.created))
	}
}

func TestCreateDuplicateOrderDifferentAmountConflicts(t *testing.T) {
	repo, xnd := newMemPaymentRepo(), &fakeXendit{}
	svc := NewPaymentService(repo, xnd)
	if _, err := svc.Create(context.Background(), testTenant, CreatePaymentInput{OrderID: "ord-1", Amount: 150000}); err != nil {
		t.Fatalf("first create: %v", err)
	}

	for _, in := range []CreatePaymentInput{
		{OrderID: "ord-1", Amount: 99000},
		{OrderID: "ord-1", Amount: 150000, Currency: "USD"},
	} {
		if _, err := svc.Create(context.Background(), testTenant, in); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("expected ErrConflict for %+v, got %v", in, err)
		}
	}
	if len(xnd.created) != 1 {
		t.Fatalf("conflicting retries created invoices: %d", len(xnd.created))
	}
}

// Order ids are scoped per tenant: the same order_id from another tenant is a
// different payment.
func TestCreateSameOrderOtherTenant(t *testing.T) {
	repo, xnd := newMemPaymentRepo(), &fakeXendit{}
	svc := NewPaymentService(repo, xnd)
	in := CreatePaymentInput{OrderID: "ord-1", Amount: 150000}
	a, err := svc.Create(context.Background(), testTenant, in)
	if err != nil {
		t.Fatalf("tenant a: %v", err)
	}
	b, err := svc.Create(context.Background(), &domain.Tenant{ID: 2, Slug: "inveet"}, in)
	if err != nil {
		t.Fatalf("tenant b: %v", err)
	}
	if a.ID == b.ID || b.ExternalID != "inveet:ord-1" || len(xnd.created) != 2 {
		t.Fatalf("expected separate payments, got a=%+v b=%+v", a, b)
	}
}

// Two concurrent requests both miss the lookup; the loser of the insert race
// expires its own invoice and returns the winner's payment.
func TestCreateInsertRaceExpiresLoserInvoice(t *testing.T) {
	repo, xnd := newMemPaymentRepo(), &fakeXendit{}
	svc := NewPaymentService(repo, xnd)
	winner := &domain.Payment{TenantID: 1, OrderID: "ord-1", ExternalID: "etala:ord-1", Amount: 150000,
		Currency: "IDR", XenditInvoiceID: "inv_winner", InvoiceURL: "https://checkout.xendit.co/web/inv_winner"}
	xnd.onCreate = func() { _ = repo.Create(context.Background(), winner) }

	got, err := svc.Create(context.Background(), testTenant, CreatePaymentInput{OrderID: "ord-1", Amount: 150000})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got.XenditInvoiceID != "inv_winner" {
		t.Fatalf("expected winner payment, got %+v", got)
	}
	if len(xnd.expired) != 1 || xnd.expired[0] != "inv_1" {
		t.Fatalf("expected loser invoice inv_1 expired, got %v", xnd.expired)
	}
}

func TestCreateInsertFailureExpiresInvoice(t *testing.T) {
	repo, xnd := newMemPaymentRepo(), &fakeXendit{}
	repo.createErr = errors.New("db down")
	svc := NewPaymentService(repo, xnd)

	_, err := svc.Create(context.Background(), testTenant, CreatePaymentInput{OrderID: "ord-1", Amount: 150000})
	if err == nil || !errors.Is(err, repo.createErr) {
		t.Fatalf("expected insert error, got %v", err)
	}
	if len(xnd.expired) != 1 || xnd.expired[0] != "inv_1" {
		t.Fatalf("expected orphan invoice expired, got %v", xnd.expired)
	}
}

// If the orphan cannot be expired either, the error says so instead of
// silently leaving a payable, unrecorded invoice.
func TestCreateInsertFailureReportsExpireFailure(t *testing.T) {
	repo, xnd := newMemPaymentRepo(), &fakeXendit{expireErr: errors.New("xendit 503")}
	repo.createErr = errors.New("db down")
	svc := NewPaymentService(repo, xnd)

	_, err := svc.Create(context.Background(), testTenant, CreatePaymentInput{OrderID: "ord-1", Amount: 150000})
	if !errors.Is(err, repo.createErr) || !errors.Is(err, xnd.expireErr) {
		t.Fatalf("expected both insert and expire errors, got %v", err)
	}
}

func TestCreateXenditErrorStoresNothing(t *testing.T) {
	repo, xnd := newMemPaymentRepo(), &fakeXendit{err: errors.New("xendit 500")}
	svc := NewPaymentService(repo, xnd)
	if _, err := svc.Create(context.Background(), testTenant, CreatePaymentInput{OrderID: "ord-1", Amount: 150000}); err == nil {
		t.Fatal("expected error")
	}
	if len(repo.byExternal) != 0 {
		t.Fatalf("payment stored despite xendit failure: %v", repo.byExternal)
	}
}

func TestCreateValidation(t *testing.T) {
	svc := NewPaymentService(newMemPaymentRepo(), &fakeXendit{})
	for name, in := range map[string]CreatePaymentInput{
		"blank order_id": {OrderID: "  ", Amount: 1000},
		"zero amount":    {OrderID: "ord-1", Amount: 0},
		"negative":       {OrderID: "ord-1", Amount: -1},
		"item no name":   {OrderID: "ord-1", Amount: 1000, Items: []domain.OrderItem{{Quantity: 1}}},
		"item zero qty":  {OrderID: "ord-1", Amount: 1000, Items: []domain.OrderItem{{Name: "Paket", Quantity: 0}}},
	} {
		if _, err := svc.Create(context.Background(), testTenant, in); !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("%s: expected ErrInvalidInput, got %v", name, err)
		}
	}
}

// Unicode order ids are kept verbatim in the routing key.
func TestCreateUnicodeOrderID(t *testing.T) {
	svc := NewPaymentService(newMemPaymentRepo(), &fakeXendit{})
	p, err := svc.Create(context.Background(), testTenant, CreatePaymentInput{OrderID: " pesanan-ü-1 ", Amount: 1000})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.ExternalID != "etala:pesanan-ü-1" {
		t.Fatalf("unexpected external_id %q", p.ExternalID)
	}
}
