package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
)

// deliveredOnce makes the fixture's payment PAID, fanned out to one endpoint,
// and finishes its delivery with status after one attempt.
func deliveredOnce(t *testing.T, f *webhookFixture, status domain.WebhookDeliveryStatus) (*domain.WebhookEndpoint, domain.WebhookDelivery) {
	t.Helper()
	e := f.addEndpoint(t, f.app, CreateWebhookEndpointInput{}).Endpoint
	if err := f.process(t, f.conn, paid(f.payment.ExternalID, 5000)); err != nil {
		t.Fatal(err)
	}
	d := f.hooks.AllDeliveries()[0]
	next := d
	next.Status, next.Attempts, next.NextAttemptAt = status, 1, nil
	a := &domain.DeliveryAttempt{DeliveryID: d.ID, AttemptNo: 1, URL: e.URL, StartedAt: fixtureStart, RequestHeaders: map[string]string{}}
	if ok, err := f.hooks.Deliveries().RecordAttempt(context.Background(), &next, 0, a); err != nil || !ok {
		t.Fatalf("record attempt: %v %v", ok, err)
	}
	f.enq.calls = nil
	f.audit.Reset()
	return e, f.hooks.AllDeliveries()[0]
}

// T-B25 / AC-11: a finished delivery gets exactly one more manual attempt
// with the same event and body; nothing new is created.
func TestReplay(t *testing.T) {
	for _, status := range []domain.WebhookDeliveryStatus{domain.WebhookDeliveryFailed, domain.WebhookDeliveryDelivered} {
		t.Run(string(status), func(t *testing.T) {
			f := newWebhookFixture(t)
			_, d := deliveredOnce(t, f, status)
			f.clock.Advance(time.Hour)
			got, err := f.deliveries.Replay(adminCtx(), d.PublicID)
			if err != nil {
				t.Fatal(err)
			}
			now := fixtureStart.Add(time.Hour)
			if got.Status != domain.WebhookDeliveryPending || got.MaxAttempts != 2 || got.Attempts != 1 || !got.NextAttemptAt.Equal(now) ||
				got.Event == nil || got.Event.PublicID != d.Event.PublicID {
				t.Fatalf("replayed = %+v", got)
			}
			stored := f.hooks.AllDeliveries()
			if len(stored) != 1 || len(f.hooks.AllEvents()) != 1 || string(stored[0].Payload) != string(d.Payload) || stored[0].MaxAttempts != 2 {
				t.Fatalf("stored = %+v", stored)
			}
			if len(f.enq.calls) != 1 || f.enq.calls[0] != (enqueued{id: d.ID, attempt: 2, manual: true, at: now}) {
				t.Fatalf("enqueued = %+v", f.enq.calls)
			}
			logs := f.audit.Logs()
			if len(logs) != 1 || logs[0].Action != "webhook_delivery.replay" || logs[0].Target != (domain.AuditTarget{Type: "webhook_delivery", ID: d.PublicID}) {
				t.Fatalf("audit = %+v", logs)
			}
			assertJSON(t, "before", logs[0].Before, map[string]any{"status": string(status), "attempts": 1})
			assertJSON(t, "after", logs[0].After, nil)

			// While the replay is pending it cannot be replayed again.
			_, err = f.deliveries.Replay(adminCtx(), d.PublicID)
			assertNotReplayable(t, err, "in_progress")
		})
	}
}

func assertNotReplayable(t *testing.T, err error, reason string) {
	t.Helper()
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != domain.CodeDeliveryNotReplayable || de.Details["reason"] != reason || !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v, want delivery_not_replayable %s", err, reason)
	}
}

// T-B25: deliveries that cannot take another attempt get 409 with a reason.
func TestReplayRefused(t *testing.T) {
	cases := map[string]func(t *testing.T, f *webhookFixture) string{
		"in_progress": func(t *testing.T, f *webhookFixture) string {
			f.addEndpoint(t, f.app, CreateWebhookEndpointInput{})
			if err := f.process(t, f.conn, paid(f.payment.ExternalID, 5000)); err != nil {
				t.Fatal(err)
			}
			return f.hooks.AllDeliveries()[0].PublicID
		},
		"no_endpoint": func(t *testing.T, f *webhookFixture) string {
			d := &domain.WebhookDelivery{PublicID: id.New(id.Delivery), PaymentID: &f.payment.ID, AppID: f.app.ID, EventType: "payment.paid",
				Payload: []byte(`{}`), Status: domain.WebhookDeliveryFailed, Attempts: 6, MaxAttempts: 6}
			if err := f.hooks.Deliveries().Create(context.Background(), d); err != nil {
				t.Fatal(err)
			}
			return d.PublicID
		},
		"endpoint_deleted": func(t *testing.T, f *webhookFixture) string {
			e, d := deliveredOnce(t, f, domain.WebhookDeliveryFailed)
			if err := f.endpoints.Delete(adminCtx(), e.PublicID); err != nil {
				t.Fatal(err)
			}
			return d.PublicID
		},
		"endpoint_disabled": func(t *testing.T, f *webhookFixture) string {
			e, d := deliveredOnce(t, f, domain.WebhookDeliveryDelivered)
			if _, err := f.endpoints.Disable(adminCtx(), e.PublicID); err != nil {
				t.Fatal(err)
			}
			return d.PublicID
		},
	}
	for reason, setup := range cases {
		t.Run(reason, func(t *testing.T) {
			f := newWebhookFixture(t)
			pub := setup(t, f)
			f.enq.calls = nil
			f.audit.Reset()
			_, err := f.deliveries.Replay(adminCtx(), pub)
			assertNotReplayable(t, err, reason)
			if len(f.enq.calls) != 0 || len(f.audit.Logs()) != 0 {
				t.Fatal("a refused replay enqueued or recorded something")
			}
		})
	}
	f := newWebhookFixture(t)
	if _, err := f.deliveries.Replay(adminCtx(), id.New(id.Delivery)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown delivery: %v", err)
	}
}

// A queue failure leaves the delivery PENDING for the sweeper and still
// answers; a replay without an actor changes nothing.
func TestReplayEnqueueFailureAndActor(t *testing.T) {
	f := newWebhookFixture(t)
	_, d := deliveredOnce(t, f, domain.WebhookDeliveryFailed)
	if _, err := f.deliveries.Replay(context.Background(), d.PublicID); !errors.Is(err, ErrNoActor) {
		t.Fatalf("no actor: %v", err)
	}
	if got := f.hooks.AllDeliveries()[0]; got.Status != domain.WebhookDeliveryFailed || got.MaxAttempts != d.MaxAttempts {
		t.Fatalf("rolled back delivery = %+v", got)
	}
	f.enq.err = errors.New("redis down")
	got, err := f.deliveries.Replay(adminCtx(), d.PublicID)
	if err != nil || got.Status != domain.WebhookDeliveryPending {
		t.Fatalf("replay with the queue down: %+v %v", got, err)
	}
}

// Replaying again after the replay finished works.
func TestReplayTwice(t *testing.T) {
	f := newWebhookFixture(t)
	_, d := deliveredOnce(t, f, domain.WebhookDeliveryFailed)
	if _, err := f.deliveries.Replay(adminCtx(), d.PublicID); err != nil {
		t.Fatal(err)
	}
	cur := f.hooks.AllDeliveries()[0]
	next := cur
	next.Status, next.Attempts = domain.WebhookDeliveryFailed, 2
	a := &domain.DeliveryAttempt{DeliveryID: d.ID, AttemptNo: 2, Manual: true, URL: "https://x.example", StartedAt: fixtureStart}
	if ok, err := f.hooks.Deliveries().RecordAttempt(context.Background(), &next, 1, a); err != nil || !ok {
		t.Fatal(err)
	}
	got, err := f.deliveries.Replay(adminCtx(), d.PublicID)
	if err != nil || got.MaxAttempts != 3 {
		t.Fatalf("second replay = %+v %v", got, err)
	}
}

func TestDeliveryGetAndList(t *testing.T) {
	f := newWebhookFixture(t)
	_, d := deliveredOnce(t, f, domain.WebhookDeliveryFailed)
	got, attempts, err := f.deliveries.Get(context.Background(), d.PublicID)
	if err != nil || got.PublicID != d.PublicID || len(got.Payload) == 0 || len(attempts) != 1 || attempts[0].AttemptNo != 1 {
		t.Fatalf("get = %+v %+v %v", got, attempts, err)
	}
	if _, _, err := f.deliveries.Get(context.Background(), id.New(id.Delivery)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	status := domain.WebhookDeliveryFailed
	page, err := f.deliveries.List(context.Background(), domain.DeliveryFilter{Status: &status}, domain.PageRequest{})
	if err != nil || len(page.Items) != 1 || page.Items[0].Payload != nil {
		t.Fatalf("list = %+v %v", page, err)
	}
	if _, err := f.deliveries.List(context.Background(), domain.DeliveryFilter{}, domain.PageRequest{Limit: 201}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("bad limit: %v", err)
	}
}

func TestInboundService(t *testing.T) {
	f := newWebhookFixture(t)
	if err := f.process(t, f.conn, paid(f.payment.ExternalID, 5000)); err != nil {
		t.Fatal(err)
	}
	svc := NewInboundService(f.hooks.Inbound())
	page, err := svc.List(context.Background(), domain.InboundFilter{}, domain.PageRequest{})
	if err != nil || len(page.Items) != 1 || page.Items[0].RawBody != nil || page.Items[0].Connection.PublicID != f.conn.PublicID {
		t.Fatalf("list = %+v %v", page, err)
	}
	got, err := svc.Get(context.Background(), page.Items[0].PublicID)
	if err != nil || len(got.RawBody) == 0 || got.PaymentPublicID != f.payment.PublicID || got.Event == nil {
		t.Fatalf("get = %+v %v", got, err)
	}
	if _, err := svc.Get(context.Background(), id.New(id.Inbound)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	if _, err := svc.List(context.Background(), domain.InboundFilter{}, domain.PageRequest{Cursor: "bad"}); !errors.Is(err, domain.ErrInvalidCursor) {
		t.Fatalf("bad cursor: %v", err)
	}
}
