package domain

import (
	"encoding/json"
	"time"
)

// WebhookDeliveryStatus is the state of one event's delivery to one endpoint.
type WebhookDeliveryStatus string

const (
	WebhookDeliveryPending   WebhookDeliveryStatus = "PENDING"
	WebhookDeliveryDelivered WebhookDeliveryStatus = "DELIVERED"
	WebhookDeliveryFailed    WebhookDeliveryStatus = "FAILED"
)

// Valid reports whether s is a known delivery status.
func (s WebhookDeliveryStatus) Valid() bool {
	return s == WebhookDeliveryPending || s == WebhookDeliveryDelivered || s == WebhookDeliveryFailed
}

// WebhookAPIVersion is the body an endpoint receives: v2 is EventEnvelope,
// v1 the older flat body kept for endpoints migrated from apps.webhook_url.
type WebhookAPIVersion string

// Webhook body versions.
const (
	WebhookAPIV1 WebhookAPIVersion = "v1"
	WebhookAPIV2 WebhookAPIVersion = "v2"
)

// Valid reports whether v is a known body version.
func (v WebhookAPIVersion) Valid() bool { return v == WebhookAPIV1 || v == WebhookAPIV2 }

// WebhookEndpointStatus is whether an endpoint receives new deliveries.
type WebhookEndpointStatus string

// Endpoint statuses.
const (
	WebhookEndpointActive   WebhookEndpointStatus = "active"
	WebhookEndpointDisabled WebhookEndpointStatus = "disabled"
)

// WebhookDisabledReason says why an endpoint is disabled.
type WebhookDisabledReason string

// Disabled reasons.
const (
	WebhookDisabledManual              WebhookDisabledReason = "manual"
	WebhookDisabledConsecutiveFailures WebhookDisabledReason = "consecutive_failures"
)

// RetryBackoff is how the wait between attempts grows.
type RetryBackoff string

// Backoff strategies.
const (
	BackoffExponential RetryBackoff = "exponential"
	BackoffFixed       RetryBackoff = "fixed"
)

// Valid reports whether b is a known strategy.
func (b RetryBackoff) Valid() bool { return b == BackoffExponential || b == BackoffFixed }

// Retry policy bounds and limits of webhook endpoints.
const (
	MinRetryAttempts          = 1
	MaxRetryAttempts          = 20
	MinRetryIntervalSeconds   = 10
	MaxRetryIntervalSeconds   = 3600
	MaxRetryDelay             = 6 * time.Hour
	MaxWebhookEndpointsPerApp = 10
	MaxWebhookHeaders         = 10
)

// RetryPolicy is how often one delivery is attempted and how long KeiPay
// waits between attempts.
type RetryPolicy struct {
	MaxAttempts     int          `json:"max_attempts"`
	Backoff         RetryBackoff `json:"backoff"`
	IntervalSeconds int          `json:"interval_seconds"`
}

// DefaultRetryPolicy is 8 attempts, exponential from 30 seconds: about an
// hour in all.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 8, Backoff: BackoffExponential, IntervalSeconds: 30}
}

// Delay is the wait after the n-th attempt (n >= 1): interval × 2^(n-1)
// capped at MaxRetryDelay for exponential, interval for fixed.
func (p RetryPolicy) Delay(n int) time.Duration {
	d := time.Duration(p.IntervalSeconds) * time.Second
	if p.Backoff != BackoffExponential {
		return d
	}
	for i := 1; i < n && d < MaxRetryDelay; i++ {
		d *= 2
	}
	return min(d, MaxRetryDelay)
}

// WebhookHeader is one custom header sent with every request to an endpoint.
type WebhookHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// WebhookHeaderHint is what is shown of a stored custom header.
type WebhookHeaderHint struct {
	Name string `json:"name"`
	// ValueLast4 is nil when the value is too short to show any of it.
	ValueLast4 *string `json:"value_last4"`
}

// WebhookEndpoint is one URL of an app that receives events. Secrets and
// header values are stored encrypted; reads fill App.
type WebhookEndpoint struct {
	ID              int64
	PublicID        string
	AppID           int64
	App             AppSummary
	URL             string
	Description     string
	Events          []string
	APIVersion      WebhookAPIVersion
	LegacySignature bool
	SecretEnc       string `json:"-"`
	// SecretLast4 is nil for a secret carried over from apps.webhook_secret_enc.
	SecretLast4             *string
	PreviousSecretEnc       *string `json:"-"`
	PreviousSecretExpiresAt *time.Time
	// HeadersEnc is the encrypted JSON list of WebhookHeader; nil when none.
	HeadersEnc          *string `json:"-"`
	HeadersHint         []WebhookHeaderHint
	Status              WebhookEndpointStatus
	DisabledReason      *WebhookDisabledReason
	DisabledAt          *time.Time
	ConsecutiveFailures int
	RetryPolicy         RetryPolicy
	Migrated            bool
	DeletedAt           *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// Ref returns the short form deliveries carry.
func (e *WebhookEndpoint) Ref() EndpointRef {
	return EndpointRef{PublicID: e.PublicID, URL: e.URL, Description: e.Description, Deleted: e.DeletedAt != nil}
}

// EndpointRef identifies the endpoint a delivery went to.
type EndpointRef struct {
	PublicID    string
	URL         string
	Description string
	Deleted     bool
}

// WebhookDelivery is one event sent to one endpoint, with its retry state.
// KeiPay schedules the attempts; Payload is the body sent on every attempt.
type WebhookDelivery struct {
	ID       int64
	PublicID string
	// PaymentID is nil for test sends; PaymentPublicID is then "".
	PaymentID       *int64
	PaymentPublicID string
	AppID           int64
	App             AppSummary
	// EventID is nil for deliveries made before events existed.
	EventID *int64
	Event   *EventRef
	// EndpointID is nil for legacy deliveries of apps without a webhook URL.
	EndpointID         *int64
	Endpoint           *EndpointRef
	EventType          string
	Payload            json.RawMessage
	Status             WebhookDeliveryStatus
	Attempts           int
	MaxAttempts        int
	NextAttemptAt      *time.Time
	LastAttemptAt      *time.Time
	LastResponseStatus *int
	LastError          string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// IsTest reports whether d delivers a test event.
func (d *WebhookDelivery) IsTest() bool { return d.Event != nil && d.Event.IsTest }

// DeliveryAttempt is one HTTP request made for a delivery.
type DeliveryAttempt struct {
	ID         int64
	DeliveryID int64
	AttemptNo  int
	// Manual marks an attempt made by a replay.
	Manual bool
	URL    string
	// RequestHeaders are the headers sent, custom header values redacted.
	RequestHeaders map[string]string
	// ResponseStatus is nil when no response arrived.
	ResponseStatus        *int
	ResponseBody          string
	ResponseBodyTruncated bool
	Error                 string
	DurationMS            int
	StartedAt             time.Time
}

// DeliveryFilter narrows delivery lists; nil fields match everything. Ids are
// canonical public ids.
type DeliveryFilter struct {
	AppPublicID      *string
	EndpointPublicID *string
	EventPublicID    *string
	EventType        *EventType
	Status           *WebhookDeliveryStatus
	Environment      *Environment
	From             *time.Time
	To               *time.Time
}
