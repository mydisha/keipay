package handler

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/dto"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// AppHandler serves /admin/apps and the apps' API keys. The router checks
// apps:read or apps:write before these handlers run.
type AppHandler struct {
	apps *service.AppService
	now  func() time.Time
}

// NewAppHandler constructs an AppHandler.
func NewAppHandler(apps *service.AppService) *AppHandler {
	return &AppHandler{apps: apps, now: time.Now}
}

// List handles GET /admin/apps (cursor-paginated, newest first).
func (h *AppHandler) List(c *gin.Context) {
	env, err := queryEnum(c, "environment", domain.Environment.Valid)
	if err != nil {
		response.Error(c, err)
		return
	}
	status, err := queryEnum(c, "status", domain.AppStatus.Valid)
	if err != nil {
		response.Error(c, err)
		return
	}
	req, err := pageRequest(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	page, err := h.apps.List(c.Request.Context(), domain.AppFilter{Environment: env, Status: status}, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.AppListFromDomain(page))
}

// Create handles POST /admin/apps. The secrets in the response are shown once.
func (h *AppHandler) Create(c *gin.Context) {
	var req dto.CreateAppRequest
	if err := bindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	created, err := h.apps.Create(c.Request.Context(), service.CreateAppInput{
		Name:               req.Name,
		Slug:               req.Slug,
		Environment:        req.Environment,
		WebhookURL:         req.WebhookURL,
		RedirectSuccessURL: req.RedirectSuccessURL,
		RedirectFailureURL: req.RedirectFailureURL,
		Metadata:           req.Metadata,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, dto.AppCreatedFromService(created, h.now()))
}

// Get handles GET /admin/apps/{id}.
func (h *AppHandler) Get(c *gin.Context) {
	appID, err := pathPublicID(c, "id", id.App)
	if err != nil {
		response.Error(c, err)
		return
	}
	app, err := h.apps.Get(c.Request.Context(), appID)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.AppResponse{App: dto.AppFromDomain(app)})
}

// Update handles PATCH /admin/apps/{id}. The body is read as raw fields so
// that sending slug or environment is detected even with an empty value.
func (h *AppHandler) Update(c *gin.Context) {
	appID, err := pathPublicID(c, "id", id.App)
	if err != nil {
		response.Error(c, err)
		return
	}
	var body map[string]json.RawMessage
	if err := decodeJSON(c, &body); err != nil {
		response.Error(c, err)
		return
	}
	in, err := updateAppInput(body)
	if err != nil {
		response.Error(c, err)
		return
	}
	app, err := h.apps.Update(c.Request.Context(), appID, in)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.AppResponse{App: dto.AppFromDomain(app)})
}

// updateAppInput maps the present fields of a PATCH body; a string field
// holding another JSON type fails with rule type. default_connection_id may
// also be null, which clears it.
func updateAppInput(body map[string]json.RawMessage) (service.UpdateAppInput, error) {
	in := service.UpdateAppInput{Metadata: body["metadata"]}
	_, in.Slug = body["slug"]
	_, in.Environment = body["environment"]
	var fields []domain.FieldError
	for _, f := range []struct {
		name string
		dst  **string
	}{
		{"name", &in.Name},
		{"webhook_url", &in.WebhookURL},
		{"redirect_success_url", &in.RedirectSuccessURL},
		{"redirect_failure_url", &in.RedirectFailureURL},
	} {
		raw, ok := body[f.name]
		if !ok {
			continue
		}
		var v *string
		if err := json.Unmarshal(raw, &v); err != nil || v == nil {
			fields = append(fields, domain.FieldError{Field: f.name, Rule: "type"})
			continue
		}
		*f.dst = v
	}
	if raw, ok := body["default_connection_id"]; ok {
		in.DefaultConnectionSet = true
		if err := json.Unmarshal(raw, &in.DefaultConnectionID); err != nil {
			fields = append(fields, domain.FieldError{Field: "default_connection_id", Rule: "type"})
		}
	}
	if len(fields) > 0 {
		return in, invalidBody(fields...)
	}
	return in, nil
}

// Disable handles POST /admin/apps/{id}/disable.
func (h *AppHandler) Disable(c *gin.Context) {
	h.setStatus(c, h.apps.Disable)
}

// Enable handles POST /admin/apps/{id}/enable.
func (h *AppHandler) Enable(c *gin.Context) {
	h.setStatus(c, h.apps.Enable)
}

func (h *AppHandler) setStatus(c *gin.Context, fn func(ctx context.Context, publicID string) (*domain.App, error)) {
	appID, err := pathPublicID(c, "id", id.App)
	if err != nil {
		response.Error(c, err)
		return
	}
	app, err := fn(c.Request.Context(), appID)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.AppResponse{App: dto.AppFromDomain(app)})
}

// ListKeys handles GET /admin/apps/{id}/keys.
func (h *AppHandler) ListKeys(c *gin.Context) {
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
	app, page, err := h.apps.ListKeys(c.Request.Context(), appID, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.APIKeyListFromDomain(app, page, h.now()))
}

// CreateKey handles POST /admin/apps/{id}/keys. The key and secret in the
// response are shown once.
func (h *AppHandler) CreateKey(c *gin.Context) {
	appID, err := pathPublicID(c, "id", id.App)
	if err != nil {
		response.Error(c, err)
		return
	}
	var req dto.CreateAPIKeyRequest
	if err := bindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	created, err := h.apps.CreateKey(c.Request.Context(), appID, service.CreateKeyInput{Label: req.Label, ExpiresAt: req.ExpiresAt})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, dto.APIKeyCreatedFromService(created, h.now()))
}

// RotateKey handles POST /admin/apps/{id}/keys/{key_id}/rotate.
func (h *AppHandler) RotateKey(c *gin.Context) {
	appID, keyID, err := pathAppKeyIDs(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	created, err := h.apps.RotateKey(c.Request.Context(), appID, keyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, dto.APIKeyCreatedFromService(created, h.now()))
}

// RevokeKey handles POST /admin/apps/{id}/keys/{key_id}/revoke.
func (h *AppHandler) RevokeKey(c *gin.Context) {
	appID, keyID, err := pathAppKeyIDs(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	app, key, err := h.apps.RevokeKey(c.Request.Context(), appID, keyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.APIKeyResponse{Key: dto.APIKeyFromDomain(app, key, h.now())})
}

func pathAppKeyIDs(c *gin.Context) (appID, keyID string, err error) {
	if appID, err = pathPublicID(c, "id", id.App); err != nil {
		return "", "", err
	}
	if keyID, err = pathPublicID(c, "key_id", id.APIKey); err != nil {
		return "", "", err
	}
	return appID, keyID, nil
}
