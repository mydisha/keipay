package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/pkg/keygen"
	"github.com/mydisha/keipay/backend/internal/pkg/logger"
)

// Field rules for webhook endpoints, reported in details.fields.
const (
	RuleHTTPS       = "https"
	RuleHeaderName  = "header_name"
	RuleHeaderValue = "header_value"
	RuleReserved    = "reserved"
)

// Input limits for webhook endpoints.
const (
	maxEndpointDescriptionRunes = 500
	maxHeaderNameLen            = 64
	maxHeaderValueRunes         = 1024
	defaultOverlapHours         = 24
	maxOverlapHours             = 168
)

var errTooManyEndpoints = &domain.Error{Code: domain.CodeValidationFailed,
	Message: fmt.Sprintf("an app can have at most %d webhook endpoints", domain.MaxWebhookEndpointsPerApp), Err: domain.ErrInvalidInput}

// reservedHeaders are set by KeiPay on every request; custom headers may not
// replace them.
var reservedHeaders = []string{"content-type", "content-length", "host", "user-agent", "connection", "transfer-encoding"}

var reservedHeaderPrefixes = []string{"x-keipay-", "x-kei-"}

// WebhookEndpointDeps are the collaborators of a WebhookEndpointService.
type WebhookEndpointDeps struct {
	Apps       domain.AppRepository
	Endpoints  domain.WebhookEndpointRepository
	Events     domain.EventRepository
	Deliveries domain.DeliveryRepository
	Cipher     SecretCipher
	Tx         domain.TxManager
	Audit      Auditor
	Enqueuer   Enqueuer
	Log        *zap.Logger
}

// WebhookEndpointService manages the webhook endpoints of apps. Every
// mutation and its audit entry commit in one transaction; signing secrets
// leave it in plain text only when created or rotated.
type WebhookEndpointService struct {
	apps       domain.AppRepository
	endpoints  domain.WebhookEndpointRepository
	events     domain.EventRepository
	deliveries domain.DeliveryRepository
	cipher     SecretCipher
	tx         domain.TxManager
	audit      Auditor
	enqueuer   Enqueuer
	log        *zap.Logger
	now        func() time.Time
}

// NewWebhookEndpointService constructs a WebhookEndpointService.
func NewWebhookEndpointService(d WebhookEndpointDeps) *WebhookEndpointService {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &WebhookEndpointService{
		apps: d.Apps, endpoints: d.Endpoints, events: d.Events, deliveries: d.Deliveries, cipher: d.Cipher,
		tx: d.Tx, audit: d.Audit, enqueuer: d.Enqueuer, log: log, now: time.Now,
	}
}

// WebhookHeaderInput is one custom header of a create or update request.
type WebhookHeaderInput struct {
	Name  string
	Value string
}

// RetryPolicyInput is a retry_policy object; nil keys take their default.
type RetryPolicyInput struct {
	MaxAttempts     *int
	Backoff         *string
	IntervalSeconds *int
}

// CreateWebhookEndpointInput is a new endpoint. A nil APIVersion is v2 and a
// nil RetryPolicy the default policy.
type CreateWebhookEndpointInput struct {
	URL             string
	Description     string
	Events          []string
	APIVersion      *string
	LegacySignature bool
	Headers         []WebhookHeaderInput
	RetryPolicy     *RetryPolicyInput
}

// UpdateWebhookEndpointInput holds the fields present in a PATCH body; nil
// means absent. Status, Secret and AppID only record that the field was sent,
// which is refused.
type UpdateWebhookEndpointInput struct {
	URL             *string
	Description     *string
	Events          *[]string
	APIVersion      *string
	LegacySignature *bool
	Headers         *[]WebhookHeaderInput
	RetryPolicy     *RetryPolicyInput
	Status          bool
	Secret          bool
	AppID           bool
}

func (in UpdateWebhookEndpointInput) empty() bool {
	return in.URL == nil && in.Description == nil && in.Events == nil && in.APIVersion == nil &&
		in.LegacySignature == nil && in.Headers == nil && in.RetryPolicy == nil
}

// EndpointWithSecret is an endpoint with its signing secret, shown once.
type EndpointWithSecret struct {
	Endpoint *domain.WebhookEndpoint
	Secret   string
}

// endpointSnapshot is the audited view of an endpoint: never a secret, a
// secret hint or a header value.
type endpointSnapshot struct {
	ID              string                        `json:"id"`
	AppID           string                        `json:"app_id"`
	URL             string                        `json:"url"`
	Description     string                        `json:"description"`
	Events          []string                      `json:"events"`
	APIVersion      domain.WebhookAPIVersion      `json:"api_version"`
	LegacySignature bool                          `json:"legacy_signature"`
	HeaderNames     []string                      `json:"header_names"`
	Status          domain.WebhookEndpointStatus  `json:"status"`
	DisabledReason  *domain.WebhookDisabledReason `json:"disabled_reason"`
	RetryPolicy     domain.RetryPolicy            `json:"retry_policy"`
}

func snapshotEndpoint(e *domain.WebhookEndpoint) endpointSnapshot {
	return endpointSnapshot{
		ID: e.PublicID, AppID: e.App.PublicID, URL: e.URL, Description: e.Description, Events: e.Events,
		APIVersion: e.APIVersion, LegacySignature: e.LegacySignature, HeaderNames: headerNames(e.HeadersHint),
		Status: e.Status, DisabledReason: e.DisabledReason, RetryPolicy: e.RetryPolicy,
	}
}

// endpointStatusSnapshot is the audited view of an enable or disable.
type endpointStatusSnapshot struct {
	Status              domain.WebhookEndpointStatus  `json:"status"`
	DisabledReason      *domain.WebhookDisabledReason `json:"disabled_reason"`
	ConsecutiveFailures int                           `json:"consecutive_failures"`
}

func snapshotEndpointStatus(e *domain.WebhookEndpoint) endpointStatusSnapshot {
	return endpointStatusSnapshot{Status: e.Status, DisabledReason: e.DisabledReason, ConsecutiveFailures: e.ConsecutiveFailures}
}

// rotateSnapshot is the audited view of a secret rotation.
type rotateSnapshot struct {
	SecretLast4             *string    `json:"secret_last4"`
	PreviousSecretExpiresAt *time.Time `json:"previous_secret_expires_at"`
}

func endpointTarget(e *domain.WebhookEndpoint) domain.AuditTarget {
	return domain.AuditTarget{Type: TargetWebhookEndpoint, ID: e.PublicID}
}

func headerNames(h []domain.WebhookHeaderHint) []string {
	out := make([]string, 0, len(h))
	for _, v := range h {
		out = append(out, v.Name)
	}
	return out
}

func (s *WebhookEndpointService) timestamp() time.Time {
	return s.now().UTC().Truncate(time.Microsecond)
}

// EventTypes returns the event catalog and the accepted wildcards.
func (s *WebhookEndpointService) EventTypes() ([]domain.EventTypeInfo, []domain.EventWildcard) {
	return domain.EventCatalog(), domain.EventWildcards()
}

// List returns one page of the app's endpoints, newest first.
func (s *WebhookEndpointService) List(ctx context.Context, appPublicID string, req domain.PageRequest) (domain.PageResult[domain.WebhookEndpoint], error) {
	p, err := req.Page()
	if err != nil {
		return domain.PageResult[domain.WebhookEndpoint]{}, err
	}
	app, err := s.apps.GetByPublicID(ctx, appPublicID)
	if err != nil {
		return domain.PageResult[domain.WebhookEndpoint]{}, fmt.Errorf("get app: %w", err)
	}
	res, err := s.endpoints.List(ctx, app.ID, p)
	if err != nil {
		return domain.PageResult[domain.WebhookEndpoint]{}, fmt.Errorf("list webhook endpoints: %w", err)
	}
	return res, nil
}

// Get returns an endpoint that is not deleted.
func (s *WebhookEndpointService) Get(ctx context.Context, publicID string) (*domain.WebhookEndpoint, error) {
	e, err := s.endpoints.GetByPublicID(ctx, publicID)
	if err != nil {
		return nil, fmt.Errorf("get webhook endpoint: %w", err)
	}
	return e, nil
}

// endpointSpec is a validated create request.
type endpointSpec struct {
	url, description string
	events           []string
	apiVersion       domain.WebhookAPIVersion
	headers          []domain.WebhookHeader
	retry            domain.RetryPolicy
}

// Create validates and stores an active endpoint of the app with a new
// signing secret. All field errors come back in one invalid_request.
func (s *WebhookEndpointService) Create(ctx context.Context, appPublicID string, in CreateWebhookEndpointInput) (*EndpointWithSecret, error) {
	app, err := s.apps.GetByPublicID(ctx, appPublicID)
	if err != nil {
		return nil, fmt.Errorf("get app: %w", err)
	}
	spec, err := validateCreate(app.Environment, in)
	if err != nil {
		return nil, err
	}
	secret := keygen.WebhookSecret()
	e := &domain.WebhookEndpoint{
		PublicID: id.New(id.WebhookEndpoint), URL: spec.url, Description: spec.description, Events: spec.events,
		APIVersion: spec.apiVersion, LegacySignature: in.LegacySignature, Status: domain.WebhookEndpointActive,
		RetryPolicy: spec.retry,
	}
	if err := s.setSecret(e, secret); err != nil {
		return nil, err
	}
	if err := s.setHeaders(e, spec.headers); err != nil {
		return nil, err
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		// The app lock serializes creates per app, so the limit holds.
		app, err := s.apps.GetByPublicIDForUpdate(ctx, appPublicID)
		if err != nil {
			return err
		}
		n, err := s.endpoints.CountByApp(ctx, app.ID)
		if err != nil {
			return err
		}
		if n >= domain.MaxWebhookEndpointsPerApp {
			return errTooManyEndpoints
		}
		e.AppID, e.App = app.ID, app.Summary()
		if err := s.endpoints.Create(ctx, e); err != nil {
			return err
		}
		return s.audit.Record(ctx, ActionWebhookEndpointCreate, endpointTarget(e), nil, snapshotEndpoint(e))
	})
	if err != nil {
		return nil, fmt.Errorf("create webhook endpoint: %w", err)
	}
	return &EndpointWithSecret{Endpoint: e, Secret: secret}, nil
}

func validateCreate(env domain.Environment, in CreateWebhookEndpointInput) (endpointSpec, error) {
	var fe fieldErrors
	var spec endpointSpec
	var rule string
	spec.url, rule = endpointURL(in.URL, env)
	fe.check("url", rule)
	spec.description, rule = endpointDescription(in.Description)
	fe.check("description", rule)
	var errs []domain.FieldError
	spec.events, errs = domain.ValidateSubscriptions(in.Events)
	fe = append(fe, errs...)
	spec.apiVersion = domain.WebhookAPIV2
	if in.APIVersion != nil {
		spec.apiVersion = domain.WebhookAPIVersion(*in.APIVersion)
		if !spec.apiVersion.Valid() {
			fe.check("api_version", RuleOneOf)
		}
	}
	spec.headers, errs = validateHeaders(in.Headers)
	fe = append(fe, errs...)
	spec.retry, errs = validateRetryPolicy(in.RetryPolicy)
	fe = append(fe, errs...)
	return spec, fe.err()
}

// endpointURL trims an endpoint URL and returns it with the failed rule, or
// "". Live apps only send to https.
func endpointURL(s string, env domain.Environment) (string, string) {
	u, rule := normalizeURL(s)
	switch {
	case u == "":
		return "", RuleRequired
	case rule != "":
		return u, rule
	case env == domain.EnvLive && strings.HasPrefix(strings.ToLower(u), "http:"):
		return u, RuleHTTPS
	}
	return u, ""
}

func endpointDescription(s string) (string, string) {
	d := strings.TrimSpace(s)
	if utf8.RuneCountInString(d) > maxEndpointDescriptionRunes {
		return d, RuleMax
	}
	for _, r := range d {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return d, RuleName
		}
	}
	return d, ""
}

// validateHeaders checks custom headers; names keep the case they were sent in.
func validateHeaders(in []WebhookHeaderInput) ([]domain.WebhookHeader, []domain.FieldError) {
	if len(in) > domain.MaxWebhookHeaders {
		return nil, []domain.FieldError{{Field: "headers", Rule: RuleMax}}
	}
	var fe fieldErrors
	out := make([]domain.WebhookHeader, 0, len(in))
	seen := map[string]bool{}
	for i, h := range in {
		prefix := "headers." + strconv.Itoa(i) + "."
		lower := strings.ToLower(h.Name)
		switch {
		case !headerNameValid(h.Name):
			fe.check(prefix+"name", RuleHeaderName)
		case reservedHeader(lower):
			fe.check(prefix+"name", RuleReserved)
		case seen[lower]:
			fe.check(prefix+"name", RuleDuplicate)
		}
		seen[lower] = true
		if !headerValueValid(h.Value) {
			fe.check(prefix+"value", RuleHeaderValue)
		}
		out = append(out, domain.WebhookHeader{Name: h.Name, Value: h.Value})
	}
	if len(fe) > 0 {
		return nil, fe
	}
	return out, nil
}

// headerNameValid reports whether s is an RFC 9110 token of 1-64 characters.
func headerNameValid(s string) bool {
	if s == "" || len(s) > maxHeaderNameLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0 {
			continue
		}
		return false
	}
	return true
}

func reservedHeader(lower string) bool {
	if slices.Contains(reservedHeaders, lower) {
		return true
	}
	for _, p := range reservedHeaderPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

func headerValueValid(s string) bool {
	if s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > maxHeaderValueRunes {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// validateRetryPolicy fills keys left out with the default policy.
func validateRetryPolicy(in *RetryPolicyInput) (domain.RetryPolicy, []domain.FieldError) {
	p := domain.DefaultRetryPolicy()
	if in == nil {
		return p, nil
	}
	var fe fieldErrors
	if in.MaxAttempts != nil {
		p.MaxAttempts = *in.MaxAttempts
		fe.check("retry_policy.max_attempts", rangeRule(p.MaxAttempts, domain.MinRetryAttempts, domain.MaxRetryAttempts))
	}
	if in.Backoff != nil {
		p.Backoff = domain.RetryBackoff(*in.Backoff)
		if !p.Backoff.Valid() {
			fe.check("retry_policy.backoff", RuleOneOf)
		}
	}
	if in.IntervalSeconds != nil {
		p.IntervalSeconds = *in.IntervalSeconds
		fe.check("retry_policy.interval_seconds", rangeRule(p.IntervalSeconds, domain.MinRetryIntervalSeconds, domain.MaxRetryIntervalSeconds))
	}
	if len(fe) > 0 {
		return p, fe
	}
	return p, nil
}

func rangeRule(v, lo, hi int) string {
	switch {
	case v < lo:
		return RuleMin
	case v > hi:
		return RuleMax
	}
	return ""
}

// setSecret makes secret the current signing secret of e.
func (s *WebhookEndpointService) setSecret(e *domain.WebhookEndpoint, secret string) error {
	enc, err := s.cipher.Encrypt(secret)
	if err != nil {
		return fmt.Errorf("encrypt webhook secret: %w", err)
	}
	last4 := secret[len(secret)-4:]
	e.SecretEnc, e.SecretLast4 = enc, &last4
	return nil
}

// setHeaders stores hs on e encrypted, with a hint per header.
func (s *WebhookEndpointService) setHeaders(e *domain.WebhookEndpoint, hs []domain.WebhookHeader) error {
	e.HeadersEnc, e.HeadersHint = nil, []domain.WebhookHeaderHint{}
	if len(hs) == 0 {
		return nil
	}
	plain, err := json.Marshal(hs)
	if err != nil {
		return fmt.Errorf("encode webhook headers: %w", err)
	}
	enc, err := s.cipher.Encrypt(string(plain))
	if err != nil {
		return fmt.Errorf("encrypt webhook headers: %w", err)
	}
	e.HeadersEnc = &enc
	for _, h := range hs {
		e.HeadersHint = append(e.HeadersHint, domain.WebhookHeaderHint{Name: h.Name, ValueLast4: last4(h.Value)})
	}
	return nil
}

// decryptHeaders recovers the custom headers of e; none when it has none.
func decryptHeaders(c SecretCipher, e *domain.WebhookEndpoint) ([]domain.WebhookHeader, error) {
	if e.HeadersEnc == nil {
		return nil, nil
	}
	plain, err := c.Decrypt(*e.HeadersEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt webhook headers: %w", err)
	}
	var hs []domain.WebhookHeader
	if err := json.Unmarshal([]byte(plain), &hs); err != nil {
		return nil, fmt.Errorf("decode webhook headers: %w", err)
	}
	return hs, nil
}

// Update changes the fields present in in. A request that changes nothing
// writes and records nothing.
func (s *WebhookEndpointService) Update(ctx context.Context, publicID string, in UpdateWebhookEndpointInput) (*domain.WebhookEndpoint, error) {
	var fe fieldErrors
	if in.Status {
		fe.check("status", RuleImmutable)
	}
	if in.Secret {
		fe.check("secret", RuleImmutable)
	}
	if in.AppID {
		fe.check("app_id", RuleImmutable)
	}
	if len(fe) == 0 && in.empty() {
		return nil, domain.InvalidFields(domain.FieldError{Field: "body", Rule: RuleRequired})
	}
	var e *domain.WebhookEndpoint
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		e, err = s.endpoints.GetByPublicIDForUpdate(ctx, publicID)
		if err != nil {
			return err
		}
		before, after, err := s.applyUpdate(e, in, fe)
		if err != nil {
			return err
		}
		if len(after) == 0 {
			return nil
		}
		if err := s.endpoints.Update(ctx, e); err != nil {
			return err
		}
		return s.audit.Record(ctx, ActionWebhookEndpointUpdate, endpointTarget(e), before, after)
	})
	if err != nil {
		return nil, fmt.Errorf("update webhook endpoint: %w", err)
	}
	return e, nil
}

// applyUpdate validates in against e (fe holds the errors found so far), sets
// every changed field, and returns the old and new values of only those.
func (s *WebhookEndpointService) applyUpdate(e *domain.WebhookEndpoint, in UpdateWebhookEndpointInput, fe fieldErrors) (before, after map[string]any, err error) {
	var url, description string
	var events []string
	var headers []domain.WebhookHeader
	var retry domain.RetryPolicy
	var rule string
	var errs []domain.FieldError
	if in.URL != nil {
		url, rule = endpointURL(*in.URL, e.App.Environment)
		fe.check("url", rule)
	}
	if in.Description != nil {
		description, rule = endpointDescription(*in.Description)
		fe.check("description", rule)
	}
	if in.Events != nil {
		events, errs = domain.ValidateSubscriptions(*in.Events)
		fe = append(fe, errs...)
	}
	if in.APIVersion != nil && !domain.WebhookAPIVersion(*in.APIVersion).Valid() {
		fe.check("api_version", RuleOneOf)
	}
	if in.Headers != nil {
		headers, errs = validateHeaders(*in.Headers)
		fe = append(fe, errs...)
	}
	if in.RetryPolicy != nil {
		retry, errs = validateRetryPolicy(in.RetryPolicy)
		fe = append(fe, errs...)
	}
	if err := fe.err(); err != nil {
		return nil, nil, err
	}

	before, after = map[string]any{}, map[string]any{}
	change := func(field string, old, next any) {
		before[field], after[field] = old, next
	}
	if in.URL != nil && url != e.URL {
		change("url", e.URL, url)
		e.URL = url
	}
	if in.Description != nil && description != e.Description {
		change("description", e.Description, description)
		e.Description = description
	}
	if in.Events != nil && !slices.Equal(events, e.Events) {
		change("events", e.Events, events)
		e.Events = events
	}
	if v := domain.WebhookAPIVersion(ptrValue(in.APIVersion)); in.APIVersion != nil && v != e.APIVersion {
		change("api_version", e.APIVersion, v)
		e.APIVersion = v
	}
	if in.LegacySignature != nil && *in.LegacySignature != e.LegacySignature {
		change("legacy_signature", e.LegacySignature, *in.LegacySignature)
		e.LegacySignature = *in.LegacySignature
	}
	if in.Headers != nil {
		current, err := decryptHeaders(s.cipher, e)
		if err != nil {
			return nil, nil, err
		}
		if !slices.Equal(current, headers) {
			old := headerNames(e.HeadersHint)
			if err := s.setHeaders(e, headers); err != nil {
				return nil, nil, err
			}
			change("header_names", old, headerNames(e.HeadersHint))
		}
	}
	if in.RetryPolicy != nil && retry != e.RetryPolicy {
		change("retry_policy", e.RetryPolicy, retry)
		e.RetryPolicy = retry
	}
	return before, after, nil
}

func ptrValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Delete soft-deletes an endpoint: it stops receiving events and leaves
// every list, while its deliveries stay readable.
func (s *WebhookEndpointService) Delete(ctx context.Context, publicID string) error {
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		e, err := s.endpoints.GetByPublicIDForUpdate(ctx, publicID)
		if err != nil {
			return err
		}
		before := snapshotEndpoint(e)
		now := s.timestamp()
		e.DeletedAt = &now
		if err := s.endpoints.Update(ctx, e); err != nil {
			return err
		}
		return s.audit.Record(ctx, ActionWebhookEndpointDelete, endpointTarget(e), before, nil)
	})
	if err != nil {
		return fmt.Errorf("delete webhook endpoint: %w", err)
	}
	return nil
}

// RotateSecret issues a new signing secret. The current one keeps signing
// next to it for overlapHours (nil is the default); any older one is dropped.
func (s *WebhookEndpointService) RotateSecret(ctx context.Context, publicID string, overlapHours *int) (*EndpointWithSecret, error) {
	hours := defaultOverlapHours
	if overlapHours != nil {
		hours = *overlapHours
		if rule := rangeRule(hours, 0, maxOverlapHours); rule != "" {
			return nil, domain.InvalidFields(domain.FieldError{Field: "overlap_hours", Rule: rule})
		}
	}
	secret := keygen.WebhookSecret()
	var e *domain.WebhookEndpoint
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		e, err = s.endpoints.GetByPublicIDForUpdate(ctx, publicID)
		if err != nil {
			return err
		}
		before := rotateSnapshot{SecretLast4: e.SecretLast4, PreviousSecretExpiresAt: e.PreviousSecretExpiresAt}
		e.PreviousSecretEnc, e.PreviousSecretExpiresAt = nil, nil
		if hours > 0 {
			prev, expires := e.SecretEnc, s.timestamp().Add(time.Duration(hours)*time.Hour)
			e.PreviousSecretEnc, e.PreviousSecretExpiresAt = &prev, &expires
		}
		if err := s.setSecret(e, secret); err != nil {
			return err
		}
		if err := s.endpoints.Update(ctx, e); err != nil {
			return err
		}
		after := rotateSnapshot{SecretLast4: e.SecretLast4, PreviousSecretExpiresAt: e.PreviousSecretExpiresAt}
		return s.audit.Record(ctx, ActionWebhookEndpointRotate, endpointTarget(e), before, after)
	})
	if err != nil {
		return nil, fmt.Errorf("rotate webhook secret: %w", err)
	}
	return &EndpointWithSecret{Endpoint: e, Secret: secret}, nil
}

// Disable stops new deliveries to an endpoint. Disabling a disabled endpoint
// changes and records nothing.
func (s *WebhookEndpointService) Disable(ctx context.Context, publicID string) (*domain.WebhookEndpoint, error) {
	return s.setStatus(ctx, publicID, domain.WebhookEndpointDisabled, ActionWebhookEndpointDisable)
}

// Enable re-activates an endpoint and resets its failure counter. Enabling an
// active endpoint changes and records nothing.
func (s *WebhookEndpointService) Enable(ctx context.Context, publicID string) (*domain.WebhookEndpoint, error) {
	return s.setStatus(ctx, publicID, domain.WebhookEndpointActive, ActionWebhookEndpointEnable)
}

func (s *WebhookEndpointService) setStatus(ctx context.Context, publicID string, status domain.WebhookEndpointStatus, action string) (*domain.WebhookEndpoint, error) {
	var e *domain.WebhookEndpoint
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		e, err = s.endpoints.GetByPublicIDForUpdate(ctx, publicID)
		if err != nil {
			return err
		}
		if e.Status == status {
			return nil
		}
		before := snapshotEndpointStatus(e)
		e.Status = status
		if status == domain.WebhookEndpointDisabled {
			reason, now := domain.WebhookDisabledManual, s.timestamp()
			e.DisabledReason, e.DisabledAt = &reason, &now
		} else {
			e.DisabledReason, e.DisabledAt, e.ConsecutiveFailures = nil, nil, 0
		}
		if err := s.endpoints.Update(ctx, e); err != nil {
			return err
		}
		return s.audit.Record(ctx, action, endpointTarget(e), before, snapshotEndpointStatus(e))
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	return e, nil
}

// testSnapshot is the audited view of a test send.
type testSnapshot struct {
	EventType  domain.EventType `json:"event_type"`
	DeliveryID string           `json:"delivery_id"`
}

// SendTest creates a test event of eventType whose data is the catalog
// example and queues one delivery of it to this endpoint only, whatever its
// subscriptions and status. The delivery is attempted once.
func (s *WebhookEndpointService) SendTest(ctx context.Context, publicID, eventType string) (*domain.WebhookDelivery, error) {
	t := domain.EventType(strings.TrimSpace(eventType))
	switch {
	case t == "":
		return nil, domain.InvalidFields(domain.FieldError{Field: "event_type", Rule: RuleRequired})
	case !t.Known():
		return nil, domain.InvalidFields(domain.FieldError{Field: "event_type", Rule: RuleOneOf})
	}
	data, err := marshalData(domain.ExampleEventData(t))
	if err != nil {
		return nil, err
	}
	var d *domain.WebhookDelivery
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		e, err := s.endpoints.GetByPublicID(ctx, publicID)
		if err != nil {
			return err
		}
		event := &domain.Event{
			PublicID: id.New(id.Event), Type: t, APIVersion: domain.EventAPIVersion, AppID: e.AppID,
			Data: data, IsTest: true, CreatedAt: s.timestamp(),
		}
		if err := s.events.Create(ctx, event); err != nil {
			return err
		}
		d, err = newDelivery(event, e, 1, event.CreatedAt)
		if err != nil {
			return err
		}
		if err := s.deliveries.Create(ctx, d); err != nil {
			return err
		}
		return s.audit.Record(ctx, ActionWebhookEndpointTest, endpointTarget(e), nil, testSnapshot{EventType: t, DeliveryID: d.PublicID})
	})
	if err != nil {
		return nil, fmt.Errorf("send test event: %w", err)
	}
	if err := s.enqueuer.EnqueueDelivery(ctx, d.ID, 1, false, *d.NextAttemptAt); err != nil {
		// The row is committed; the sweeper sends it.
		logger.Ctx(ctx, s.log).Warn("test delivery enqueue deferred",
			zap.String(logger.FieldLogType, logger.LogTypeWebhook),
			zap.String(logger.FieldEvent, "webhook.outbound.enqueue_deferred"),
			zap.String("delivery_id", d.PublicID),
			zap.Error(err),
		)
	}
	return d, nil
}
