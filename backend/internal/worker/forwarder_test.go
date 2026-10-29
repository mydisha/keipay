package worker

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/hibiken/asynq"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/hmac"
)

// Every request carries the KeiPay headers and custom headers;
// the attempt log keeps the headers with custom values redacted.
func TestForwardHeaders(t *testing.T) {
	x := newFwdFixture(t, 5)
	hint := "1234"
	ep := x.addEndpoint(t, func(e *domain.WebhookEndpoint) {
		enc := `enc:[{"name":"Authorization","value":"Bearer token-1234"},{"name":"x-team","value":"core"}]`
		e.HeadersEnc = &enc
		e.HeadersHint = []domain.WebhookHeaderHint{{Name: "Authorization", ValueLast4: &hint}, {Name: "x-team"}}
	})
	d := x.addDelivery(t, ep, false, 8)
	if err := x.run(t, d, 1, false); err != nil {
		t.Fatal(err)
	}
	reqs := x.recv.requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d", len(reqs))
	}
	h := reqs[0].header
	stored := x.delivery(t, d)
	want := map[string]string{
		"Content-Type": "application/json", "User-Agent": "KeiPay-Webhooks/1", "X-Keipay-Event-Id": stored.Event.PublicID,
		"X-Keipay-Event-Type": "payment.paid", "X-Keipay-Delivery-Id": d.PublicID,
		"Authorization": "Bearer token-1234", "X-Team": "core",
	}
	for k, v := range want {
		if got := h.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	for _, absent := range []string{"X-Keipay-Test", "X-Kei-Signature", "X-Kei-Event"} {
		if h.Get(absent) != "" {
			t.Errorf("%s sent: %q", absent, h.Get(absent))
		}
	}
	if !bytes.Equal(reqs[0].body, d.Payload) {
		t.Fatalf("body = %s, want %s", reqs[0].body, d.Payload)
	}
	if !VerifyKeiPaySignature(h.Get("X-Keipay-Signature"), reqs[0].body, testSecret, 5*time.Minute, start) {
		t.Fatalf("signature %q does not verify", h.Get("X-Keipay-Signature"))
	}

	if stored.Status != domain.WebhookDeliveryDelivered || stored.Attempts != 1 || stored.NextAttemptAt != nil || stored.LastError != "" ||
		*stored.LastResponseStatus != 200 || !stored.LastAttemptAt.Equal(start) {
		t.Fatalf("delivery = %+v", stored)
	}
	atts := x.attempts(t, d)
	if len(atts) != 1 || atts[0].AttemptNo != 1 || atts[0].Manual || atts[0].URL != ep.URL || *atts[0].ResponseStatus != 200 || atts[0].Error != "" {
		t.Fatalf("attempts = %+v", atts)
	}
	logged := atts[0].RequestHeaders
	if logged["Authorization"] != "[REDACTED]" || logged["x-team"] != "[REDACTED]" || logged["X-KeiPay-Delivery-Id"] != d.PublicID ||
		!strings.HasPrefix(logged["X-KeiPay-Signature"], "t=") || logged["User-Agent"] != "KeiPay-Webhooks/1" {
		t.Fatalf("logged headers = %v", logged)
	}
	for _, v := range logged {
		if strings.Contains(v, "token-1234") || v == "core" {
			t.Fatalf("custom header value logged: %v", logged)
		}
	}
	if len(x.enq.calls) != 0 {
		t.Fatalf("a delivered attempt scheduled another: %+v", x.enq.calls)
	}
}

// Legacy endpoints also get X-Kei-Signature over the
// raw body with the current secret; test sends carry X-KeiPay-Test.
func TestForwardLegacyAndTestHeaders(t *testing.T) {
	x := newFwdFixture(t, 5)
	migrated := x.addEndpoint(t, func(e *domain.WebhookEndpoint) {
		e.APIVersion, e.LegacySignature, e.Migrated, e.SecretLast4 = domain.WebhookAPIV1, true, true, nil
		e.SecretEnc = "enc:sk_legacy_app_secret"
	})
	d := x.addDelivery(t, migrated, true, 1)
	d.Payload = []byte(`{"event":"payment.paid","payment_id":12,"metadata":{"note":"ü"}}`)
	if err := x.run(t, d, 1, false); err != nil {
		t.Fatal(err)
	}
	r := x.recv.requests()[0]
	stored := x.delivery(t, d)
	if r.header.Get("X-Kei-Signature") != hmac.Sign("sk_legacy_app_secret", r.body) || r.header.Get("X-Kei-Event") != "payment.paid" ||
		r.header.Get("X-Keipay-Test") != "true" || !bytes.Equal(r.body, stored.Payload) {
		t.Fatalf("headers = %v body %s", r.header, r.body)
	}
	if !VerifyKeiPaySignature(r.header.Get("X-Keipay-Signature"), r.body, "sk_legacy_app_secret", time.Minute, start) {
		t.Fatal("X-KeiPay-Signature does not verify with the carried-over secret")
	}
}

// A legacy delivery from before events has no event id header.
func TestForwardLegacyDeliveryWithoutEvent(t *testing.T) {
	x := newFwdFixture(t, 5)
	ep := x.addEndpoint(t, nil)
	legacy := &domain.WebhookDelivery{PublicID: "dlv_01JBKQ7Z8M4X2Y6V3N9T5R1W0C", AppID: x.app.ID, EndpointID: &ep.ID, EventType: "payment.paid",
		Payload: []byte(`{"event":"payment.paid"}`), Status: domain.WebhookDeliveryPending, Attempts: 2, MaxAttempts: 8}
	if err := x.hooks.Deliveries().Create(context.Background(), legacy); err != nil {
		t.Fatal(err)
	}
	// Tasks queued before attempts were numbered carry only the delivery id.
	if err := x.fwd.HandleWebhookForward(context.Background(), task(t, `{"delivery_id":`+strconv.FormatInt(legacy.ID, 10)+`}`)); err != nil {
		t.Fatal(err)
	}
	r := x.recv.requests()[0]
	if _, ok := r.header["X-Keipay-Event-Id"]; ok || r.header.Get("X-Keipay-Delivery-Id") != legacy.PublicID {
		t.Fatalf("headers = %v", r.header)
	}
	if got := x.delivery(t, legacy); got.Attempts != 3 || got.Status != domain.WebhookDeliveryDelivered {
		t.Fatalf("legacy delivery = %+v", got)
	}
}

// During the overlap both secrets sign, newest first;
// after it only the new one.
func TestForwardSignatureDuringOverlap(t *testing.T) {
	x := newFwdFixture(t, 5)
	const oldSecret = "kpwh_old000000000000000000000000000000000000000000"
	expires := start.Add(time.Hour)
	ep := x.addEndpoint(t, func(e *domain.WebhookEndpoint) {
		prev := "enc:" + oldSecret
		e.PreviousSecretEnc, e.PreviousSecretExpiresAt, e.LegacySignature = &prev, &expires, true
	})
	d := x.addDelivery(t, ep, false, 8)
	if err := x.run(t, d, 1, false); err != nil {
		t.Fatal(err)
	}
	r := x.recv.requests()[0]
	sig := r.header.Get("X-Keipay-Signature")
	if want := Signature(start, r.body, testSecret, oldSecret); sig != want || strings.Count(sig, "v1=") != 2 {
		t.Fatalf("signature = %q, want %q", sig, want)
	}
	for _, secret := range []string{testSecret, oldSecret} {
		if !VerifyKeiPaySignature(sig, r.body, secret, 5*time.Minute, start) {
			t.Fatalf("does not verify with %s", secret[:9])
		}
	}
	if r.header.Get("X-Kei-Signature") != hmac.Sign(testSecret, r.body) {
		t.Fatal("X-Kei-Signature not signed with the current secret")
	}
	// The README verifier rejects tampering, a wrong secret and an old t.
	if VerifyKeiPaySignature(sig, append([]byte(" "), r.body...), testSecret, 5*time.Minute, start) ||
		VerifyKeiPaySignature(sig, r.body, "kpwh_wrong", 5*time.Minute, start) ||
		VerifyKeiPaySignature(sig, r.body, testSecret, 5*time.Minute, start.Add(5*time.Minute+time.Second)) ||
		VerifyKeiPaySignature(sig, r.body, testSecret, 5*time.Minute, start.Add(-6*time.Minute)) ||
		VerifyKeiPaySignature("v1="+sig, r.body, testSecret, 5*time.Minute, start) {
		t.Fatal("a bad signature verified")
	}

	x.clock.Advance(time.Hour)
	d2 := x.addDelivery(t, ep, false, 8)
	if err := x.run(t, d2, 1, false); err != nil {
		t.Fatal(err)
	}
	sig = x.recv.requests()[1].header.Get("X-Keipay-Signature")
	if strings.Count(sig, "v1=") != 1 || VerifyKeiPaySignature(sig, x.recv.requests()[1].body, oldSecret, time.Minute, x.clock.Now()) {
		t.Fatalf("expired previous secret still signs: %q", sig)
	}
}

// Failures are retried on the endpoint's schedule and the
// delivery fails once attempts run out; every attempt is logged.
func TestForwardRetrySchedule(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy domain.RetryPolicy
		waits  []time.Duration
	}{
		{"exponential", domain.RetryPolicy{MaxAttempts: 4, Backoff: domain.BackoffExponential, IntervalSeconds: 30},
			[]time.Duration{30 * time.Second, time.Minute, 2 * time.Minute}},
		{"fixed", domain.RetryPolicy{MaxAttempts: 3, Backoff: domain.BackoffFixed, IntervalSeconds: 45},
			[]time.Duration{45 * time.Second, 45 * time.Second}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := newFwdFixture(t, 0)
			x.recv.status(http.StatusInternalServerError)
			ep := x.addEndpoint(t, func(e *domain.WebhookEndpoint) { e.RetryPolicy = tc.policy })
			d := x.addDelivery(t, ep, false, tc.policy.MaxAttempts)
			for n := 1; n <= tc.policy.MaxAttempts; n++ {
				if err := x.run(t, d, n, false); err != nil {
					t.Fatal(err)
				}
				got := x.delivery(t, d)
				if n < tc.policy.MaxAttempts {
					due := x.clock.Now().Add(tc.waits[n-1])
					if got.Status != domain.WebhookDeliveryPending || got.Attempts != n || !got.NextAttemptAt.Equal(due) || got.LastError != "status=500" {
						t.Fatalf("after attempt %d: %+v", n, got)
					}
					if c := x.enq.calls[len(x.enq.calls)-1]; c != (scheduled{id: d.ID, attempt: n + 1, at: due}) {
						t.Fatalf("scheduled = %+v", c)
					}
					x.clock.Advance(tc.waits[n-1])
					continue
				}
				if got.Status != domain.WebhookDeliveryFailed || got.Attempts != n || got.NextAttemptAt != nil || *got.LastResponseStatus != 500 {
					t.Fatalf("final: %+v", got)
				}
			}
			atts := x.attempts(t, d)
			if len(atts) != tc.policy.MaxAttempts || len(x.enq.calls) != tc.policy.MaxAttempts-1 {
				t.Fatalf("attempts=%d scheduled=%d", len(atts), len(x.enq.calls))
			}
			for i, a := range atts {
				if a.AttemptNo != i+1 || a.Error != "unexpected status 500" || *a.ResponseStatus != 500 {
					t.Fatalf("attempt %d = %+v", i, a)
				}
			}
		})
	}
}

// A redirect is not followed and counts as a failure.
func TestForwardRedirectIsFailure(t *testing.T) {
	x := newFwdFixture(t, 5)
	followed := newReceiver(t)
	x.recv.answer = func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, followed.srv.URL, http.StatusFound)
	}
	d := x.addDelivery(t, x.addEndpoint(t, nil), false, 2)
	if err := x.run(t, d, 1, false); err != nil {
		t.Fatal(err)
	}
	if len(followed.requests()) != 0 {
		t.Fatal("redirect followed")
	}
	got := x.delivery(t, d)
	if got.Status != domain.WebhookDeliveryPending || got.LastError != "status=302" || x.attempts(t, d)[0].Error != "unexpected status 302" {
		t.Fatalf("delivery = %+v", got)
	}
}

// A timeout is a failed attempt without a response status.
func TestForwardTimeoutIsFailure(t *testing.T) {
	x := newFwdFixture(t, 5)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	x.recv.answer = func(http.ResponseWriter, *http.Request) { <-release }
	x.fwd = x.newForwarder(5, &http.Client{Timeout: 50 * time.Millisecond})
	d := x.addDelivery(t, x.addEndpoint(t, nil), false, 1)
	if err := x.run(t, d, 1, false); err != nil {
		t.Fatal(err)
	}
	got := x.delivery(t, d)
	a := x.attempts(t, d)[0]
	if got.Status != domain.WebhookDeliveryFailed || got.LastResponseStatus != nil || a.ResponseStatus != nil ||
		!strings.Contains(a.Error, "Client.Timeout") || got.LastError != a.Error {
		t.Fatalf("delivery = %+v attempt = %+v", got, a)
	}
}

// At most 4096 bytes of the answer are kept, as valid UTF-8 without NUL.
func TestForwardResponseBodyKept(t *testing.T) {
	cases := []struct {
		name      string
		body      []byte
		want      string
		truncated bool
	}{
		{"short", []byte("ok"), "ok", false},
		{"exactly 4096", bytes.Repeat([]byte("a"), 4096), strings.Repeat("a", 4096), false},
		{"long", bytes.Repeat([]byte("b"), 10000), strings.Repeat("b", 4096), true},
		{"invalid utf-8 and NUL", []byte("x\xff\x00y"), "x��y", false},
		{"rune cut at the limit", append(bytes.Repeat([]byte("c"), 4095), []byte("é")...), strings.Repeat("c", 4095), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x := newFwdFixture(t, 5)
			x.recv.answer = func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(tc.body) }
			d := x.addDelivery(t, x.addEndpoint(t, nil), false, 1)
			if err := x.run(t, d, 1, false); err != nil {
				t.Fatal(err)
			}
			a := x.attempts(t, d)[0]
			if a.ResponseBody != tc.want || a.ResponseBodyTruncated != tc.truncated || !utf8.ValidString(a.ResponseBody) || len(a.ResponseBody) > 4096 {
				t.Fatalf("body len %d truncated %v: %q", len(a.ResponseBody), a.ResponseBodyTruncated, a.ResponseBody[:min(len(a.ResponseBody), 20)])
			}
		})
	}
}

// Stale tasks and finished deliveries are ignored.
func TestForwardIgnoresStaleTasks(t *testing.T) {
	x := newFwdFixture(t, 5)
	ep := x.addEndpoint(t, nil)
	d := x.addDelivery(t, ep, false, 8)
	for _, attempt := range []int{2, 7} {
		if err := x.run(t, d, attempt, false); err != nil {
			t.Fatal(err)
		}
	}
	if len(x.recv.requests()) != 0 {
		t.Fatal("stale task sent a request")
	}
	if err := x.run(t, d, 1, false); err != nil {
		t.Fatal(err)
	}
	if err := x.run(t, d, 1, false); err != nil || len(x.recv.requests()) != 1 || len(x.attempts(t, d)) != 1 {
		t.Fatalf("second run of attempt 1: err=%v requests=%d", err, len(x.recv.requests()))
	}
	if err := x.run(t, d, 0, false); err != nil || len(x.recv.requests()) != 1 {
		t.Fatal("a delivered delivery was sent again")
	}
}

// The optimistic guard: a worker whose attempt was recorded by another while
// it was sending writes nothing.
func TestForwardGuardLosesQuietly(t *testing.T) {
	x := newFwdFixture(t, 5)
	ep := x.addEndpoint(t, nil)
	d := x.addDelivery(t, ep, false, 8)
	x.recv.answer = func(w http.ResponseWriter, _ *http.Request) {
		cur := x.delivery(t, d)
		next := cur
		next.Status, next.Attempts = domain.WebhookDeliveryDelivered, 1
		other := &domain.DeliveryAttempt{DeliveryID: d.ID, AttemptNo: 1, URL: "other-worker", StartedAt: start}
		_, _ = x.hooks.Deliveries().RecordAttempt(context.Background(), &next, 0, other)
		w.WriteHeader(http.StatusInternalServerError)
	}
	if err := x.run(t, d, 1, false); err != nil {
		t.Fatal(err)
	}
	atts := x.attempts(t, d)
	if len(atts) != 1 || atts[0].URL != "other-worker" || x.delivery(t, d).Status != domain.WebhookDeliveryDelivered || len(x.enq.calls) != 0 {
		t.Fatalf("attempts = %+v", atts)
	}
}

// Exhausted deliveries in a row disable the endpoint at the
// threshold, audited as system:worker; a success resets the count; test and
// manual attempts never count.
func TestForwardAutoDisable(t *testing.T) {
	x := newFwdFixture(t, 3)
	ep := x.addEndpoint(t, nil)
	fail := func(isTest, manual bool) {
		t.Helper()
		d := x.addDelivery(t, ep, isTest, 1)
		if err := x.run(t, d, 1, manual); err != nil {
			t.Fatal(err)
		}
	}
	x.recv.status(http.StatusServiceUnavailable)
	fail(false, false)
	fail(false, false)
	x.recv.status(http.StatusNoContent)
	ok := x.addDelivery(t, ep, false, 1)
	if err := x.run(t, ok, 1, false); err != nil {
		t.Fatal(err)
	}
	if got := x.endpoint(t, ep); got.ConsecutiveFailures != 0 {
		t.Fatalf("success did not reset: %d", got.ConsecutiveFailures)
	}
	x.recv.status(http.StatusServiceUnavailable)
	fail(false, false)
	fail(true, false)
	fail(false, true)
	fail(false, false)
	if got := x.endpoint(t, ep); got.ConsecutiveFailures != 2 || got.Status != domain.WebhookEndpointActive {
		t.Fatalf("test or manual failure counted: %+v", got)
	}
	if len(x.audit.Logs()) != 0 {
		t.Fatal("audited before the threshold")
	}
	fail(false, false)
	got := x.endpoint(t, ep)
	if got.Status != domain.WebhookEndpointDisabled || *got.DisabledReason != domain.WebhookDisabledConsecutiveFailures ||
		got.ConsecutiveFailures != 3 || !got.DisabledAt.Equal(start) {
		t.Fatalf("endpoint = %+v", got)
	}
	logs := x.audit.Logs()
	if len(logs) != 1 || logs[0].Action != "webhook_endpoint.auto_disable" || logs[0].Actor != domain.SystemActor("worker") ||
		logs[0].Target != (domain.AuditTarget{Type: "webhook_endpoint", ID: ep.PublicID}) ||
		string(logs[0].After) != `{"consecutive_failures":3,"disabled_reason":"consecutive_failures","status":"disabled"}` {
		t.Fatalf("audit = %+v %s", logs, logs[0].After)
	}

	// Pending deliveries of the disabled endpoint end without an attempt.
	pending := x.addDelivery(t, ep, false, 8)
	if err := x.run(t, pending, 1, false); err != nil {
		t.Fatal(err)
	}
	if got := x.delivery(t, pending); got.Status != domain.WebhookDeliveryFailed || got.LastError != "endpoint disabled" {
		t.Fatalf("pending delivery = %+v", got)
	}
}

// A threshold of 0 never disables.
func TestForwardAutoDisableOff(t *testing.T) {
	x := newFwdFixture(t, 0)
	x.recv.status(http.StatusInternalServerError)
	ep := x.addEndpoint(t, nil)
	for range 6 {
		if err := x.run(t, x.addDelivery(t, ep, false, 1), 1, false); err != nil {
			t.Fatal(err)
		}
	}
	if got := x.endpoint(t, ep); got.Status != domain.WebhookEndpointActive || got.ConsecutiveFailures != 6 {
		t.Fatalf("endpoint = %+v", got)
	}
}

// Deliveries an endpoint can no longer take end FAILED
// without an attempt; a test send to a disabled endpoint still goes out.
func TestForwardBlockedDeliveries(t *testing.T) {
	x := newFwdFixture(t, 5)
	now := start
	disabled := x.addEndpoint(t, func(e *domain.WebhookEndpoint) {
		reason := domain.WebhookDisabledManual
		e.Status, e.DisabledReason, e.DisabledAt = domain.WebhookEndpointDisabled, &reason, &now
	})
	deleted := x.addEndpoint(t, func(e *domain.WebhookEndpoint) { e.DeletedAt = &now })
	cases := map[string]*domain.WebhookDelivery{
		"endpoint disabled": x.addDelivery(t, disabled, false, 8),
		"endpoint deleted":  x.addDelivery(t, deleted, false, 8),
		"no endpoint":       x.addDelivery(t, nil, false, 8),
	}
	for reason, d := range cases {
		if err := x.run(t, d, 1, false); err != nil {
			t.Fatal(err)
		}
		if got := x.delivery(t, d); got.Status != domain.WebhookDeliveryFailed || got.LastError != reason || got.Attempts != 0 || got.NextAttemptAt != nil {
			t.Fatalf("%s: %+v", reason, got)
		}
		if len(x.attempts(t, d)) != 0 {
			t.Fatalf("%s: attempt logged", reason)
		}
	}
	if len(x.recv.requests()) != 0 || x.endpoint(t, disabled).ConsecutiveFailures != 0 {
		t.Fatal("blocked deliveries sent or counted")
	}
	test := x.addDelivery(t, disabled, true, 1)
	if err := x.run(t, test, 1, false); err != nil {
		t.Fatal(err)
	}
	if len(x.recv.requests()) != 1 || x.delivery(t, test).Status != domain.WebhookDeliveryDelivered {
		t.Fatal("test send to a disabled endpoint not made")
	}
}

// A replay's single attempt is never retried and never counts.
func TestForwardManualAttempt(t *testing.T) {
	x := newFwdFixture(t, 1)
	x.recv.status(http.StatusBadGateway)
	ep := x.addEndpoint(t, nil)
	d := x.addDelivery(t, ep, false, 8)
	if err := x.run(t, d, 1, true); err != nil {
		t.Fatal(err)
	}
	got := x.delivery(t, d)
	if got.Status != domain.WebhookDeliveryFailed || len(x.enq.calls) != 0 || !x.attempts(t, d)[0].Manual {
		t.Fatalf("manual failure = %+v", got)
	}
	if e := x.endpoint(t, ep); e.ConsecutiveFailures != 0 || e.Status != domain.WebhookEndpointActive {
		t.Fatalf("manual failure counted: %+v", e)
	}
}

// A secret that cannot be decrypted fails the delivery without a request and
// without asynq retries.
func TestForwardDecryptFailure(t *testing.T) {
	x := newFwdFixture(t, 5)
	ep := x.addEndpoint(t, func(e *domain.WebhookEndpoint) { e.SecretEnc = "bad:secret" })
	d := x.addDelivery(t, ep, false, 8)
	err := x.run(t, d, 1, false)
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("err = %v", err)
	}
	if got := x.delivery(t, d); got.Status != domain.WebhookDeliveryFailed || got.LastError != "secret decrypt failed" || len(x.recv.requests()) != 0 {
		t.Fatalf("delivery = %+v", got)
	}
}

// A retry the queue refuses is left for the sweeper; the task still succeeds.
func TestForwardRetryEnqueueFailure(t *testing.T) {
	x := newFwdFixture(t, 5)
	x.recv.status(http.StatusInternalServerError)
	x.enq.err = errors.New("redis down")
	d := x.addDelivery(t, x.addEndpoint(t, nil), false, 8)
	if err := x.run(t, d, 1, false); err != nil {
		t.Fatalf("err = %v", err)
	}
	if got := x.delivery(t, d); got.Status != domain.WebhookDeliveryPending || got.NextAttemptAt == nil {
		t.Fatalf("delivery = %+v", got)
	}
}

func TestForwardBadPayloads(t *testing.T) {
	x := newFwdFixture(t, 5)
	if err := x.fwd.HandleWebhookForward(context.Background(), task(t, `{`)); !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("malformed: %v", err)
	}
	if err := x.fwd.HandleWebhookForward(context.Background(), task(t, `{"delivery_id":999}`)); !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("unknown delivery: %v", err)
	}
}

func TestForwardTaskID(t *testing.T) {
	tk, err := NewWebhookForwardTask(42, 3, true)
	if err != nil || string(tk.Payload()) != `{"delivery_id":42,"attempt":3,"manual":true}` || tk.Type() != "webhook:forward" {
		t.Fatalf("task = %s %v", tk.Payload(), err)
	}
	if ForwardTaskID(42, 3) != "webhook:forward:42:3" {
		t.Fatal(ForwardTaskID(42, 3))
	}
}
