package service

import (
	"context"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// LedgerService exposes per-tenant reconciliation reports.
type LedgerService struct {
	repo domain.LedgerRepository
}

// NewLedgerService constructs a LedgerService.
func NewLedgerService(repo domain.LedgerRepository) *LedgerService {
	return &LedgerService{repo: repo}
}

// List returns ledger entries matching the filter.
func (s *LedgerService) List(ctx context.Context, f domain.LedgerFilter) ([]domain.LedgerEntry, error) {
	return s.repo.List(ctx, f)
}

// Summary returns aggregated totals per tenant.
func (s *LedgerService) Summary(ctx context.Context, f domain.LedgerFilter) ([]domain.LedgerSummary, error) {
	return s.repo.Summary(ctx, f)
}
