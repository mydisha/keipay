package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/dto"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// DeliveryHandler serves the two webhook logs: outbound deliveries to apps
// and inbound gateway callbacks. The router checks deliveries:read or
// deliveries:replay first.
type DeliveryHandler struct {
	deliveries *service.DeliveryService
	inbound    *service.InboundService
}

// NewDeliveryHandler constructs a DeliveryHandler.
func NewDeliveryHandler(deliveries *service.DeliveryService, inbound *service.InboundService) *DeliveryHandler {
	return &DeliveryHandler{deliveries: deliveries, inbound: inbound}
}

// ListDeliveries handles GET /admin/webhook-deliveries (cursor-paginated,
// newest first). Every filter is validated strictly.
func (h *DeliveryHandler) ListDeliveries(c *gin.Context) {
	f, err := deliveryFilter(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	req, err := pageRequest(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	page, err := h.deliveries.List(c.Request.Context(), f, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.DeliveryListFromDomain(page))
}

func deliveryFilter(c *gin.Context) (domain.DeliveryFilter, error) {
	var f domain.DeliveryFilter
	var err error
	if f.AppPublicID, err = queryPublicID(c, "app_id", id.App); err != nil {
		return f, err
	}
	if f.EndpointPublicID, err = queryPublicID(c, "endpoint_id", id.WebhookEndpoint); err != nil {
		return f, err
	}
	if f.EventPublicID, err = queryPublicID(c, "event_id", id.Event); err != nil {
		return f, err
	}
	if f.EventType, err = queryEnum(c, "event_type", domain.EventType.Known); err != nil {
		return f, err
	}
	if f.Status, err = queryEnum(c, "status", domain.WebhookDeliveryStatus.Valid); err != nil {
		return f, err
	}
	if f.Environment, err = queryEnum(c, "environment", domain.Environment.Valid); err != nil {
		return f, err
	}
	if f.From, err = queryTime(c, "from"); err != nil {
		return f, err
	}
	f.To, err = queryTime(c, "to")
	return f, err
}

// GetDelivery handles GET /admin/webhook-deliveries/{id}.
func (h *DeliveryHandler) GetDelivery(c *gin.Context) {
	deliveryID, err := pathPublicID(c, "id", id.Delivery)
	if err != nil {
		response.Error(c, err)
		return
	}
	d, attempts, err := h.deliveries.Get(c.Request.Context(), deliveryID)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.WebhookDeliveryDetailFromDomain(d, attempts))
}

// ReplayDelivery handles POST /admin/webhook-deliveries/{id}/replay.
func (h *DeliveryHandler) ReplayDelivery(c *gin.Context) {
	deliveryID, err := pathPublicID(c, "id", id.Delivery)
	if err != nil {
		response.Error(c, err)
		return
	}
	d, err := h.deliveries.Replay(c.Request.Context(), deliveryID)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Accepted(c, dto.WebhookDeliveryResponse{Delivery: dto.WebhookDeliveryFromDomain(d)})
}

// ListInbound handles GET /admin/inbound-webhooks (cursor-paginated, newest
// first). Every filter is validated strictly.
func (h *DeliveryHandler) ListInbound(c *gin.Context) {
	var f domain.InboundFilter
	var err error
	if f.ConnectionPublicID, err = queryPublicID(c, "connection_id", id.Connection); err != nil {
		response.Error(c, err)
		return
	}
	if f.Result, err = queryEnum(c, "result", domain.InboundResult.Valid); err != nil {
		response.Error(c, err)
		return
	}
	if f.Environment, err = queryEnum(c, "environment", domain.Environment.Valid); err != nil {
		response.Error(c, err)
		return
	}
	if f.From, err = queryTime(c, "from"); err != nil {
		response.Error(c, err)
		return
	}
	if f.To, err = queryTime(c, "to"); err != nil {
		response.Error(c, err)
		return
	}
	req, err := pageRequest(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	page, err := h.inbound.List(c.Request.Context(), f, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.InboundWebhookListFromDomain(page))
}

// GetInbound handles GET /admin/inbound-webhooks/{id}.
func (h *DeliveryHandler) GetInbound(c *gin.Context) {
	inboundID, err := pathPublicID(c, "id", id.Inbound)
	if err != nil {
		response.Error(c, err)
		return
	}
	w, err := h.inbound.Get(c.Request.Context(), inboundID)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.InboundWebhookDetailFromDomain(w))
}
