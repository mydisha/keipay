package worker

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	kphmac "github.com/mydisha/keipay/backend/internal/pkg/hmac"
	"github.com/mydisha/keipay/backend/internal/pkg/logger"
)

// Request limits of one delivery attempt.
const (
	sendTimeout = 15 * time.Second
	// MaxResponseBody is how much of a receiver's answer an attempt keeps.
	MaxResponseBody = 4096
	userAgent       = "KeiPay-Webhooks/1"
	redactedValue   = "[REDACTED]"
)

// Failure reasons of a delivery that ends without an attempt.
const (
	reasonNoEndpoint      = "no endpoint"
	reasonEndpointDeleted = "endpoint deleted"
	reasonEndpointOff     = "endpoint disabled"
	reasonDecryptFailed   = "secret decrypt failed"
)

// SecretDecryptor recovers endpoint signing secrets and custom headers.
type SecretDecryptor interface {
	Decrypt(ciphertext string) (string, error)
}

// Auditor records the automatic disabling of an endpoint. It must be called
// with the ctx of the transaction that disables it.
type Auditor interface {
	Record(ctx context.Context, action string, target domain.AuditTarget, before, after any) error
}

// DeliveryEnqueuer queues one attempt of a delivery.
type DeliveryEnqueuer interface {
	EnqueueDelivery(ctx context.Context, deliveryID int64, attempt int, manual bool, at time.Time) error
}

// Audit names of an automatic disable, kept equal to the service's.
const (
	actionAutoDisable     = "webhook_endpoint.auto_disable"
	targetWebhookEndpoint = "webhook_endpoint"
)

// ForwarderDeps are the collaborators of a Forwarder.
type ForwarderDeps struct {
	Deliveries domain.DeliveryRepository
	Endpoints  domain.WebhookEndpointRepository
	Tx         domain.TxManager
	Cipher     SecretDecryptor
	Audit      Auditor
	Enqueuer   DeliveryEnqueuer
	Log        *zap.Logger
	// DisableAfterFailures disables an endpoint after this many exhausted
	// deliveries in a row; 0 never does.
	DisableAfterFailures int
	// Client sends the requests; nil uses a client with a 15 second timeout
	// that never follows redirects.
	Client *http.Client
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Forwarder executes webhook:forward tasks: one signed POST of a delivery's
// stored body to its endpoint, logged as an attempt. KeiPay, not asynq,
// schedules the next attempt per the endpoint's retry policy.
type Forwarder struct {
	deliveries   domain.DeliveryRepository
	endpoints    domain.WebhookEndpointRepository
	tx           domain.TxManager
	cipher       SecretDecryptor
	audit        Auditor
	enqueuer     DeliveryEnqueuer
	log          *zap.Logger
	disableAfter int
	client       *http.Client
	now          func() time.Time
}

// NewForwarder constructs a Forwarder.
func NewForwarder(d ForwarderDeps) *Forwarder {
	f := &Forwarder{
		deliveries: d.Deliveries, endpoints: d.Endpoints, tx: d.Tx, cipher: d.Cipher, audit: d.Audit,
		enqueuer: d.Enqueuer, log: d.Log, disableAfter: d.DisableAfterFailures, client: d.Client, now: d.Now,
	}
	if f.log == nil {
		f.log = zap.NewNop()
	}
	if f.client == nil {
		f.client = NewHTTPClient()
	}
	if f.now == nil {
		f.now = time.Now
	}
	return f
}

// NewHTTPClient is the client attempts use: a 15 second timeout, and a
// redirect is an answer, never followed, so it counts as a failure.
func NewHTTPClient() *http.Client {
	return &http.Client{
		Timeout:       sendTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// HandleWebhookForward implements asynq.HandlerFunc for TypeWebhookForward.
// It returns an error only for infrastructure failures, which asynq retries.
func (f *Forwarder) HandleWebhookForward(ctx context.Context, task *asynq.Task) error {
	var p WebhookForwardPayload
	if err := json.Unmarshal(task.Payload(), &p); err != nil {
		return fmt.Errorf("unmarshal payload: %w: %v", asynq.SkipRetry, err)
	}
	d, ep, err := f.deliveries.LoadForSend(ctx, p.DeliveryID)
	if errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("delivery %d: %w: %v", p.DeliveryID, asynq.SkipRetry, err)
	}
	if err != nil {
		return err
	}
	attempt := d.Attempts + 1
	if d.Status != domain.WebhookDeliveryPending || (p.Attempt != 0 && p.Attempt != attempt) {
		return nil // already finished, or a stale task of an attempt that was made
	}
	log := f.log.With(
		zap.String(logger.FieldLogType, logger.LogTypeWebhook),
		zap.String("delivery_id", d.PublicID),
		zap.String(logger.FieldAppID, d.App.PublicID),
		zap.String("event_type", d.EventType),
		zap.Int("attempt", attempt),
	)
	if reason := blockedReason(d, ep); reason != "" {
		if _, err := f.deliveries.Fail(ctx, d.ID, reason); err != nil {
			return err
		}
		log.Info("webhook delivery ended without attempt",
			zap.String(logger.FieldEvent, "webhook.outbound.blocked"), zap.String("reason", reason))
		return nil
	}
	log = log.With(zap.String("endpoint_id", ep.PublicID), zap.String("target_url", ep.URL))
	keys, err := f.signingKeys(ep)
	if err != nil {
		if _, ferr := f.deliveries.Fail(ctx, d.ID, reasonDecryptFailed); ferr != nil {
			return ferr
		}
		log.Error("webhook endpoint secret decrypt failed",
			zap.String(logger.FieldEvent, "webhook.outbound.decrypt_failed"), zap.Error(err))
		return fmt.Errorf("decrypt endpoint %s: %w", ep.PublicID, asynq.SkipRetry)
	}

	a := f.send(ctx, d, ep, keys)
	a.DeliveryID, a.AttemptNo, a.Manual = d.ID, attempt, p.Manual
	next, err := f.record(ctx, d, ep, a)
	if err != nil {
		return err
	}
	fields := []zap.Field{zap.Int64(logger.FieldLatencyMS, int64(a.DurationMS))}
	if a.ResponseStatus != nil {
		fields = append(fields, zap.Int("status_code", *a.ResponseStatus))
	}
	if a.Error == "" {
		log.Info("webhook outbound delivered", append(fields, zap.String(logger.FieldEvent, "webhook.outbound.delivered"))...)
		return nil
	}
	log.Warn("webhook outbound attempt failed", append(fields,
		zap.String(logger.FieldEvent, "webhook.outbound.failed"), zap.String("error", a.Error))...)
	if next != nil {
		if err := f.enqueuer.EnqueueDelivery(ctx, d.ID, attempt+1, false, *next); err != nil {
			log.Warn("webhook retry enqueue deferred",
				zap.String(logger.FieldEvent, "webhook.outbound.enqueue_deferred"), zap.Error(err))
		}
	}
	return nil
}

// blockedReason is why d cannot be attempted, or "". A test may go to a
// disabled endpoint, so it can be checked before enabling it.
func blockedReason(d *domain.WebhookDelivery, ep *domain.WebhookEndpoint) string {
	switch {
	case ep == nil:
		return reasonNoEndpoint
	case ep.DeletedAt != nil:
		return reasonEndpointDeleted
	case ep.Status != domain.WebhookEndpointActive && !d.IsTest():
		return reasonEndpointOff
	}
	return ""
}

// signingKeys are the decrypted secrets and custom headers of an endpoint.
type signingKeys struct {
	current string
	// previous is "" once the overlap window of the last rotation ended.
	previous string
	headers  []domain.WebhookHeader
}

func (f *Forwarder) signingKeys(ep *domain.WebhookEndpoint) (signingKeys, error) {
	var k signingKeys
	var err error
	if k.current, err = f.cipher.Decrypt(ep.SecretEnc); err != nil {
		return k, fmt.Errorf("current secret: %w", err)
	}
	if ep.PreviousSecretEnc != nil && ep.PreviousSecretExpiresAt != nil && ep.PreviousSecretExpiresAt.After(f.now()) {
		if k.previous, err = f.cipher.Decrypt(*ep.PreviousSecretEnc); err != nil {
			return k, fmt.Errorf("previous secret: %w", err)
		}
	}
	if ep.HeadersEnc != nil {
		plain, err := f.cipher.Decrypt(*ep.HeadersEnc)
		if err != nil {
			return k, fmt.Errorf("headers: %w", err)
		}
		if err := json.Unmarshal([]byte(plain), &k.headers); err != nil {
			return k, fmt.Errorf("decode headers: %w", err)
		}
	}
	return k, nil
}

// Signature is the X-KeiPay-Signature value for body sent at t: one v1 per
// secret, newest first.
func Signature(t time.Time, body []byte, secrets ...string) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	var b strings.Builder
	b.WriteString("t=" + ts)
	for _, s := range secrets {
		mac := hmac.New(sha256.New, []byte(s))
		mac.Write([]byte(ts + "."))
		mac.Write(body)
		b.WriteString(",v1=" + hex.EncodeToString(mac.Sum(nil)))
	}
	return b.String()
}

// send makes one attempt and describes it; it never fails, a request that
// could not be made is a failed attempt.
func (f *Forwarder) send(ctx context.Context, d *domain.WebhookDelivery, ep *domain.WebhookEndpoint, k signingKeys) *domain.DeliveryAttempt {
	started := f.now().UTC().Truncate(time.Microsecond)
	a := &domain.DeliveryAttempt{URL: ep.URL, StartedAt: started, RequestHeaders: map[string]string{}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL, bytes.NewReader(d.Payload))
	if err != nil {
		a.Error = err.Error()
		return a
	}
	// Header names are set as written so receivers and the attempt log see
	// the documented spelling. Custom header values may be credentials, so the
	// log keeps only their names; signatures are not secrets.
	set := func(name, value string) {
		req.Header[name] = []string{value}
		a.RequestHeaders[name] = value
	}
	for _, h := range k.headers {
		req.Header[h.Name] = []string{h.Value}
		a.RequestHeaders[h.Name] = redactedValue
	}
	secrets := []string{k.current}
	if k.previous != "" {
		secrets = append(secrets, k.previous)
	}
	signature := Signature(started, d.Payload, secrets...)
	set("Content-Type", "application/json")
	set("User-Agent", userAgent)
	if d.Event != nil {
		set("X-KeiPay-Event-Id", d.Event.PublicID)
	}
	set("X-KeiPay-Event-Type", d.EventType)
	set("X-KeiPay-Delivery-Id", d.PublicID)
	set("X-KeiPay-Signature", signature)
	if d.IsTest() {
		set("X-KeiPay-Test", "true")
	}
	if ep.LegacySignature {
		set("X-Kei-Signature", kphmac.Sign(k.current, d.Payload))
		set("X-Kei-Event", d.EventType)
	}

	resp, err := f.client.Do(req)
	a.DurationMS = int(max(0, f.now().Sub(started).Milliseconds()))
	if err != nil {
		a.Error = err.Error()
		return a
	}
	defer resp.Body.Close()
	status := resp.StatusCode
	a.ResponseStatus = &status
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBody+1))
	a.ResponseBodyTruncated = len(body) > MaxResponseBody
	a.ResponseBody = sanitizeBody(body[:min(len(body), MaxResponseBody)])
	switch {
	case status < 200 || status > 299:
		a.Error = fmt.Sprintf("unexpected status %d", status)
	case readErr != nil:
		// The status already acknowledged the event; a broken body changes nothing.
		a.ResponseBodyTruncated = true
	}
	return a
}

// sanitizeBody returns b as valid UTF-8 text without NUL, at most
// MaxResponseBody bytes, as Postgres text can store it.
func sanitizeBody(b []byte) string {
	s := strings.ToValidUTF8(string(b), "�")
	s = strings.ReplaceAll(s, "\x00", "�")
	for len(s) > MaxResponseBody {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}

// record stores attempt a and moves d on: DELIVERED after a 2xx, PENDING
// with the next attempt scheduled while attempts remain, FAILED otherwise.
// It returns when the next attempt is due, nil when there is none or another
// worker recorded this attempt first.
func (f *Forwarder) record(ctx context.Context, d *domain.WebhookDelivery, ep *domain.WebhookEndpoint, a *domain.DeliveryAttempt) (*time.Time, error) {
	prev := d.Attempts
	next := *d
	next.Attempts, next.LastAttemptAt, next.LastResponseStatus = a.AttemptNo, &a.StartedAt, a.ResponseStatus
	delivered := a.Error == ""
	switch {
	case delivered:
		next.Status, next.LastError, next.NextAttemptAt = domain.WebhookDeliveryDelivered, "", nil
	case !a.Manual && next.Attempts < d.MaxAttempts:
		due := f.now().UTC().Truncate(time.Microsecond).Add(ep.RetryPolicy.Delay(next.Attempts))
		next.Status, next.LastError, next.NextAttemptAt = domain.WebhookDeliveryPending, lastError(a), &due
	default:
		next.Status, next.LastError, next.NextAttemptAt = domain.WebhookDeliveryFailed, lastError(a), nil
	}
	// Test sends and replays are attempts on request; they say nothing about
	// whether the endpoint keeps up.
	counts := !d.IsTest() && !a.Manual
	recorded := false
	err := f.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		recorded, err = f.deliveries.RecordAttempt(ctx, &next, prev, a)
		if err != nil || !recorded || !counts {
			return err
		}
		switch next.Status {
		case domain.WebhookDeliveryDelivered:
			return f.resetFailures(ctx, ep.ID)
		case domain.WebhookDeliveryFailed:
			return f.countFailure(ctx, ep.ID)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("record attempt %d of delivery %s: %w", a.AttemptNo, d.PublicID, err)
	}
	if !recorded || next.Status != domain.WebhookDeliveryPending {
		return nil, nil
	}
	return next.NextAttemptAt, nil
}

// lastError is the delivery's summary of a failed attempt.
func lastError(a *domain.DeliveryAttempt) string {
	if a.ResponseStatus != nil {
		return "status=" + strconv.Itoa(*a.ResponseStatus)
	}
	return a.Error
}

func (f *Forwarder) resetFailures(ctx context.Context, endpointID int64) error {
	ep, err := f.endpoints.GetByIDForUpdate(ctx, endpointID)
	if err != nil || ep.ConsecutiveFailures == 0 {
		return err
	}
	ep.ConsecutiveFailures = 0
	return f.endpoints.UpdateHealth(ctx, ep)
}

// healthSnapshot is the audited view of an automatic disable.
type healthSnapshot struct {
	Status              domain.WebhookEndpointStatus  `json:"status"`
	DisabledReason      *domain.WebhookDisabledReason `json:"disabled_reason"`
	ConsecutiveFailures int                           `json:"consecutive_failures"`
}

// countFailure adds an exhausted delivery to the endpoint's counter and
// disables an active endpoint that reached the threshold.
func (f *Forwarder) countFailure(ctx context.Context, endpointID int64) error {
	ep, err := f.endpoints.GetByIDForUpdate(ctx, endpointID)
	if err != nil {
		return err
	}
	before := healthSnapshot{Status: ep.Status, DisabledReason: ep.DisabledReason, ConsecutiveFailures: ep.ConsecutiveFailures}
	ep.ConsecutiveFailures++
	disable := f.disableAfter > 0 && ep.ConsecutiveFailures >= f.disableAfter &&
		ep.Status == domain.WebhookEndpointActive && ep.DeletedAt == nil
	if disable {
		reason, now := domain.WebhookDisabledConsecutiveFailures, f.now().UTC().Truncate(time.Microsecond)
		ep.Status, ep.DisabledReason, ep.DisabledAt = domain.WebhookEndpointDisabled, &reason, &now
	}
	if err := f.endpoints.UpdateHealth(ctx, ep); err != nil {
		return err
	}
	if !disable {
		return nil
	}
	f.log.Warn("webhook endpoint disabled after consecutive failures",
		zap.String(logger.FieldLogType, logger.LogTypeWebhook),
		zap.String(logger.FieldEvent, "webhook.endpoint.auto_disabled"),
		zap.String("endpoint_id", ep.PublicID),
		zap.Int("consecutive_failures", ep.ConsecutiveFailures),
	)
	ctx = domain.WithActor(ctx, domain.SystemActor("worker"))
	after := healthSnapshot{Status: ep.Status, DisabledReason: ep.DisabledReason, ConsecutiveFailures: ep.ConsecutiveFailures}
	return f.audit.Record(ctx, actionAutoDisable, domain.AuditTarget{Type: targetWebhookEndpoint, ID: ep.PublicID}, before, after)
}
