package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/dto"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// ConnectionHandler serves /admin/providers and /admin/connections. The
// router checks connections:read or connections:write first. Request bodies
// carry credentials, so they are never logged.
type ConnectionHandler struct {
	conns *service.ConnectionService
}

// NewConnectionHandler constructs a ConnectionHandler.
func NewConnectionHandler(conns *service.ConnectionService) *ConnectionHandler {
	return &ConnectionHandler{conns: conns}
}

// Providers handles GET /admin/providers.
func (h *ConnectionHandler) Providers(c *gin.Context) {
	response.Success(c, dto.ProviderListFromDomain(h.conns.Providers()))
}

// List handles GET /admin/connections (cursor-paginated, newest first).
func (h *ConnectionHandler) List(c *gin.Context) {
	env, err := queryEnum(c, "environment", domain.Environment.Valid)
	if err != nil {
		response.Error(c, err)
		return
	}
	status, err := queryEnum(c, "status", domain.ConnectionStatus.Valid)
	if err != nil {
		response.Error(c, err)
		return
	}
	prov, err := queryEnum(c, "provider", h.conns.KnownProvider)
	if err != nil {
		response.Error(c, err)
		return
	}
	req, err := pageRequest(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	page, err := h.conns.List(c.Request.Context(), domain.ConnectionFilter{Environment: env, Status: status, Provider: prov}, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.ConnectionListFromDomain(page))
}

type createConnectionRequest struct {
	Provider    string             `json:"provider"`
	Name        string             `json:"name"`
	Environment domain.Environment `json:"environment"`
	Credentials json.RawMessage    `json:"credentials"`
	Settings    json.RawMessage    `json:"settings"`
}

// Create handles POST /admin/connections.
func (h *ConnectionHandler) Create(c *gin.Context) {
	var req createConnectionRequest
	if err := decodeJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	creds, credsErr := jsonObject(req.Credentials)
	settings, settingsErr := jsonObject(req.Settings)
	if fields := objectErrors(map[string]error{"credentials": credsErr, "settings": settingsErr}); fields != nil {
		response.Error(c, invalidBody(fields...))
		return
	}
	d, err := h.conns.Create(c.Request.Context(), service.CreateConnectionInput{
		Provider: req.Provider, Name: req.Name, Environment: req.Environment, Credentials: creds, Settings: settings,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, dto.ConnectionResponse{Connection: dto.ConnectionFromService(d)})
}

// Get handles GET /admin/connections/{id}.
func (h *ConnectionHandler) Get(c *gin.Context) {
	connID, err := pathPublicID(c, "id", id.Connection)
	if err != nil {
		response.Error(c, err)
		return
	}
	d, err := h.conns.Get(c.Request.Context(), connID)
	respondConnection(c, d, err)
}

// Update handles PATCH /admin/connections/{id}. The body is read as raw
// fields so that sending provider, environment or credentials is detected
// whatever its value.
func (h *ConnectionHandler) Update(c *gin.Context) {
	connID, err := pathPublicID(c, "id", id.Connection)
	if err != nil {
		response.Error(c, err)
		return
	}
	var body map[string]json.RawMessage
	if err := decodeJSON(c, &body); err != nil {
		response.Error(c, err)
		return
	}
	in, err := updateConnectionInput(body)
	if err != nil {
		response.Error(c, err)
		return
	}
	d, err := h.conns.Update(c.Request.Context(), connID, in)
	respondConnection(c, d, err)
}

func updateConnectionInput(body map[string]json.RawMessage) (service.UpdateConnectionInput, error) {
	var in service.UpdateConnectionInput
	_, in.Provider = body["provider"]
	_, in.Environment = body["environment"]
	_, in.Credentials = body["credentials"]
	var fields []domain.FieldError
	if raw, ok := body["name"]; ok {
		if err := json.Unmarshal(raw, &in.Name); err != nil || in.Name == nil {
			fields = append(fields, domain.FieldError{Field: "name", Rule: "type"})
		}
	}
	if raw, ok := body["settings"]; ok {
		settings, err := jsonObject(raw)
		if err != nil || settings == nil {
			fields = append(fields, domain.FieldError{Field: "settings", Rule: "type"})
		}
		in.Settings = settings
	}
	if len(fields) > 0 {
		return in, invalidBody(fields...)
	}
	return in, nil
}

type updateCredentialsRequest struct {
	Credentials json.RawMessage `json:"credentials"`
}

// UpdateCredentials handles PATCH /admin/connections/{id}/credentials.
func (h *ConnectionHandler) UpdateCredentials(c *gin.Context) {
	connID, err := pathPublicID(c, "id", id.Connection)
	if err != nil {
		response.Error(c, err)
		return
	}
	var req updateCredentialsRequest
	if err := decodeJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	creds, err := jsonObject(req.Credentials)
	if err != nil {
		response.Error(c, invalidBody(domain.FieldError{Field: "credentials", Rule: "type"}))
		return
	}
	d, err := h.conns.UpdateCredentials(c.Request.Context(), connID, creds)
	respondConnection(c, d, err)
}

// ReplaceChannels handles PUT /admin/connections/{id}/channels.
func (h *ConnectionHandler) ReplaceChannels(c *gin.Context) {
	connID, err := pathPublicID(c, "id", id.Connection)
	if err != nil {
		response.Error(c, err)
		return
	}
	var req dto.ReplaceConnectionChannelsRequest
	if err := bindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	choices := make([]service.ChannelChoice, 0, len(req.Channels))
	for _, ch := range req.Channels {
		choices = append(choices, service.ChannelChoice{Channel: ch.Channel, Enabled: *ch.Enabled})
	}
	d, err := h.conns.ReplaceChannels(c.Request.Context(), connID, choices)
	respondConnection(c, d, err)
}

// Test handles POST /admin/connections/{id}/test.
func (h *ConnectionHandler) Test(c *gin.Context) {
	connID, err := pathPublicID(c, "id", id.Connection)
	if err != nil {
		response.Error(c, err)
		return
	}
	res, err := h.conns.Test(c.Request.Context(), connID)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.ConnectionTestFromService(res))
}

// Disable handles POST /admin/connections/{id}/disable.
func (h *ConnectionHandler) Disable(c *gin.Context) {
	h.setStatus(c, h.conns.Disable)
}

// Enable handles POST /admin/connections/{id}/enable.
func (h *ConnectionHandler) Enable(c *gin.Context) {
	h.setStatus(c, h.conns.Enable)
}

func (h *ConnectionHandler) setStatus(c *gin.Context, fn func(ctx context.Context, publicID string) (*service.ConnectionDetail, error)) {
	connID, err := pathPublicID(c, "id", id.Connection)
	if err != nil {
		response.Error(c, err)
		return
	}
	d, err := fn(c.Request.Context(), connID)
	respondConnection(c, d, err)
}

func respondConnection(c *gin.Context, d *service.ConnectionDetail, err error) {
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.ConnectionResponse{Connection: dto.ConnectionFromService(d)})
}

// jsonObject decodes an optional JSON object keeping numbers as json.Number,
// so integer fields can be checked exactly. Absent or null is nil; any other
// JSON type is an error.
func jsonObject(raw json.RawMessage) (map[string]any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] != '{' {
		return nil, errNotObject
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

var errNotObject = errors.New("not a JSON object")

// objectErrors returns a type error per failed field, in a fixed order.
func objectErrors(errs map[string]error) []domain.FieldError {
	var fields []domain.FieldError
	for _, name := range []string{"credentials", "settings"} {
		if errs[name] != nil {
			fields = append(fields, domain.FieldError{Field: name, Rule: "type"})
		}
	}
	return fields
}
