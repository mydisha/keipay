package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
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
