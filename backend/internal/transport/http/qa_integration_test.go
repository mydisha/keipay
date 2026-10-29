package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/service"
)

// countOrders returns how many payments of any app carry orderID.
func countOrders(t *testing.T, e *e2e, orderID string) int {
	t.Helper()
	res, err := e.payments.List(context.Background(), domain.PaymentFilter{}, domain.Page{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, p := range res.Items {
		if p.OrderID == orderID {
			n++
		}
	}
	return n
}

// Concurrent duplicates of one order_id without an Idempotency-Key still
// create exactly one payment, and every caller gets that payment.
func TestIntegrationConcurrentOrderWithoutIdempotencyKey(t *testing.T) {
	e := newE2E(t)

	const n = 15
	var wg sync.WaitGroup
	codes := make([]int, n)
	ids := make([]string, n)
	replayed := make([]string, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := e.call(http.MethodPost, "/v1/payments", legacyKey, legacySecret, `{"order_id":"race-nokey","amount":500}`)
			codes[i] = w.Code
			replayed[i] = w.Header().Get("Idempotent-Replayed")
			var p struct{ ID string }
			_ = json.Unmarshal(w.Body.Bytes(), &p)
			ids[i] = p.ID
		}()
	}
	wg.Wait()
	if got := countOrders(t, e, "race-nokey"); got != 1 {
		t.Fatalf("%d payment rows for one order", got)
	}
	fresh := 0
	for i := range n {
		if codes[i] != http.StatusCreated || ids[i] != ids[0] {
			t.Fatalf("request %d: %d %s (first %s)", i, codes[i], ids[i], ids[0])
		}
		if replayed[i] != "true" {
			fresh++
		}
	}
	if fresh != 1 {
		t.Fatalf("%d responses without Idempotent-Replayed, want exactly 1", fresh)
	}
}

// Concurrent requests for one order_id with different amounts create one
// payment; the losers with another amount get 409 conflict.
func TestIntegrationConcurrentOrderConflictingAmounts(t *testing.T) {
	e := newE2E(t)

	const n = 12
	var wg sync.WaitGroup
	codes := make([]int, n)
	amounts := make([]int, n)
	bodies := make([]string, n)
	for i := range n {
		amounts[i] = 100 + 100*(i%2)
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := e.call(http.MethodPost, "/v1/payments", legacyKey, legacySecret,
				`{"order_id":"race-amount","amount":`+strconv.Itoa(amounts[i])+`}`)
			codes[i] = w.Code
			bodies[i] = w.Body.String()
		}()
	}
	wg.Wait()
	if got := countOrders(t, e, "race-amount"); got != 1 {
		t.Fatalf("%d payment rows for one order", got)
	}
	winner := -1
	for i := range n {
		if codes[i] == http.StatusCreated {
			if winner >= 0 && amounts[winner] != amounts[i] {
				t.Fatalf("both amounts %d and %d were accepted", amounts[winner], amounts[i])
			}
			winner = i
			continue
		}
		if codes[i] != http.StatusConflict {
			t.Fatalf("request %d: %d %s", i, codes[i], bodies[i])
		}
	}
	if winner < 0 {
		t.Fatal("no request succeeded")
	}
	for i := range n {
		if codes[i] == http.StatusConflict && amounts[i] == amounts[winner] {
			t.Fatalf("request %d with the winning amount got 409", i)
		}
	}
}

// Concurrent rotations never pass the 10 usable keys limit.
func TestIntegrationConcurrentRotateRespectsLimit(t *testing.T) {
	e := newE2E(t)
	actor := domain.WithActor(context.Background(), domain.SystemActor("test"))
	created, err := e.appSvc.Create(actor, service.CreateAppInput{Name: "Rotor", Slug: "rotor", Environment: domain.EnvTest})
	if err != nil {
		t.Fatal(err)
	}
	for range 8 { // Default + 8 = 9 usable keys
		if _, err := e.appSvc.CreateKey(actor, created.App.PublicID, service.CreateKeyInput{Label: "pre"}); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var ok, limited int
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.appSvc.RotateKey(actor, created.App.PublicID, created.Key.PublicID)
			var coded *domain.Error
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.As(err, &coded) && coded.Code == domain.CodeValidationFailed:
				limited++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok != 1 || limited != 5 {
		t.Fatalf("ok=%d limited=%d, want 1 and 5", ok, limited)
	}
}

// GET /v1/payments/{id} is scoped to the calling app for both the legacy
// integer and the pay_ id.
func TestIntegrationGetPaymentScopedToCallingApp(t *testing.T) {
	e := newE2E(t)
	actor := domain.WithActor(context.Background(), domain.SystemActor("test"))
	other, err := e.appSvc.Create(actor, service.CreateAppInput{Name: "Other", Slug: "other", Environment: domain.EnvLive})
	if err != nil {
		t.Fatal(err)
	}
	w := e.call(http.MethodGet, "/v1/payments/1", legacyKey, legacySecret, "")
	if w.Code != http.StatusOK {
		t.Fatalf("own legacy payment: %d %s", w.Code, w.Body.String())
	}
	var own struct{ ID string }
	_ = json.Unmarshal(w.Body.Bytes(), &own)
	if own.ID == "" {
		t.Fatal("legacy payment has no pay_ id")
	}
	expectError(t, e.call(http.MethodGet, "/v1/payments/1", other.APIKey, other.APISecret, ""), http.StatusNotFound, domain.CodeNotFound)
	expectError(t, e.call(http.MethodGet, "/v1/payments/"+own.ID, other.APIKey, other.APISecret, ""), http.StatusNotFound, domain.CodeNotFound)
	expectError(t, e.call(http.MethodGet, "/v1/payments/acme:ord-1", legacyKey, legacySecret, ""), http.StatusBadRequest, domain.CodeInvalidRequest)
}

// Against Postgres, revocation and disabling take effect on the next request
// with no cache, rotation keeps both keys working, and last_used_at is
// written once per minute.
func TestIntegrationKeyStateTakesEffectImmediately(t *testing.T) {
	e := newE2E(t)
	actor := domain.WithActor(context.Background(), domain.SystemActor("test"))
	app, err := e.appSvc.Create(actor, service.CreateAppInput{Name: "Live", Slug: "live-now", Environment: domain.EnvLive})
	if err != nil {
		t.Fatal(err)
	}
	missing := "/v1/payments/pay_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	expectError(t, e.call(http.MethodGet, missing, app.APIKey, app.APISecret, ""), http.StatusNotFound, domain.CodeNotFound)

	keyUse := func(publicID string) *domain.APIKey {
		t.Helper()
		_, page, err := e.appSvc.ListKeys(context.Background(), app.App.PublicID, domain.PageRequest{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		for i := range page.Items {
			if page.Items[i].PublicID == publicID {
				return &page.Items[i]
			}
		}
		t.Fatalf("key %s not listed", publicID)
		return nil
	}
	first := keyUse(app.Key.PublicID).LastUsedAt
	if first == nil {
		t.Fatal("last_used_at not set after a request")
	}
	expectError(t, e.call(http.MethodGet, missing, app.APIKey, app.APISecret, ""), http.StatusNotFound, domain.CodeNotFound)
	if again := keyUse(app.Key.PublicID).LastUsedAt; again == nil || !again.Equal(*first) {
		t.Fatalf("last_used_at moved within a minute: %v -> %v", first, again)
	}

	rotated, err := e.appSvc.RotateKey(actor, app.App.PublicID, app.Key.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	expectError(t, e.call(http.MethodGet, missing, rotated.APIKey, rotated.APISecret, ""), http.StatusNotFound, domain.CodeNotFound)
	expectError(t, e.call(http.MethodGet, missing, app.APIKey, app.APISecret, ""), http.StatusNotFound, domain.CodeNotFound)

	if _, _, err := e.appSvc.RevokeKey(actor, app.App.PublicID, app.Key.PublicID); err != nil {
		t.Fatal(err)
	}
	expectError(t, e.call(http.MethodGet, missing, app.APIKey, app.APISecret, ""), http.StatusUnauthorized, domain.CodeAPIKeyRevoked)
	expectError(t, e.call(http.MethodGet, missing, app.APIKey, "wrong-secret", ""), http.StatusUnauthorized, domain.CodeInvalidSignature)
	expectError(t, e.call(http.MethodGet, missing, rotated.APIKey, rotated.APISecret, ""), http.StatusNotFound, domain.CodeNotFound)

	if _, err := e.appSvc.Disable(actor, app.App.PublicID); err != nil {
		t.Fatal(err)
	}
	expectError(t, e.call(http.MethodGet, missing, rotated.APIKey, rotated.APISecret, ""), http.StatusForbidden, domain.CodeTenantInactive)
	if _, err := e.appSvc.Enable(actor, app.App.PublicID); err != nil {
		t.Fatal(err)
	}
	expectError(t, e.call(http.MethodGet, missing, rotated.APIKey, rotated.APISecret, ""), http.StatusNotFound, domain.CodeNotFound)
}
