package dto

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/service"
)

// EventTypeInfo is one catalog entry.
type EventTypeInfo struct {
	Type        domain.EventType     `json:"type"`
	Description string               `json:"description"`
	Example     domain.EventEnvelope `json:"example"`
}

// EventWildcard is an accepted wildcard subscription.
type EventWildcard struct {
	Pattern     string `json:"pattern"`
	Description string `json:"description"`
}

// EventTypeListResponse is the response for GET /admin/event-types.
type EventTypeListResponse struct {
	Data      []EventTypeInfo `json:"data"`
	Wildcards []EventWildcard `json:"wildcards"`
}

// EventTypeListFromDomain converts the catalog, in its order.
func EventTypeListFromDomain(types []domain.EventTypeInfo, wildcards []domain.EventWildcard) EventTypeListResponse {
	out := EventTypeListResponse{Data: make([]EventTypeInfo, 0, len(types)), Wildcards: make([]EventWildcard, 0, len(wildcards))}
	for _, t := range types {
		out.Data = append(out.Data, EventTypeInfo{Type: t.Type, Description: t.Description, Example: t.Example})
	}
	for _, w := range wildcards {
		out.Wildcards = append(out.Wildcards, EventWildcard{Pattern: w.Pattern, Description: w.Description})
	}
	return out
}

// WebhookHeaderHint is a custom header's name and a hint of its value.
type WebhookHeaderHint struct {
	Name       string  `json:"name"`
	ValueLast4 *string `json:"value_last4"`
}

// WebhookEndpoint is an endpoint as the API shows it: never a secret or a
// header value.
type WebhookEndpoint struct {
	ID                      string                        `json:"id"`
	App                     AppSummary                    `json:"app"`
	URL                     string                        `json:"url"`
	Description             string                        `json:"description"`
	Events                  []string                      `json:"events"`
	APIVersion              domain.WebhookAPIVersion      `json:"api_version"`
	LegacySignature         bool                          `json:"legacy_signature"`
	Headers                 []WebhookHeaderHint           `json:"headers"`
	RetryPolicy             domain.RetryPolicy            `json:"retry_policy"`
	Status                  domain.WebhookEndpointStatus  `json:"status"`
	DisabledReason          *domain.WebhookDisabledReason `json:"disabled_reason"`
	DisabledAt              *time.Time                    `json:"disabled_at"`
	ConsecutiveFailures     int                           `json:"consecutive_failures"`
	SecretLast4             *string                       `json:"secret_last4"`
	PreviousSecretExpiresAt *time.Time                    `json:"previous_secret_expires_at"`
	Migrated                bool                          `json:"migrated"`
	CreatedAt               time.Time                     `json:"created_at"`
	UpdatedAt               time.Time                     `json:"updated_at"`
}

// WebhookEndpointFromDomain converts an endpoint; lists are [] when empty
// and timestamps UTC.
func WebhookEndpointFromDomain(e *domain.WebhookEndpoint) WebhookEndpoint {
	out := WebhookEndpoint{
		ID: e.PublicID, App: AppSummaryFromDomain(e.App), URL: e.URL, Description: e.Description,
		Events: append([]string{}, e.Events...), APIVersion: e.APIVersion, LegacySignature: e.LegacySignature,
		Headers: make([]WebhookHeaderHint, 0, len(e.HeadersHint)), RetryPolicy: e.RetryPolicy, Status: e.Status,
		DisabledReason: e.DisabledReason, DisabledAt: utcPtr(e.DisabledAt), ConsecutiveFailures: e.ConsecutiveFailures,
		SecretLast4: e.SecretLast4, PreviousSecretExpiresAt: utcPtr(e.PreviousSecretExpiresAt), Migrated: e.Migrated,
		CreatedAt: e.CreatedAt.UTC(), UpdatedAt: e.UpdatedAt.UTC(),
	}
	for _, h := range e.HeadersHint {
		out.Headers = append(out.Headers, WebhookHeaderHint{Name: h.Name, ValueLast4: h.ValueLast4})
	}
	return out
}

// WebhookEndpointResponse wraps one endpoint.
type WebhookEndpointResponse struct {
	Endpoint WebhookEndpoint `json:"endpoint"`
}

// WebhookEndpointSecretResponse carries an endpoint and its signing secret,
// shown once.
type WebhookEndpointSecretResponse struct {
	Endpoint WebhookEndpoint `json:"endpoint"`
	Secret   string          `json:"secret"`
}

// WebhookEndpointSecretFromService converts a created or rotated endpoint.
func WebhookEndpointSecretFromService(e *service.EndpointWithSecret) WebhookEndpointSecretResponse {
	return WebhookEndpointSecretResponse{Endpoint: WebhookEndpointFromDomain(e.Endpoint), Secret: e.Secret}
}

// WebhookEndpointListResponse is the response for GET /admin/apps/{id}/webhook-endpoints.
type WebhookEndpointListResponse struct {
	Data       []WebhookEndpoint `json:"data"`
	NextCursor *string           `json:"next_cursor"`
}

// WebhookEndpointListFromDomain converts a page.
func WebhookEndpointListFromDomain(page domain.PageResult[domain.WebhookEndpoint]) WebhookEndpointListResponse {
	out := WebhookEndpointListResponse{Data: make([]WebhookEndpoint, 0, len(page.Items)), NextCursor: nextCursor(page.NextCursor)}
	for i := range page.Items {
		out.Data = append(out.Data, WebhookEndpointFromDomain(&page.Items[i]))
	}
	return out
}

// EventRef identifies an event.
type EventRef struct {
	ID   string           `json:"id"`
	Type domain.EventType `json:"type"`
	Test bool             `json:"test"`
}

func eventRefFromDomain(r *domain.EventRef) *EventRef {
	if r == nil {
		return nil
	}
	return &EventRef{ID: r.PublicID, Type: r.Type, Test: r.IsTest}
}

// WebhookEndpointRef identifies the endpoint a delivery went to.
type WebhookEndpointRef struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Deleted     bool   `json:"deleted"`
}

// WebhookDelivery is a delivery as lists show it: never the body.
type WebhookDelivery struct {
	ID                 string                       `json:"id"`
	App                AppSummary                   `json:"app"`
	Endpoint           *WebhookEndpointRef          `json:"endpoint"`
	Event              *EventRef                    `json:"event"`
	EventType          string                       `json:"event_type"`
	PaymentID          *string                      `json:"payment_id"`
	Status             domain.WebhookDeliveryStatus `json:"status"`
	Attempts           int                          `json:"attempts"`
	MaxAttempts        int                          `json:"max_attempts"`
	NextAttemptAt      *time.Time                   `json:"next_attempt_at"`
	LastAttemptAt      *time.Time                   `json:"last_attempt_at"`
	LastResponseStatus *int                         `json:"last_response_status"`
	LastError          string                       `json:"last_error"`
	CreatedAt          time.Time                    `json:"created_at"`
	UpdatedAt          time.Time                    `json:"updated_at"`
}

// WebhookDeliveryFromDomain converts a delivery; timestamps in UTC.
func WebhookDeliveryFromDomain(d *domain.WebhookDelivery) WebhookDelivery {
	out := WebhookDelivery{
		ID: d.PublicID, App: AppSummaryFromDomain(d.App), Event: eventRefFromDomain(d.Event), EventType: d.EventType,
		Status: d.Status, Attempts: d.Attempts, MaxAttempts: d.MaxAttempts, NextAttemptAt: utcPtr(d.NextAttemptAt),
		LastAttemptAt: utcPtr(d.LastAttemptAt), LastResponseStatus: d.LastResponseStatus, LastError: d.LastError,
		CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}
	if d.Endpoint != nil {
		out.Endpoint = &WebhookEndpointRef{ID: d.Endpoint.PublicID, URL: d.Endpoint.URL, Description: d.Endpoint.Description,
			Deleted: d.Endpoint.Deleted}
	}
	if d.PaymentPublicID != "" {
		pay := d.PaymentPublicID
		out.PaymentID = &pay
	}
	return out
}

// WebhookDeliveryAttempt is one logged request of a delivery.
type WebhookDeliveryAttempt struct {
	Attempt               int               `json:"attempt"`
	Manual                bool              `json:"manual"`
	URL                   string            `json:"url"`
	RequestHeaders        map[string]string `json:"request_headers"`
	ResponseStatus        *int              `json:"response_status"`
	ResponseBody          string            `json:"response_body"`
	ResponseBodyTruncated bool              `json:"response_body_truncated"`
	Error                 string            `json:"error"`
	DurationMS            int               `json:"duration_ms"`
	StartedAt             time.Time         `json:"started_at"`
}

// WebhookDeliveryDetail is a delivery with its body and attempt log.
type WebhookDeliveryDetail struct {
	WebhookDelivery
	Payload    json.RawMessage          `json:"payload"`
	AttemptLog []WebhookDeliveryAttempt `json:"attempt_log"`
}

// WebhookDeliveryResponse wraps one delivery.
type WebhookDeliveryResponse struct {
	Delivery WebhookDelivery `json:"delivery"`
}

// WebhookDeliveryDetailResponse wraps one delivery with its body and attempts.
type WebhookDeliveryDetailResponse struct {
	Delivery WebhookDeliveryDetail `json:"delivery"`
}

// WebhookDeliveryDetailFromDomain converts a delivery and its attempts.
func WebhookDeliveryDetailFromDomain(d *domain.WebhookDelivery, attempts []domain.DeliveryAttempt) WebhookDeliveryDetailResponse {
	out := WebhookDeliveryDetail{
		WebhookDelivery: WebhookDeliveryFromDomain(d),
		Payload:         d.Payload,
		AttemptLog:      make([]WebhookDeliveryAttempt, 0, len(attempts)),
	}
	if len(out.Payload) == 0 {
		out.Payload = json.RawMessage(`{}`)
	}
	for _, a := range attempts {
		headers := a.RequestHeaders
		if headers == nil {
			headers = map[string]string{}
		}
		out.AttemptLog = append(out.AttemptLog, WebhookDeliveryAttempt{
			Attempt: a.AttemptNo, Manual: a.Manual, URL: a.URL, RequestHeaders: headers, ResponseStatus: a.ResponseStatus,
			ResponseBody: a.ResponseBody, ResponseBodyTruncated: a.ResponseBodyTruncated, Error: a.Error,
			DurationMS: a.DurationMS, StartedAt: a.StartedAt.UTC(),
		})
	}
	return WebhookDeliveryDetailResponse{Delivery: out}
}

// DeliveryListResponse is the response for GET /admin/webhook-deliveries.
type DeliveryListResponse struct {
	Data       []WebhookDelivery `json:"data"`
	NextCursor *string           `json:"next_cursor"`
}

// DeliveryListFromDomain converts a page.
func DeliveryListFromDomain(page domain.PageResult[domain.WebhookDelivery]) DeliveryListResponse {
	out := DeliveryListResponse{Data: make([]WebhookDelivery, 0, len(page.Items)), NextCursor: nextCursor(page.NextCursor)}
	for i := range page.Items {
		out.Data = append(out.Data, WebhookDeliveryFromDomain(&page.Items[i]))
	}
	return out
}

// InboundWebhook is a gateway callback as lists show it: never the body.
type InboundWebhook struct {
	ID             string               `json:"id"`
	Connection     ConnectionSummary    `json:"connection"`
	Provider       string               `json:"provider"`
	Source         string               `json:"source"`
	SignatureValid bool                 `json:"signature_valid"`
	Result         domain.InboundResult `json:"result"`
	ResultDetail   string               `json:"result_detail"`
	EventKey       *string              `json:"event_key"`
	ProviderStatus *string              `json:"provider_status"`
	Reference      *string              `json:"reference"`
	PaymentID      *string              `json:"payment_id"`
	Event          *EventRef            `json:"event"`
	ReceivedAt     time.Time            `json:"received_at"`
}

// InboundWebhookFromDomain converts a callback.
func InboundWebhookFromDomain(w *domain.InboundWebhook) InboundWebhook {
	out := InboundWebhook{
		ID: w.PublicID, Connection: *ConnectionSummaryFromDomain(&w.Connection), Provider: w.Provider, Source: w.Source,
		SignatureValid: w.SignatureValid, Result: w.Result, ResultDetail: w.ResultDetail, EventKey: w.EventKey,
		ProviderStatus: w.ProviderStatus, Reference: w.Reference, Event: eventRefFromDomain(w.Event),
		ReceivedAt: w.ReceivedAt.UTC(),
	}
	if w.PaymentPublicID != "" {
		pay := w.PaymentPublicID
		out.PaymentID = &pay
	}
	return out
}

// InboundWebhookDetail is a callback with its raw body as text.
type InboundWebhookDetail struct {
	InboundWebhook
	RawBody          string `json:"raw_body"`
	RawBodyTruncated bool   `json:"raw_body_truncated"`
}

// InboundWebhookDetailResponse wraps one callback with its body.
type InboundWebhookDetailResponse struct {
	InboundWebhook InboundWebhookDetail `json:"inbound_webhook"`
}

// InboundWebhookDetailFromDomain converts a callback; bytes that are not
// UTF-8 become U+FFFD.
func InboundWebhookDetailFromDomain(w *domain.InboundWebhook) InboundWebhookDetailResponse {
	return InboundWebhookDetailResponse{InboundWebhook: InboundWebhookDetail{
		InboundWebhook:   InboundWebhookFromDomain(w),
		RawBody:          strings.ToValidUTF8(string(w.RawBody), "�"),
		RawBodyTruncated: w.BodyTruncated,
	}}
}

// InboundWebhookListResponse is the response for GET /admin/inbound-webhooks.
type InboundWebhookListResponse struct {
	Data       []InboundWebhook `json:"data"`
	NextCursor *string          `json:"next_cursor"`
}

// InboundWebhookListFromDomain converts a page.
func InboundWebhookListFromDomain(page domain.PageResult[domain.InboundWebhook]) InboundWebhookListResponse {
	out := InboundWebhookListResponse{Data: make([]InboundWebhook, 0, len(page.Items)), NextCursor: nextCursor(page.NextCursor)}
	for i := range page.Items {
		out.Data = append(out.Data, InboundWebhookFromDomain(&page.Items[i]))
	}
	return out
}
