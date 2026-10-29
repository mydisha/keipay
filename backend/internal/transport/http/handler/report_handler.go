package handler

import (
	"regexp"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/dto"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// reportCSVHeader is the column order of a payment report export.
var reportCSVHeader = []string{"payment_id", "reference", "order_id", "app_id", "app_slug", "app_environment",
	"connection_id", "connection_name", "provider", "currency", "gross", "fee", "net", "status", "paid_at", "settled_at"}

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

// ReportHandler serves payment reports. The router checks payments:read
// first.
type ReportHandler struct {
	reports *service.PaymentReportService
	log     *zap.Logger
}

// NewReportHandler constructs a ReportHandler; log receives aborted exports.
func NewReportHandler(reports *service.PaymentReportService, log *zap.Logger) *ReportHandler {
	if log == nil {
		log = zap.NewNop()
	}
	return &ReportHandler{reports: reports, log: log}
}

// List handles GET /admin/payment-reports (cursor-paginated, newest
// payment first).
func (h *ReportHandler) List(c *gin.Context) {
	f, err := reportFilter(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	req, err := pageRequest(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	page, err := h.reports.List(c.Request.Context(), f, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.PaymentReportListFromDomain(page))
}

// Summary handles GET /admin/payment-reports/summary.
func (h *ReportHandler) Summary(c *gin.Context) {
	f, err := reportFilter(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	// The service defaults and validates the interval.
	interval := domain.ReportInterval(c.Query("interval"))
	rows, err := h.reports.Summary(c.Request.Context(), f, interval)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.PaymentReportSummaryFromDomain(interval.OrTotal(), *f.From, *f.To, rows))
}

// Export handles GET /admin/payment-reports/export: every matching report,
// streamed as CSV.
func (h *ReportHandler) Export(c *gin.Context) {
	f, err := reportFilter(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	filename := "keipay-payment-reports-" + csvDate(f.From) + "-" + csvDate(f.To) + ".csv"
	streamCSV(c, h.log, filename, reportCSVHeader, func(write func([]string) error) error {
		return h.reports.Export(c.Request.Context(), f, func(r *domain.PaymentReport) error {
			return write(reportCSVRow(r))
		})
	})
}

func reportCSVRow(r *domain.PaymentReport) []string {
	var connID, connName string
	if r.Connection != nil {
		connID, connName = r.Connection.PublicID, r.Connection.Name
	}
	return []string{
		r.PaymentPublicID, csvText(r.Reference), csvText(r.OrderID), r.App.PublicID, csvText(r.App.Slug),
		string(r.App.Environment), connID, csvText(connName), csvText(r.Provider()), csvText(r.Currency),
		csvInt(&r.Gross), csvInt(r.Fee), csvInt(r.Net), string(r.Status), csvTime(&r.PaidAt), csvTime(r.SettledAt),
	}
}

// reportFilter parses the filters shared by the list, summary and export.
func reportFilter(c *gin.Context) (domain.PaymentReportFilter, error) {
	var f domain.PaymentReportFilter
	var err error
	if f.AppPublicID, err = queryPublicID(c, "app_id", id.App); err != nil {
		return f, err
	}
	if f.ConnectionPublicID, err = queryPublicID(c, "connection_id", id.Connection); err != nil {
		return f, err
	}
	if f.Environment, err = queryEnum(c, "environment", domain.Environment.Valid); err != nil {
		return f, err
	}
	if v := c.Query("currency"); v != "" {
		if !currencyRe.MatchString(v) {
			return f, invalidRequest("invalid query parameter", domain.FieldError{Field: "currency", Rule: service.RuleFormat})
		}
		f.Currency = &v
	}
	if f.From, err = queryTime(c, "from"); err != nil {
		return f, err
	}
	f.To, err = queryTime(c, "to")
	return f, err
}
