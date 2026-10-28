package response

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mydisha/keipay/backend/internal/domain"
)

func render(err error) (*httptest.ResponseRecorder, *gin.Context) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	Error(c, err)
	return w, c
}

func decode(t *testing.T, w *httptest.ResponseRecorder) (map[string]any, Body) {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	var env Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	return raw, env.Error
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		status  int
		code    domain.ErrorCode
		message string
	}{
		{"invalid input", domain.ErrInvalidInput, 400, domain.CodeValidationFailed, "invalid input"},
		{"invalid input wrapped", fmt.Errorf("%w: invalid slug", domain.ErrInvalidInput), 400, domain.CodeValidationFailed, "invalid input: invalid slug"},
		{"not found", domain.ErrNotFound, 404, domain.CodeNotFound, "not found"},
		{"not found wrapped", fmt.Errorf("load delivery 9: %w", domain.ErrNotFound), 404, domain.CodeNotFound, "not found"},
		{"conflict", domain.ErrConflict, 409, domain.CodeConflict, "conflict"},
		{"conflict wrapped", fmt.Errorf("%w: app slug exists", domain.ErrConflict), 409, domain.CodeConflict, "conflict"},
		{"unauthorized", domain.ErrUnauthorized, 401, domain.CodeUnauthorized, "unauthorized"},
		{"unauthorized wrapped", fmt.Errorf("verify: %w", domain.ErrUnauthorized), 401, domain.CodeUnauthorized, "unauthorized"},
		{"disabled app", domain.ErrAppDisabled, 403, domain.CodeTenantInactive, "app disabled"},
		{"disabled app wrapped", fmt.Errorf("x: %w", domain.ErrAppDisabled), 403, domain.CodeTenantInactive, "app disabled"},
		{"revoked key", domain.ErrAPIKeyRevoked, 401, domain.CodeAPIKeyRevoked, "api key revoked"},
		{"expired key", domain.ErrAPIKeyExpired, 401, domain.CodeAPIKeyExpired, "api key expired"},
		{"idempotency key reused", fmt.Errorf("create: %w", domain.ErrIdempotencyKeyReused), 409, domain.CodeIdempotencyKeyReused, "idempotency key already used for a different request"},
		{"duplicate order", domain.ErrDuplicateOrderID, 409, domain.CodeConflict, "conflict"},
		{"invalid credentials", domain.ErrInvalidCredentials, 401, domain.CodeInvalidCredentials, "invalid credentials"},
		{"invalid credentials wrapped", fmt.Errorf("login: %w", domain.ErrInvalidCredentials), 401, domain.CodeInvalidCredentials, "invalid credentials"},
		{"invalid cursor", domain.ErrInvalidCursor, 400, domain.CodeInvalidCursor, "invalid cursor"},
		{"coded", domain.NewError(domain.CodeReplayedRequest, "replayed request"), 401, domain.CodeReplayedRequest, "replayed request"},
		{"unknown", errors.New("pq: password authentication failed for user keipay"), 500, domain.CodeInternal, "internal server error"},
		{"internal wrapping a sentinel", domain.Internal(fmt.Errorf("lookup: %w", domain.ErrNotFound)), 500, domain.CodeInternal, "internal server error"},
		{"coded internal keeps generic message", &domain.Error{Code: domain.CodeInternal, Message: "db at 10.0.0.1 down"}, 500, domain.CodeInternal, "internal server error"},
		{"unmapped code", domain.NewError("made_up", "x"), 500, domain.CodeInternal, "internal server error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, c := render(tc.err)
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d", w.Code, tc.status)
			}
			raw, body := decode(t, w)
			if body.Code != tc.code || body.Message != tc.message {
				t.Fatalf("body = %+v, want %s %q", body, tc.code, tc.message)
			}
			details, ok := raw["error"].(map[string]any)["details"].(map[string]any)
			if !ok || len(details) != 0 {
				t.Fatalf("details = %#v, want {}", raw["error"].(map[string]any)["details"])
			}
			if !c.IsAborted() {
				t.Fatal("request not aborted")
			}
			if logged := len(c.Errors) > 0; logged != (tc.status == 500) {
				t.Fatalf("error attached for logging = %v, want %v", logged, tc.status == 500)
			}
		})
	}
}

func TestErrorKeepsDetails(t *testing.T) {
	err := &domain.Error{Code: domain.CodeInvalidRequest, Message: "invalid request body",
		Details: map[string]any{"fields": []domain.FieldError{{Field: "order_id", Rule: "required"}}}}
	w, _ := render(err)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	want := `{"error":{"code":"invalid_request","message":"invalid request body","details":{"fields":[{"field":"order_id","rule":"required"}]}}}`
	if w.Body.String() != want {
		t.Fatalf("body = %s, want %s", w.Body.String(), want)
	}
}

func TestEveryCodeHasAStatus(t *testing.T) {
	for _, code := range domain.ErrorCodes() {
		if _, ok := statusByCode[code]; !ok {
			t.Errorf("code %s has no HTTP status", code)
		}
	}
	if len(statusByCode) != len(domain.ErrorCodes()) {
		t.Fatalf("status table has %d codes, domain has %d", len(statusByCode), len(domain.ErrorCodes()))
	}
}

func TestAuthErrorStatuses(t *testing.T) {
	cases := []struct {
		err    error
		status int
	}{
		{domain.ErrSessionRequired, 401},
		{fmt.Errorf("load: %w", domain.ErrSessionExpired), 401},
		{domain.ErrForbidden, 403},
		{domain.ErrPasswordChangeRequired, 403},
		{domain.ErrInvalidCSRFToken, 403},
		{domain.ErrInvalidSetupToken, 403},
		{fmt.Errorf("setup: %w", domain.ErrSetupCompleted), 409},
		{fmt.Errorf("update user: %w", domain.ErrLastOwner), 409},
		{domain.ErrCannotModifySelf, 409},
		{domain.RateLimited(42), 429},
	}
	for _, tc := range cases {
		w, _ := render(tc.err)
		if w.Code != tc.status {
			t.Errorf("%v: status = %d, want %d", tc.err, w.Code, tc.status)
		}
		if got := w.Header().Get("Retry-After"); (got != "") != (tc.status == 429) {
			t.Errorf("%v: Retry-After = %q", tc.err, got)
		}
	}
}

func TestRateLimitedSetsRetryAfter(t *testing.T) {
	w, _ := render(fmt.Errorf("login: %w", domain.RateLimited(61)))
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "61" {
		t.Fatalf("status = %d, Retry-After = %q", w.Code, w.Header().Get("Retry-After"))
	}
	want := `{"error":{"code":"rate_limited","message":"too many failed attempts","details":{"retry_after_seconds":61}}}`
	if w.Body.String() != want {
		t.Fatalf("body = %s", w.Body.String())
	}
}
