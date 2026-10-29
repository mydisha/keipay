package service

import (
	"context"
	"fmt"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// Field rules for time ranges, reported in details.fields.
const (
	RuleDatetime    = "datetime"
	RuleAfter       = "after"
	RuleMaxRange    = "max_range"
	RulePast        = "past"
	RuleUnsupported = "unsupported"
)

// Summary range limits: a day-by-day summary has at most 92 rows per group,
// the others cover at most a year.
const (
	maxSummaryRange    = 366 * 24 * time.Hour
	maxDaySummaryRange = 92 * 24 * time.Hour
)

// PaymentReportService lists, totals and exports payment reports.
type PaymentReportService struct {
	reports domain.PaymentReportRepository
}

// NewPaymentReportService constructs a PaymentReportService.
func NewPaymentReportService(reports domain.PaymentReportRepository) *PaymentReportService {
	return &PaymentReportService{reports: reports}
}

// List returns one page of reports, newest payment first.
func (s *PaymentReportService) List(ctx context.Context, f domain.PaymentReportFilter, req domain.PageRequest) (domain.PageResult[domain.PaymentReport], error) {
	if err := rangeOrder(f.From, f.To); err != nil {
		return domain.PageResult[domain.PaymentReport]{}, err
	}
	p, err := req.Page()
	if err != nil {
		return domain.PageResult[domain.PaymentReport]{}, err
	}
	res, err := s.reports.List(ctx, f, p)
	if err != nil {
		return domain.PageResult[domain.PaymentReport]{}, fmt.Errorf("list payment reports: %w", err)
	}
	return res, nil
}

// Summary totals the reports paid within [f.From, f.To) per period of
// interval ("" is total), app, connection and currency. Both bounds are
// required.
func (s *PaymentReportService) Summary(ctx context.Context, f domain.PaymentReportFilter, interval domain.ReportInterval) ([]domain.PaymentReportSummaryRow, error) {
	interval = interval.OrTotal()
	var errs fieldErrors
	if f.From == nil {
		errs.check("from", RuleRequired)
	}
	if f.To == nil {
		errs.check("to", RuleRequired)
	}
	if !interval.Valid() {
		errs.check("interval", RuleOneOf)
	}
	if len(errs) > 0 {
		return nil, invalidQuery(errs...)
	}
	if err := rangeOrder(f.From, f.To); err != nil {
		return nil, err
	}
	limit := maxSummaryRange
	if interval == domain.ReportIntervalDay {
		limit = maxDaySummaryRange
	}
	if f.To.Sub(*f.From) > limit {
		return nil, invalidQuery(domain.FieldError{Field: "to", Rule: RuleMaxRange})
	}
	rows, err := s.reports.Summary(ctx, f, interval)
	if err != nil {
		return nil, fmt.Errorf("summarize payment reports: %w", err)
	}
	return rows, nil
}

// Export validates f, then calls fn with every matching report, newest
// first. fn is never called when f is invalid.
func (s *PaymentReportService) Export(ctx context.Context, f domain.PaymentReportFilter, fn func(*domain.PaymentReport) error) error {
	if err := rangeOrder(f.From, f.To); err != nil {
		return err
	}
	return s.reports.Stream(ctx, f, fn)
}

// rangeOrder refuses a range whose end is not after its start.
func rangeOrder(from, to *time.Time) error {
	if from != nil && to != nil && !to.After(*from) {
		return invalidQuery(domain.FieldError{Field: "to", Rule: RuleAfter})
	}
	return nil
}

// invalidQuery is an invalid_request error about query parameters.
func invalidQuery(fields ...domain.FieldError) *domain.Error {
	err := domain.InvalidFields(fields...)
	err.Message = "invalid query parameter"
	return err
}
