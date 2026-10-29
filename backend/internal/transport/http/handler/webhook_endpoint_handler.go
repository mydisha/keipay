package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/dto"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// WebhookEndpointHandler serves the event catalog and the webhook endpoints
// of apps. The router checks apps:read or apps:write first. Request bodies
// may carry header values, so they are never logged.
type WebhookEndpointHandler struct {
	endpoints *service.WebhookEndpointService
}

// NewWebhookEndpointHandler constructs a WebhookEndpointHandler.
func NewWebhookEndpointHandler(endpoints *service.WebhookEndpointService) *WebhookEndpointHandler {
	return &WebhookEndpointHandler{endpoints: endpoints}
}

// EventTypes handles GET /admin/event-types.
func (h *WebhookEndpointHandler) EventTypes(c *gin.Context) {
	response.Success(c, dto.EventTypeListFromDomain(h.endpoints.EventTypes()))
}

// List handles GET /admin/apps/{id}/webhook-endpoints (cursor-paginated).
func (h *WebhookEndpointHandler) List(c *gin.Context) {
	appID, err := pathPublicID(c, "id", id.App)
	if err != nil {
		response.Error(c, err)
		return
	}
	req, err := pageRequest(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	page, err := h.endpoints.List(c.Request.Context(), appID, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.WebhookEndpointListFromDomain(page))
}

type headerRequest struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type retryPolicyRequest struct {
	MaxAttempts     *int    `json:"max_attempts"`
	Backoff         *string `json:"backoff"`
	IntervalSeconds *int    `json:"interval_seconds"`
}

func (r *retryPolicyRequest) input() *service.RetryPolicyInput {
	if r == nil {
		return nil
	}
	return &service.RetryPolicyInput{MaxAttempts: r.MaxAttempts, Backoff: r.Backoff, IntervalSeconds: r.IntervalSeconds}
}

func headerInputs(hs []headerRequest) []service.WebhookHeaderInput {
	out := make([]service.WebhookHeaderInput, 0, len(hs))
	for _, v := range hs {
		out = append(out, service.WebhookHeaderInput{Name: v.Name, Value: v.Value})
	}
	return out
}

type createEndpointRequest struct {
	URL             string              `json:"url"`
	Description     string              `json:"description"`
	Events          []string            `json:"events"`
	APIVersion      *string             `json:"api_version"`
	LegacySignature bool                `json:"legacy_signature"`
	Headers         []headerRequest     `json:"headers"`
	RetryPolicy     *retryPolicyRequest `json:"retry_policy"`
}

// Create handles POST /admin/apps/{id}/webhook-endpoints. The secret in the
// response is shown once.
func (h *WebhookEndpointHandler) Create(c *gin.Context) {
	appID, err := pathPublicID(c, "id", id.App)
	if err != nil {
		response.Error(c, err)
		return
	}
	var req createEndpointRequest
	if err := decodeJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	created, err := h.endpoints.Create(c.Request.Context(), appID, service.CreateWebhookEndpointInput{
		URL: req.URL, Description: req.Description, Events: req.Events, APIVersion: req.APIVersion,
		LegacySignature: req.LegacySignature, Headers: headerInputs(req.Headers), RetryPolicy: req.RetryPolicy.input(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, dto.WebhookEndpointSecretFromService(created))
}

// Get handles GET /admin/webhook-endpoints/{id}.
func (h *WebhookEndpointHandler) Get(c *gin.Context) {
	endpointID, err := pathPublicID(c, "id", id.WebhookEndpoint)
	if err != nil {
		response.Error(c, err)
		return
	}
	e, err := h.endpoints.Get(c.Request.Context(), endpointID)
	respondEndpoint(c, e, err)
}

// Update handles PATCH /admin/webhook-endpoints/{id}. The body is read as
// raw fields so that sending status, secret or app_id is detected whatever
// its value.
func (h *WebhookEndpointHandler) Update(c *gin.Context) {
	endpointID, err := pathPublicID(c, "id", id.WebhookEndpoint)
	if err != nil {
		response.Error(c, err)
		return
	}
	var body map[string]json.RawMessage
	if err := decodeJSON(c, &body); err != nil {
		response.Error(c, err)
		return
	}
	in, err := updateEndpointInput(body)
	if err != nil {
		response.Error(c, err)
		return
	}
	e, err := h.endpoints.Update(c.Request.Context(), endpointID, in)
	respondEndpoint(c, e, err)
}

// updateEndpointInput maps the present fields of a PATCH body. A field
// holding null or another JSON type fails with rule type.
func updateEndpointInput(body map[string]json.RawMessage) (service.UpdateWebhookEndpointInput, error) {
	var in service.UpdateWebhookEndpointInput
	_, in.Status = body["status"]
	_, in.Secret = body["secret"]
	_, in.AppID = body["app_id"]
	var fields []domain.FieldError
	decode := func(name string, dst any) bool {
		raw, ok := body[name]
		if !ok {
			return false
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, dst) != nil {
			fields = append(fields, domain.FieldError{Field: name, Rule: "type"})
			return false
		}
		return true
	}
	var url, description, apiVersion string
	var events []string
	var legacy bool
	var headers []headerRequest
	var retry retryPolicyRequest
	if decode("url", &url) {
		in.URL = &url
	}
	if decode("description", &description) {
		in.Description = &description
	}
	if decode("events", &events) {
		in.Events = &events
	}
	if decode("api_version", &apiVersion) {
		in.APIVersion = &apiVersion
	}
	if decode("legacy_signature", &legacy) {
		in.LegacySignature = &legacy
	}
	if decode("headers", &headers) {
		hs := headerInputs(headers)
		in.Headers = &hs
	}
	if decode("retry_policy", &retry) {
		in.RetryPolicy = retry.input()
	}
	if len(fields) > 0 {
		return in, invalidBody(fields...)
	}
	return in, nil
}

// Delete handles DELETE /admin/webhook-endpoints/{id}.
func (h *WebhookEndpointHandler) Delete(c *gin.Context) {
	endpointID, err := pathPublicID(c, "id", id.WebhookEndpoint)
	if err != nil {
		response.Error(c, err)
		return
	}
	if err := h.endpoints.Delete(c.Request.Context(), endpointID); err != nil {
		response.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type rotateSecretRequest struct {
	OverlapHours *int `json:"overlap_hours"`
}

// RotateSecret handles POST /admin/webhook-endpoints/{id}/rotate-secret. The
// body is optional; the new secret in the response is shown once.
func (h *WebhookEndpointHandler) RotateSecret(c *gin.Context) {
	endpointID, err := pathPublicID(c, "id", id.WebhookEndpoint)
	if err != nil {
		response.Error(c, err)
		return
	}
	var req rotateSecretRequest
	if err := decodeOptionalJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	rotated, err := h.endpoints.RotateSecret(c.Request.Context(), endpointID, req.OverlapHours)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.WebhookEndpointSecretFromService(rotated))
}

// Disable handles POST /admin/webhook-endpoints/{id}/disable.
func (h *WebhookEndpointHandler) Disable(c *gin.Context) {
	h.setStatus(c, h.endpoints.Disable)
}

// Enable handles POST /admin/webhook-endpoints/{id}/enable.
func (h *WebhookEndpointHandler) Enable(c *gin.Context) {
	h.setStatus(c, h.endpoints.Enable)
}

func (h *WebhookEndpointHandler) setStatus(c *gin.Context, fn func(ctx context.Context, publicID string) (*domain.WebhookEndpoint, error)) {
	endpointID, err := pathPublicID(c, "id", id.WebhookEndpoint)
	if err != nil {
		response.Error(c, err)
		return
	}
	e, err := fn(c.Request.Context(), endpointID)
	respondEndpoint(c, e, err)
}

type sendTestRequest struct {
	EventType string `json:"event_type"`
}

// SendTest handles POST /admin/webhook-endpoints/{id}/test.
func (h *WebhookEndpointHandler) SendTest(c *gin.Context) {
	endpointID, err := pathPublicID(c, "id", id.WebhookEndpoint)
	if err != nil {
		response.Error(c, err)
		return
	}
	var req sendTestRequest
	if err := decodeJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	d, err := h.endpoints.SendTest(c.Request.Context(), endpointID, req.EventType)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Accepted(c, dto.WebhookDeliveryResponse{Delivery: dto.WebhookDeliveryFromDomain(d)})
}

func respondEndpoint(c *gin.Context, e *domain.WebhookEndpoint, err error) {
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.WebhookEndpointResponse{Endpoint: dto.WebhookEndpointFromDomain(e)})
}

// decodeOptionalJSON is decodeJSON for an optional body: an empty one leaves
// dst untouched.
func decodeOptionalJSON(c *gin.Context, dst any) error {
	if c.Request.Body == nil {
		return nil
	}
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return readError(err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	return decodeJSON(c, dst)
}
