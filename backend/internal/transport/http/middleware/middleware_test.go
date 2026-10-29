package middleware

import (
	"encoding/json"
	"io"
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

// A handler that cuts its stream off panics with http.ErrAbortHandler;
// Recover passes it on to the server instead of writing a 500 into the
// half-sent response.
func TestRecoverRepanicsAbortHandler(t *testing.T) {
	r := newEngine(func(c *gin.Context) {
		c.Writer.WriteHeader(http.StatusOK)
		_, _ = c.Writer.WriteString("partial,")
		panic(http.ErrAbortHandler)
	})
	w := httptest.NewRecorder()
	defer func() {
		if got := recover(); got != http.ErrAbortHandler {
			t.Fatalf("recovered %v, want http.ErrAbortHandler", got)
		}
		if strings.Contains(w.Body.String(), "internal_error") {
			t.Fatalf("an error envelope was written: %s", w.Body.String())
		}
	}()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	t.Fatal("the abort did not reach the server")
}

// Over a real server the aborted response is cut off: the client sees an
// error, not a complete body.
func TestAbortHandlerCutsTheConnection(t *testing.T) {
	r := newEngine(func(c *gin.Context) {
		c.Writer.WriteHeader(http.StatusOK)
		_, _ = c.Writer.WriteString("partial,")
		c.Writer.Flush()
		panic(http.ErrAbortHandler)
	})
	srv := httptest.NewServer(r)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/x")
	if err != nil {
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.ReadAll(resp.Body); err == nil {
		t.Fatal("the truncated body read as complete")
	}
}

func TestRecoverReturnsEnvelope(t *testing.T) {
	r := newEngine(func(*gin.Context) { panic("boom: secret detail") })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	assertEnvelope(t, w, http.StatusInternalServerError, domain.CodeInternal)
	if strings.Contains(w.Body.String(), "boom") {
		t.Fatalf("panic text leaked: %s", w.Body.String())
	}
}
