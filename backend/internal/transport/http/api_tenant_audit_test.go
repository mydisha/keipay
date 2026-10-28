package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/handler"
)

// memTenants is an in-memory TenantRepository for the HTTP-level audit tests.
type memTenants struct {
	domain.TenantRepository
	byID   map[int64]domain.Tenant
	nextID int64
}

func (m *memTenants) Create(_ context.Context, t *domain.Tenant) error {
	for _, v := range m.byID {
		if v.Slug == t.Slug {
			return domain.ErrConflict
		}
	}
	m.nextID++
	t.ID = m.nextID
	m.byID[t.ID] = *t
	return nil
}

func (m *memTenants) GetByIDForUpdate(_ context.Context, id int64) (*domain.Tenant, error) {
	v, ok := m.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &v, nil
}

func (m *memTenants) Update(_ context.Context, t *domain.Tenant) error {
	m.byID[t.ID] = *t
	return nil
}

func (m *memTenants) List(context.Context) ([]domain.Tenant, error) { return nil, nil }

// snapshotTx gives WithinTx rollback semantics over memTenants and the audit
// fake, so a failed audit insert must undo the tenant change.
type snapshotTx struct {
	tenants *memTenants
	audit   *failingAudit
}

func (s snapshotTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	saved := make(map[int64]domain.Tenant, len(s.tenants.byID))
	for k, v := range s.tenants.byID {
		saved[k] = v
	}
	savedNext, savedLogs := s.tenants.nextID, len(s.audit.logs)
	if err := fn(ctx); err != nil {
		s.tenants.byID, s.tenants.nextID = saved, savedNext
		s.audit.logs = s.audit.logs[:savedLogs]
		return err
	}
	return nil
}

type failingAudit struct {
	logs []domain.AuditLog
	err  error
}

func (a *failingAudit) Create(_ context.Context, l *domain.AuditLog) error {
	if a.err != nil {
		return a.err
	}
	a.logs = append(a.logs, *l)
	return nil
}

func (a *failingAudit) List(context.Context, domain.Page) (domain.PageResult[domain.AuditLog], error) {
	return domain.PageResult[domain.AuditLog]{}, nil
}

type plainCipher struct{}

func (plainCipher) Encrypt(s string) (string, error) { return "enc:" + s, nil }
func (plainCipher) Decrypt(s string) (string, error) { return strings.TrimPrefix(s, "enc:"), nil }

type tenantAPI struct {
	*apiFixture
	tenants *memTenants
	logs    *failingAudit
}

func newTenantAPI(t *testing.T) *tenantAPI {
	t.Helper()
	base := newAPI(t, "")
	tenants := &memTenants{byID: map[int64]domain.Tenant{}}
	logs := &failingAudit{}
	auditSvc := service.NewAuditService(logs)
	tx := snapshotTx{tenants: tenants, audit: logs}
	base.deps.Admin = handler.NewAdminHandler(
		service.NewTenantService(tenants, nil, plainCipher{}, tx, auditSvc),
		service.NewPaymentService(emptyPayments{}, nil),
		service.NewLedgerService(emptyLedger{}),
		service.NewDeliveryService(deliveries{}, base.replay, auditSvc),
		auditSvc,
	)
	base.rebuild()
	return &tenantAPI{apiFixture: base, tenants: tenants, logs: logs}
}

// mutate sends a request as b with a fixed origin, so the actor can be checked.
func (f *tenantAPI) mutate(b *browser, method, target, body string) *httptest.ResponseRecorder {
	b.remote = "203.0.113.9:4444"
	return f.send(b, method, target, body, "User-Agent", "qa-agent/1.0", "X-Request-ID", "req-qa-1")
}

// AC-14 at the HTTP layer: each tenant mutation records exactly one entry
// with the signed-in user as actor (usr_ id, IP, UA, request id).
func TestTenantMutationsRecordActorFromRequest(t *testing.T) {
	f := newTenantAPI(t)
	owner := f.signIn(t, f.owner)

	w := f.mutate(owner, http.MethodPost, "/admin/tenants", `{"name":"Acme","slug":"acme"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	w = f.mutate(owner, http.MethodPut, "/admin/tenants/1", `{"name":"Acme 2","active":false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "enc:") || strings.Contains(w.Body.String(), "api_secret") {
		t.Fatalf("update response leaks a secret: %s", w.Body.String())
	}
	w = f.mutate(owner, http.MethodPost, "/admin/tenants/1/rotate", "")
	if w.Code != http.StatusOK {
		t.Fatalf("rotate: %d %s", w.Code, w.Body.String())
	}

	wantActions := []string{service.ActionTenantCreate, service.ActionTenantUpdate, service.ActionTenantRotate}
	if len(f.logs.logs) != len(wantActions) {
		t.Fatalf("entries = %d, want %d", len(f.logs.logs), len(wantActions))
	}
	for i, l := range f.logs.logs {
		if l.Action != wantActions[i] {
			t.Errorf("entry %d action = %q, want %q", i, l.Action, wantActions[i])
		}
		want := domain.Actor{Type: domain.ActorAdmin, ID: f.owner.PublicID, IP: "203.0.113.9", UserAgent: "qa-agent/1.0", RequestID: "req-qa-1"}
		if l.Actor != want {
			t.Errorf("entry %d actor = %+v, want %+v", i, l.Actor, want)
		}
		if l.Target.Type != service.TargetTenant || l.Target.ID != "1" {
			t.Errorf("entry %d target = %+v", i, l.Target)
		}
		for _, raw := range []json.RawMessage{l.Before, l.After} {
			s := string(raw)
			if strings.Contains(s, "enc:") || strings.Contains(s, "secret") || strings.Contains(s, "api_key\"") {
				t.Errorf("entry %d snapshot leaks credentials: %s", i, s)
			}
		}
	}
}

// AC-14: if the audit insert fails, the mutation rolls back and the endpoint
// answers 500 internal_error without the internal error text.
func TestTenantMutationAuditFailureIs500AndRollsBack(t *testing.T) {
	f := newTenantAPI(t)
	owner := f.signIn(t, f.owner)
	if w := f.mutate(owner, http.MethodPost, "/admin/tenants", `{"name":"Acme","slug":"acme"}`); w.Code != http.StatusCreated {
		t.Fatalf("seed create: %d %s", w.Code, w.Body.String())
	}
	before := f.tenants.byID[1]

	f.logs.err = errors.New("pq: relation audit_logs at 10.1.2.3 does not exist")
	cases := []struct{ method, target, body string }{
		{http.MethodPost, "/admin/tenants", `{"name":"Globex","slug":"globex"}`},
		{http.MethodPut, "/admin/tenants/1", `{"name":"Changed","active":false}`},
		{http.MethodPost, "/admin/tenants/1/rotate", ""},
	}
	for _, tc := range cases {
		w := f.mutate(owner, tc.method, tc.target, tc.body)
		env := expectError(t, w, http.StatusInternalServerError, domain.CodeInternal)
		if env.Error.Message != "internal server error" || strings.Contains(w.Body.String(), "10.1.2.3") {
			t.Fatalf("%s %s leaked internal text: %s", tc.method, tc.target, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "api_secret") {
			t.Fatalf("%s %s returned credentials on failure: %s", tc.method, tc.target, w.Body.String())
		}
	}
	if len(f.tenants.byID) != 1 || f.tenants.byID[1] != before {
		t.Fatalf("tenant changed despite audit failure: %+v", f.tenants.byID)
	}
	if len(f.logs.logs) != 1 {
		t.Fatalf("entries = %d, want only the first create", len(f.logs.logs))
	}
}

// Duplicate slug stays 409 conflict when the insert runs in the audit tx.
func TestTenantCreateDuplicateSlugIsConflictOverHTTP(t *testing.T) {
	f := newTenantAPI(t)
	owner := f.signIn(t, f.owner)
	f.mutate(owner, http.MethodPost, "/admin/tenants", `{"name":"Acme","slug":"acme"}`)
	expectError(t, f.mutate(owner, http.MethodPost, "/admin/tenants", `{"name":"Acme","slug":"acme"}`), http.StatusConflict, domain.CodeConflict)
	expectError(t, f.mutate(owner, http.MethodPost, "/admin/tenants", `{"name":"Acme","slug":"Bad Slug!"}`), http.StatusBadRequest, domain.CodeValidationFailed)
	expectError(t, f.mutate(owner, http.MethodPut, "/admin/tenants/99", `{"name":"x"}`), http.StatusNotFound, domain.CodeNotFound)
	expectError(t, f.mutate(owner, http.MethodPost, "/admin/tenants/99/rotate", ""), http.StatusNotFound, domain.CodeNotFound)
	if len(f.logs.logs) != 1 {
		t.Fatalf("entries = %d, want 1", len(f.logs.logs))
	}
}
