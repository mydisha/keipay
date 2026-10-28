package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/hmac"
)

type envelope struct {
	Error struct {
		Code    domain.ErrorCode `json:"code"`
		Message string           `json:"message"`
		Details map[string]any   `json:"details"`
	} `json:"error"`
}

func assertEnvelope(t *testing.T, w *httptest.ResponseRecorder, status int, code domain.ErrorCode) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", w.Code, status, w.Body.String())
	}
	var env envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	if env.Error.Code != code || env.Error.Details == nil {
		t.Fatalf("error = %+v, want code %s with details {}", env.Error, code)
	}
}

func newEngine(mw ...gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID(), Recover(zap.NewNop()))
	handlers := append(mw, func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	r.Any("/x", handlers...)
	return r
}

// --- AdminAuth ---

type stubVerifier struct{}

func (stubVerifier) Verify(token string) (string, error) {
	if token == "good" {
		return "admin", nil
	}
	return "", domain.ErrUnauthorized
}

func TestAdminAuthRejections(t *testing.T) {
	r := newEngine(AdminAuth(stubVerifier{}, zap.NewNop()))
	cases := []struct {
		header string
		code   domain.ErrorCode
	}{
		{"", domain.CodeMissingToken},
		{"good", domain.CodeMissingToken},
		{"Basic abc", domain.CodeMissingToken},
		{"Bearer ", domain.CodeMissingToken},
		{"Bearer bad", domain.CodeInvalidToken},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		if tc.header != "" {
			req.Header.Set("Authorization", tc.header)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assertEnvelope(t, w, http.StatusUnauthorized, tc.code)
	}
}

func TestAdminAuthSetsActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID())
	var actor domain.Actor
	var username string
	r.GET("/x", AdminAuth(stubVerifier{}, zap.NewNop()), func(c *gin.Context) {
		actor, _ = domain.ActorFromContext(c.Request.Context())
		username, _ = AdminFromContext(c)
	})
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer good")
	req.Header.Set("User-Agent", "console/1.0")
	req.Header.Set("X-Request-ID", "req-42")
	req.RemoteAddr = "198.51.100.9:5555"
	r.ServeHTTP(httptest.NewRecorder(), req)

	want := domain.Actor{Type: domain.ActorAdmin, ID: "admin", IP: "198.51.100.9", UserAgent: "console/1.0", RequestID: "req-42"}
	if actor != want {
		t.Fatalf("actor = %+v, want %+v", actor, want)
	}
	if username != "admin" {
		t.Fatalf("username = %q", username)
	}
}

// --- TenantAuth ---

type stubResolver struct {
	tenants map[string]*domain.Tenant
	err     error
}

func (s stubResolver) GetByAPIKey(_ context.Context, key string) (*domain.Tenant, error) {
	if s.err != nil {
		return nil, s.err
	}
	if t, ok := s.tenants[key]; ok {
		return t, nil
	}
	return nil, domain.ErrNotFound
}

type memNonces map[string]bool

func (m memNonces) Acquire(_ context.Context, key string) bool {
	if m[key] {
		return false
	}
	m[key] = true
	return true
}

type stubDecryptor struct{ err error }

func (s stubDecryptor) Decrypt(c string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return strings.TrimPrefix(c, "enc:"), nil
}

const tenantSecret = "sk_test_secret"

func signedRequest(apiKey, ts, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set("X-Signature", hmac.Sign(tenantSecret, []byte(ts+"."+body)))
	return req
}

func TestTenantAuth(t *testing.T) {
	resolver := stubResolver{tenants: map[string]*domain.Tenant{
		"kp_active":   {ID: 1, Active: true, APISecretEnc: "enc:" + tenantSecret},
		"kp_inactive": {ID: 2, Active: false, APISecretEnc: "enc:" + tenantSecret},
	}}
	now := strconv.FormatInt(time.Now().Unix(), 10)
	old := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)

	missingSig := signedRequest("kp_active", now, `{}`)
	missingSig.Header.Del("X-Signature")
	badSig := signedRequest("kp_active", now, `{}`)
	badSig.Header.Set("X-Signature", strings.Repeat("0", 64))

	cases := []struct {
		name   string
		req    *http.Request
		auth   gin.HandlerFunc
		status int
		code   domain.ErrorCode
	}{
		{"missing headers", missingSig, TenantAuth(resolver, memNonces{}, stubDecryptor{}, zap.NewNop()), 401, domain.CodeMissingSignatureHeaders},
		{"invalid timestamp", signedRequest("kp_active", "yesterday", `{}`), TenantAuth(resolver, memNonces{}, stubDecryptor{}, zap.NewNop()), 401, domain.CodeInvalidTimestamp},
		{"timestamp out of range", signedRequest("kp_active", old, `{}`), TenantAuth(resolver, memNonces{}, stubDecryptor{}, zap.NewNop()), 401, domain.CodeTimestampOutOfRange},
		{"unknown api key", signedRequest("kp_nope", now, `{}`), TenantAuth(resolver, memNonces{}, stubDecryptor{}, zap.NewNop()), 401, domain.CodeUnknownAPIKey},
		{"lookup failure", signedRequest("kp_active", now, `{}`), TenantAuth(stubResolver{err: errors.New("redis down")}, memNonces{}, stubDecryptor{}, zap.NewNop()), 500, domain.CodeInternal},
		{"inactive tenant", signedRequest("kp_inactive", now, `{}`), TenantAuth(resolver, memNonces{}, stubDecryptor{}, zap.NewNop()), 403, domain.CodeTenantInactive},
		{"decrypt failure", signedRequest("kp_active", now, `{}`), TenantAuth(resolver, memNonces{}, stubDecryptor{err: errors.New("bad key")}, zap.NewNop()), 500, domain.CodeInternal},
		{"invalid signature", badSig, TenantAuth(resolver, memNonces{}, stubDecryptor{}, zap.NewNop()), 401, domain.CodeInvalidSignature},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			newEngine(tc.auth).ServeHTTP(w, tc.req)
			assertEnvelope(t, w, tc.status, tc.code)
			if strings.Contains(w.Body.String(), "redis") || strings.Contains(w.Body.String(), "bad key") {
				t.Fatalf("internal error text leaked: %s", w.Body.String())
			}
		})
	}
}

func TestTenantAuthReplay(t *testing.T) {
	resolver := stubResolver{tenants: map[string]*domain.Tenant{"kp_active": {ID: 1, Active: true, APISecretEnc: "enc:" + tenantSecret}}}
	r := newEngine(TenantAuth(resolver, memNonces{}, stubDecryptor{}, zap.NewNop()))
	now := strconv.FormatInt(time.Now().Unix(), 10)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, signedRequest("kp_active", now, `{"a":1}`))
	if w.Code != http.StatusOK {
		t.Fatalf("first request: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, signedRequest("kp_active", now, `{"a":1}`))
	assertEnvelope(t, w, http.StatusUnauthorized, domain.CodeReplayedRequest)
}

// --- XenditCallbackToken ---

func TestCallbackToken(t *testing.T) {
	r := newEngine(XenditCallbackToken("cb-token", zap.NewNop()))
	for _, token := range []string{"", "wrong"} {
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		if token != "" {
			req.Header.Set("x-callback-token", token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assertEnvelope(t, w, http.StatusUnauthorized, domain.CodeInvalidCallbackToken)
	}
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("x-callback-token", "cb-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("valid token: %d", w.Code)
	}
}

// --- Recover ---

func TestRecoverReturnsEnvelope(t *testing.T) {
	r := newEngine(func(*gin.Context) { panic("boom: secret detail") })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	assertEnvelope(t, w, http.StatusInternalServerError, domain.CodeInternal)
	if strings.Contains(w.Body.String(), "boom") {
		t.Fatalf("panic text leaked: %s", w.Body.String())
	}
}
