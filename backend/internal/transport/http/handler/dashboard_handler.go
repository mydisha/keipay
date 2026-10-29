package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/dto"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// DashboardHandler serves the console dashboard. The router checks
// payments:read first.
type DashboardHandler struct {
	dashboard *service.DashboardService
}

// NewDashboardHandler constructs a DashboardHandler.
func NewDashboardHandler(dashboard *service.DashboardService) *DashboardHandler {
	return &DashboardHandler{dashboard: dashboard}
}

// Get handles GET /admin/dashboard.
func (h *DashboardHandler) Get(c *gin.Context) {
	env, err := queryEnum(c, "environment", domain.Environment.Valid)
	if err != nil {
		response.Error(c, err)
		return
	}
	// The service defaults and validates the range.
	q := domain.DashboardQuery{Environment: env, Range: domain.DashboardRange(c.Query("range"))}
	d, err := h.dashboard.Get(c.Request.Context(), q)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.DashboardFromDomain(d))
}
