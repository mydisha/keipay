package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/dto"
	"github.com/mydisha/keipay/backend/internal/transport/http/middleware"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// itemCSVHeader is the column order of a reconciliation item export.
var itemCSVHeader = []string{"item_id", "reference", "result", "resolved", "resolution_note", "resolved_at",
	"payment_id", "app_slug", "keipay_status", "keipay_amount", "keipay_currency", "keipay_fee", "keipay_paid_at",
	"gateway_payment_id", "gateway_status", "gateway_provider_status", "gateway_amount", "gateway_currency",
	"gateway_fee", "gateway_occurred_at"}

// ReconciliationHandler serves reconciliation runs and their items. The
// router checks payments:read or reconciliation:write first.
type ReconciliationHandler struct {
	runs *service.ReconciliationService
	log  *zap.Logger
}

// NewReconciliationHandler constructs a ReconciliationHandler; log receives
// aborted exports.
func NewReconciliationHandler(runs *service.ReconciliationService, log *zap.Logger) *ReconciliationHandler {
	if log == nil {
		log = zap.NewNop()
	}
	return &ReconciliationHandler{runs: runs, log: log}
}

// ListRuns handles GET /admin/reconciliation-runs (cursor-paginated, newest
// first).
func (h *ReconciliationHandler) ListRuns(c *gin.Context) {
	var f domain.ReconciliationRunFilter
	var err error
	if f.ConnectionPublicID, err = queryPublicID(c, "connection_id", id.Connection); err != nil {
		response.Error(c, err)
		return
	}
	if f.Status, err = queryEnum(c, "status", domain.ReconciliationRunStatus.Valid); err != nil {
		response.Error(c, err)
		return
	}
	if f.Environment, err = queryEnum(c, "environment", domain.Environment.Valid); err != nil {
		response.Error(c, err)
		return
	}
	req, err := pageRequest(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	page, err := h.runs.List(c.Request.Context(), f, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.ReconciliationRunListFromDomain(page))
}

// CreateRun handles POST /admin/reconciliation-runs.
func (h *ReconciliationHandler) CreateRun(c *gin.Context) {
	var req dto.CreateReconciliationRunRequest
	if err := decodeJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	run, err := h.runs.CreateRun(c.Request.Context(), service.CreateRunInput{
		ConnectionID: req.ConnectionID, From: req.From, To: req.To, CreatedByID: currentUserID(c),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, dto.ReconciliationRunResponse{Run: dto.ReconciliationRunFromDomain(run)})
}

// GetRun handles GET /admin/reconciliation-runs/{id}.
func (h *ReconciliationHandler) GetRun(c *gin.Context) {
	runID, err := pathPublicID(c, "id", id.Reconciliation)
	if err != nil {
		response.Error(c, err)
		return
	}
	run, err := h.runs.Get(c.Request.Context(), runID)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.ReconciliationRunResponse{Run: dto.ReconciliationRunFromDomain(run)})
}

// RerunRun handles POST /admin/reconciliation-runs/{id}/rerun.
func (h *ReconciliationHandler) RerunRun(c *gin.Context) {
	runID, err := pathPublicID(c, "id", id.Reconciliation)
	if err != nil {
		response.Error(c, err)
		return
	}
	run, err := h.runs.Rerun(c.Request.Context(), runID)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Accepted(c, dto.ReconciliationRunResponse{Run: dto.ReconciliationRunFromDomain(run)})
}

// ListItems handles GET /admin/reconciliation-runs/{id}/items
// (cursor-paginated, newest first).
func (h *ReconciliationHandler) ListItems(c *gin.Context) {
	runID, f, err := itemQuery(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	req, err := pageRequest(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	page, err := h.runs.ListItems(c.Request.Context(), runID, f, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.ReconciliationItemListFromDomain(page))
}

// ExportItems handles GET /admin/reconciliation-runs/{id}/items/export:
// every matching item, streamed as CSV.
func (h *ReconciliationHandler) ExportItems(c *gin.Context) {
	runID, f, err := itemQuery(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	streamCSV(c, h.log, "keipay-reconciliation-"+runID+".csv", itemCSVHeader, func(write func([]string) error) error {
		return h.runs.ExportItems(c.Request.Context(), runID, f, func(it *domain.ReconciliationItem) error {
			return write(itemCSVRow(it))
		})
	})
}

func itemCSVRow(it *domain.ReconciliationItem) []string {
	row := make([]string, 0, len(itemCSVHeader))
	var note, resolvedAt string
	if r := it.Resolution; r != nil {
		note, resolvedAt = csvText(r.Note), csvTime(&r.ResolvedAt)
	}
	row = append(row, it.PublicID, csvText(it.Reference), string(it.Result), strconv.FormatBool(it.Resolution != nil),
		note, resolvedAt)
	if k := it.Keipay; k != nil {
		row = append(row, k.PaymentPublicID, csvText(k.App.Slug), string(k.Status), csvInt(&k.Amount),
			csvText(k.Currency), csvInt(k.Fee), csvTime(k.PaidAt))
	} else {
		row = append(row, "", "", "", "", "", "", "")
	}
	if g := it.Gateway; g != nil {
		row = append(row, csvText(g.ProviderPaymentID), string(g.Status), csvText(g.ProviderStatus), csvInt(&g.Amount),
			csvText(g.Currency), csvInt(g.Fee), csvTime(&g.OccurredAt))
	} else {
		row = append(row, "", "", "", "", "", "", "")
	}
	return row
}

// itemQuery parses the run id and item filters shared by the list and the
// export.
func itemQuery(c *gin.Context) (string, domain.ReconciliationItemFilter, error) {
	var f domain.ReconciliationItemFilter
	runID, err := pathPublicID(c, "id", id.Reconciliation)
	if err != nil {
		return "", f, err
	}
	if f.Result, err = queryEnum(c, "result", domain.ReconciliationResult.Valid); err != nil {
		return "", f, err
	}
	switch v := c.Query("resolved"); v {
	case "":
	case "true", "false":
		resolved := v == "true"
		f.Resolved = &resolved
	default:
		return "", f, invalidRequest("invalid query parameter", domain.FieldError{Field: "resolved", Rule: "bool"})
	}
	return runID, f, nil
}

// GetItem handles GET /admin/reconciliation-items/{id}.
func (h *ReconciliationHandler) GetItem(c *gin.Context) {
	itemID, err := pathPublicID(c, "id", id.ReconciliationItem)
	if err != nil {
		response.Error(c, err)
		return
	}
	item, err := h.runs.GetItem(c.Request.Context(), itemID)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.ReconciliationItemResponse{Item: dto.ReconciliationItemFromDomain(item)})
}

// ResolveItem handles POST /admin/reconciliation-items/{id}/resolve.
func (h *ReconciliationHandler) ResolveItem(c *gin.Context) {
	itemID, err := pathPublicID(c, "id", id.ReconciliationItem)
	if err != nil {
		response.Error(c, err)
		return
	}
	var req dto.ResolveReconciliationItemRequest
	if err := decodeJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	item, err := h.runs.ResolveItem(c.Request.Context(), itemID, req.Note, currentUserID(c))
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.ReconciliationItemResponse{Item: dto.ReconciliationItemFromDomain(item)})
}

// currentUserID is the internal id of the signed-in user, nil without one.
func currentUserID(c *gin.Context) *int64 {
	u, _, ok := middleware.CurrentUser(c)
	if !ok {
		return nil
	}
	return &u.ID
}
