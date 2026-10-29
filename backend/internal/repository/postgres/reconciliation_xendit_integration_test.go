package postgres_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/crypto"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/provider"
	"github.com/mydisha/keipay/backend/internal/provider/xendit"
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/testutil/pgtest"
)

// xenditTransactionsAPI serves Xendit's List Transactions endpoint from
// canned bodies keyed by after_id, or a fixed error status.
type xenditTransactionsAPI struct {
	mu     sync.Mutex
	pages  map[string]string
	status int
	body   string
	calls  int
	auth   []string
}

func (a *xenditTransactionsAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	user, _, _ := r.BasicAuth()
	a.auth = append(a.auth, user)
	w.Header().Set("Content-Type", "application/json")
	if a.status != 0 {
		w.WriteHeader(a.status)
		_, _ = w.Write([]byte(a.body))
		return
	}
	if r.URL.Path != "/transactions" || r.URL.Query().Get("types") != "PAYMENT" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_, _ = w.Write([]byte(a.pages[r.URL.Query().Get("after_id")]))
}

// xenditRecon is a fresh database with a Xendit test connection whose API is
// a local server, one test app, and an admin.
type xenditRecon struct {
	api      *xenditTransactionsAPI
	repo     *postgres.ReconciliationRepo
	reports  *postgres.PaymentReportRepo
	payments *postgres.PaymentRepo
	svc      *service.ReconciliationService
	conn     *domain.Connection
	app      *domain.App
	admin    *domain.User
	ctx      context.Context
}

const xenditTestSecret = "xnd_development_reconqa0001"

func newXenditRecon(t *testing.T) *xenditRecon {
	t.Helper()
	db := pgtest.New(t, 0)
	pool := openPool(t, db.DSN)
	api := &xenditTransactionsAPI{pages: map[string]string{}}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)

	registry, err := provider.NewRegistry(xendit.New(xendit.WithBaseURL(srv.URL)))
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := crypto.New("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	if err != nil {
		t.Fatal(err)
	}
	tx := postgres.NewTxManager(pool)
	audit := service.NewAuditService(postgres.NewAuditRepo(pool))
	conns := postgres.NewConnectionRepo(pool)
	payments := postgres.NewPaymentRepo(pool)
	connSvc := service.NewConnectionService(service.ConnectionDeps{Connections: conns, Merchants: postgres.NewMerchantRepo(pool),
		Apps: postgres.NewAppRepo(pool), Payments: payments, Registry: registry, Sealer: cipher, Tx: tx, Audit: audit,
		BaseURL: "https://pay.example.com"})
	imported, err := connSvc.ImportLegacyXendit(context.Background(), xenditTestSecret, "callback-token-reconqa")
	if err != nil || imported == nil {
		t.Fatalf("import xendit connection: %v", err)
	}

	x := &xenditRecon{api: api, repo: postgres.NewReconciliationRepo(pool), reports: postgres.NewPaymentReportRepo(pool),
		payments: payments, conn: imported.Connection}
	x.admin = &domain.User{PublicID: id.New(id.User), Email: "recon-qa@example.com", Name: "Recon QA", Role: domain.RoleAdmin,
		PasswordHash: "hash", SessionVersion: 1}
	if err := postgres.NewUserRepo(pool).Create(context.Background(), x.admin); err != nil {
		t.Fatal(err)
	}
	x.ctx = domain.WithActor(context.Background(), domain.UserActor(x.admin.PublicID, "203.0.113.9", "it-agent", "req-xendit"))
	x.app = &domain.App{PublicID: id.New(id.App), Slug: "globex", Name: "Globex", Environment: domain.EnvTest,
		Status: domain.AppActive, WebhookSecretEnc: "x"}
	if err := postgres.NewAppRepo(pool).Create(context.Background(), x.app); err != nil {
		t.Fatal(err)
	}
	x.svc = service.NewReconciliationService(service.ReconciliationDeps{Runs: x.repo, Connections: conns, Registry: registry,
		Source: service.NewProviderSource(registry, cipher), Tx: tx, Enqueuer: &runQueue{}, Audit: audit})
	return x
}

// window is yesterday in whole hours, like the console would send.
func (x *xenditRecon) window() (time.Time, time.Time) {
	to := time.Now().UTC().Truncate(time.Hour).Add(-time.Hour)
	return to.Add(-24 * time.Hour), to
}

func (x *xenditRecon) paid(t *testing.T, ref string, amount int64, fee *int64, paidAt time.Time) {
	t.Helper()
	ctx := context.Background()
	p := &domain.Payment{PublicID: id.New(id.Payment), AppID: x.app.ID, OrderID: "order-" + ref, ExternalID: ref,
		ConnectionID: &x.conn.ID, Provider: "xendit", Amount: amount, Currency: "IDR", Status: domain.PaymentStatusPaid}
	if err := x.payments.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	rep := &domain.PaymentReport{PaymentID: p.ID, AppID: x.app.ID, ConnectionID: &x.conn.ID, Currency: "IDR", Gross: amount,
		Fee: fee, Status: domain.PaymentStatusPaid, PaidAt: paidAt}
	if fee != nil {
		net := amount - *fee
		rep.Net = &net
	}
	if err := x.reports.Upsert(ctx, rep); err != nil {
		t.Fatal(err)
	}
}

func (x *xenditRecon) run(t *testing.T) *domain.ReconciliationRun {
	t.Helper()
	from, to := x.window()
	run, err := x.svc.CreateRun(x.ctx, service.CreateRunInput{ConnectionID: x.conn.PublicID, From: from.Format(time.RFC3339),
		To: to.Format(time.RFC3339), CreatedByID: &x.admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := x.svc.Execute(context.Background(), run.ID, run.RunCount); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, err := x.repo.GetRun(context.Background(), run.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func xenditRow(txID, ref, status string, amount string, fee string, at time.Time) string {
	feeJSON := ""
	if fee != "" {
		feeJSON = fmt.Sprintf(`,"fee":{"xendit_fee":%s,"value_added_tax":0}`, fee)
	}
	return fmt.Sprintf(`{"id":%q,"reference_id":%q,"type":"PAYMENT","status":%q,"currency":"IDR","amount":%s%s,"created":%q}`,
		txID, ref, status, amount, feeJSON, at.Format(time.RFC3339Nano))
}

// A run over a Xendit connection pages through the real adapter and the
// provider source, keeps amounts above float32 precision exact, and stores
// every result with its fee.
func TestIntegrationReconciliationThroughXenditAdapter(t *testing.T) {
	x := newXenditRecon(t)
	from, _ := x.window()
	at := from.Add(2 * time.Hour)
	fee := int64(4500)
	x.paid(t, "globex:exact", 16777217, &fee, at)
	x.paid(t, "globex:fee", 25000000, &fee, at)
	x.paid(t, "globex:only-keipay", 1000, nil, at)
	x.api.pages[""] = `{"has_more":true,"data":[` +
		xenditRow("txn_a", "globex:exact", "SUCCESS", "16777217", "4500", at) + `,` +
		xenditRow("txn_b", "globex:fee", "SUCCESS", "25000000", "5000", at) + `]}`
	x.api.pages["txn_b"] = `{"has_more":false,"data":[` +
		xenditRow("txn_c", "globex:only-gateway", "SUCCESS", "7000", "", at) + `,` +
		xenditRow("txn_d", "globex:failed-only", "FAILED", "8000", "", at) + `]}`

	run := x.run(t)
	if run.Status != domain.RunSucceeded || run.Summary == nil || run.Summary.Total != 4 || run.UnresolvedCount != 3 {
		t.Fatalf("run = %+v (summary %+v, error %+v)", run, run.Summary, run.Error)
	}
	if x.api.calls != 2 || x.api.auth[0] != xenditTestSecret {
		t.Fatalf("api calls = %d, auth = %v", x.api.calls, x.api.auth)
	}
	want := map[string]domain.ReconciliationResult{
		"globex:exact":        domain.ResultMatched,
		"globex:fee":          domain.ResultFeeMismatch,
		"globex:only-keipay":  domain.ResultMissingInGateway,
		"globex:only-gateway": domain.ResultMissingInKeipay,
	}
	got := map[string]domain.ReconciliationItem{}
	err := x.repo.StreamItems(context.Background(), run.ID, domain.ReconciliationItemFilter{}, func(it *domain.ReconciliationItem) error {
		got[it.Reference] = *it
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("items = %v", got)
	}
	for ref, result := range want {
		if got[ref].Result != result {
			t.Fatalf("%s: result %s, want %s", ref, got[ref].Result, result)
		}
	}
	exact := got["globex:exact"]
	if exact.Gateway == nil || exact.Gateway.Amount != 16777217 || exact.Gateway.Fee == nil || *exact.Gateway.Fee != 4500 {
		t.Fatalf("exact gateway side = %+v", exact.Gateway)
	}
	if g := got["globex:only-gateway"].Gateway; g == nil || g.Fee != nil || g.Amount != 7000 {
		t.Fatalf("gateway-only side = %+v", g)
	}
}

// Gateway failures through the real adapter end the run failed with a fixed
// message: credentials refused, and a gateway that keeps answering 503
// after the source's retries.
func TestIntegrationReconciliationXenditFailures(t *testing.T) {
	cases := []struct {
		name   string
		status int
		code   string
		calls  int
	}{
		{"rejected", http.StatusUnauthorized, domain.RunErrorProviderRejected, 1},
		{"unavailable", http.StatusServiceUnavailable, domain.RunErrorProviderUnavailable, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x := newXenditRecon(t)
			x.api.status = tc.status
			x.api.body = `{"error_code":"INVALID_API_KEY","message":"gateway says xnd_secret_leak"}`
			run := x.run(t)
			if run.Status != domain.RunFailed || run.Error == nil || run.Error.Code != tc.code || run.FinishedAt == nil {
				t.Fatalf("run = %+v (error %+v)", run, run.Error)
			}
			if strings.Contains(run.Error.Message, "xnd_") || strings.Contains(run.Error.Message, "INVALID_API_KEY") {
				t.Fatalf("gateway text in message: %q", run.Error.Message)
			}
			if x.api.calls != tc.calls {
				t.Fatalf("api calls = %d, want %d", x.api.calls, tc.calls)
			}
		})
	}
}
