package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// readAll is a handler that reads its whole body and reports how it went.
func readAll(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	switch {
	case TooLarge(err):
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"read": "too large"})
	case err != nil:
		c.JSON(http.StatusBadRequest, gin.H{"read": err.Error()})
	default:
		c.JSON(http.StatusOK, gin.H{"bytes": len(body)})
	}
}

func TestMaxBody_DeclaredLengthOverLimitIsRefusedUnread(t *testing.T) {
	handled := false
	r := newEngine(MaxBody(16), func(c *gin.Context) { handled = true; c.Next() }, readAll)
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(strings.Repeat("a", 17)))
	w := serve(r, req)
	assertEnvelope(t, w, http.StatusRequestEntityTooLarge, domain.CodeRequestTooLarge)
	if handled {
		t.Fatal("the handler ran for a body declared over the limit")
	}
	if w.Header().Get("Connection") != "close" {
		t.Fatalf("Connection = %q, want close", w.Header().Get("Connection"))
	}
}

// Without a Content-Length the limit holds while the body is read.
func TestMaxBody_UndeclaredLengthFailsOnRead(t *testing.T) {
	r := newEngine(MaxBody(16), readAll)
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(strings.Repeat("a", 17)))
	req.ContentLength = -1
	if w := serve(r, req); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}

func TestMaxBody_UnderLimitAndNoBodyPass(t *testing.T) {
	r := newEngine(MaxBody(16), readAll)
	if w := serve(r, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(strings.Repeat("a", 16)))); w.Code != http.StatusOK {
		t.Fatalf("at the limit: %d %s", w.Code, w.Body.String())
	}
	if w := serve(r, httptest.NewRequest(http.MethodGet, "/x", nil)); w.Code != http.StatusOK {
		t.Fatalf("no body: %d %s", w.Code, w.Body.String())
	}
}

func TestTooLarge(t *testing.T) {
	if TooLarge(nil) || TooLarge(io.ErrUnexpectedEOF) {
		t.Fatal("ordinary errors reported as too large")
	}
	if !TooLarge(&http.MaxBytesError{Limit: 1}) {
		t.Fatal("MaxBytesError not recognised")
	}
}
