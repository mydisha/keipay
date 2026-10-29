package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/testutil/pgtest"
	"github.com/mydisha/keipay/backend/internal/transport/http/handler"
	"github.com/mydisha/keipay/backend/internal/transport/http/middleware"
)

// failingReports streams from Postgres, then fails after failAt rows.
type failingReports struct {
	domain.PaymentReportRepository
	failAt int
}

func (r failingReports) Stream(ctx context.Context, f domain.PaymentReportFilter, fn func(*domain.PaymentReport) error) error {
	n := 0
	return r.PaymentReportRepository.Stream(ctx, f, func(rep *domain.PaymentReport) error {
		n++
		if n > r.failAt {
			return errors.New("lost the database mid-stream")
		}
		return fn(rep)
	})
}

// The export streams rows from Postgres row by row with periodic flushes; a
// database failure after the first row cuts the response off.
func TestIntegrationPaymentReportExportStreams(t *testing.T) {
	db := pgtest.New(t, 0)
	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, db.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	app := &domain.App{PublicID: id.New(id.App), Slug: "acme", Name: "Acme", Environment: domain.EnvLive, Status: domain.AppActive, WebhookSecretEnc: "x"}
	if err := postgres.NewAppRepo(pool).Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	const n = 10001
	for _, q := range []string{
		`INSERT INTO payments (public_id, app_id, order_id, external_id, amount, status, paid_at)
			SELECT 'pay_' || lpad(i::text, 26, '0'), $1, '=order-' || i, 'kp_' || i, i, 'PAID',
				timestamptz '2026-10-01 00:00:00Z' + i * interval '1 second'
			FROM generate_series(1, ` + strconv.Itoa(n) + `) AS i`,
		`INSERT INTO payment_reports (payment_id, app_id, currency, gross, status, paid_at)
			SELECT id, app_id, 'IDR', amount, 'PAID', paid_at FROM payments WHERE app_id = $1`,
	} {
		if _, err := pool.Exec(ctx, q, app.ID); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	serve := func(reports domain.PaymentReportRepository) *gin.Engine {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.Use(middleware.Recover(zap.NewNop()))
		r.GET("/export", handler.NewReportHandler(service.NewPaymentReportService(reports), zap.NewNop()).Export)
		return r
	}
	repo := postgres.NewPaymentReportRepo(pool)

	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	serve(repo).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/export?app_id="+app.PublicID, nil))
	rows := readCSV(t, w.Body.Bytes())
	if w.Code != http.StatusOK || len(rows) != n+1 {
		t.Fatalf("status %d, %d rows", w.Code, len(rows))
	}
	// Newest first, the formula guard on order ids, WIB times.
	if rows[1][0] != "pay_"+strings.Repeat("0", 26-len(strconv.Itoa(n)))+strconv.Itoa(n) || rows[1][2] != "'=order-10001" ||
		rows[1][14] != "2026-10-01T09:46:41+07:00" {
		t.Fatalf("first row = %q", rows[1])
	}
	early := 0
	for _, at := range w.flushedAt {
		if at < w.Body.Len() {
			early++
		}
	}
	if early < n/500 {
		t.Fatalf("flushed %d times before the end (%v)", early, w.flushedAt)
	}

	rec := httptest.NewRecorder()
	func() {
		defer func() {
			if got := recover(); got != http.ErrAbortHandler {
				t.Fatalf("recovered %v, want http.ErrAbortHandler", got)
			}
		}()
		serve(failingReports{PaymentReportRepository: repo, failAt: 700}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/export", nil))
		t.Fatal("the export completed")
	}()
	if body := rec.Body.String(); !strings.HasPrefix(body, "payment_id,") || strings.Contains(body, "internal_error") {
		t.Fatalf("aborted body starts %q", body[:min(len(body), 80)])
	}
}
