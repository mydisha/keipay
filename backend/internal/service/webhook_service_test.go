package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/testutil/memstore"
)

func paid(ref string, amount int64) xenditCallback {
	return xenditCallback{ID: "inv_1", ExternalID: ref, Status: "PAID", Amount: amount, Currency: "IDR", PaidAmount: amount}
}

// One event per applied callback, fanned out to every active,
// not deleted endpoint whose subscriptions match; all its deliveries carry
// the event's id.
func TestProcessFansOutToMatchingEndpoints(t *testing.T) {
	f := newWebhookFixture(t)
	all := f.addEndpoint(t, f.app, CreateWebhookEndpointInput{Events: []string{"*"}})
	group := f.addEndpoint(t, f.app, CreateWebhookEndpointInput{Events: []string{"payment.*"}})
	settledOnly := f.addEndpoint(t, f.app, CreateWebhookEndpointInput{Events: []string{"payment.settled"}})
	disabled := f.addEndpoint(t, f.app, CreateWebhookEndpointInput{Events: []string{"*"}})
	if _, err := f.endpoints.Disable(adminCtx(), disabled.Endpoint.PublicID); err != nil {
		t.Fatal(err)
	}
	deleted := f.addEndpoint(t, f.app, CreateWebhookEndpointInput{Events: []string{"*"}})
	if err := f.endpoints.Delete(adminCtx(), deleted.Endpoint.PublicID); err != nil {
		t.Fatal(err)
	}
	other := f.addEndpoint(t, f.addApp(t, "globex", domain.EnvTest), CreateWebhookEndpointInput{Events: []string{"*"}})

	if err := f.process(t, f.conn, paid(f.payment.ExternalID, 5000)); err != nil {
		t.Fatal(err)
	}
	events, ds := f.hooks.AllEvents(), f.hooks.AllDeliveries()
	if len(events) != 1 || events[0].Type != domain.EventPaymentPaid || events[0].IsTest || *events[0].PaymentID != f.payment.ID {
		t.Fatalf("events = %+v", events)
	}
	assertDeliveredTo(t, ds, all.Endpoint, group.Endpoint)
	for i, d := range ds {
		if d.Event == nil || d.Event.PublicID != events[0].PublicID || d.Status != domain.WebhookDeliveryPending || d.Attempts != 0 ||
			d.MaxAttempts != 8 || d.PaymentPublicID != f.payment.PublicID || d.EventType != "payment.paid" ||
			!d.NextAttemptAt.Equal(fixtureStart) || !strings.HasPrefix(d.PublicID, "dlv_") {
			t.Fatalf("delivery %d = %+v", i, d)
		}
		if c := f.enq.calls[i]; c.id != d.ID || c.attempt != 1 || c.manual || !c.at.Equal(fixtureStart) {
			t.Fatalf("enqueued %d = %+v", i, c)
		}
	}
	in := f.lastInbound(t)
	if in.Result != domain.InboundApplied || in.PaymentID == nil || *in.PaymentID != f.payment.ID ||
		in.Event == nil || in.Event.PublicID != events[0].PublicID {
		t.Fatalf("inbound = %+v", in)
	}

	settled := xenditCallback{ID: "inv_1", ExternalID: f.payment.ExternalID, Status: "SETTLED", Amount: 5000}
	if err := f.process(t, f.conn, settled); err != nil {
		t.Fatal(err)
	}
	assertDeliveredTo(t, f.hooks.AllDeliveries()[2:], all.Endpoint, group.Endpoint, settledOnly.Endpoint)
	for _, d := range f.hooks.AllDeliveries() {
		if *d.EndpointID == other.Endpoint.ID || *d.EndpointID == disabled.Endpoint.ID || *d.EndpointID == deleted.Endpoint.ID {
			t.Fatalf("delivery to an endpoint that should get none: %+v", d)
		}
	}
}

func assertDeliveredTo(t *testing.T, ds []domain.WebhookDelivery, want ...*domain.WebhookEndpoint) {
	t.Helper()
	if len(ds) != len(want) {
		t.Fatalf("got %d deliveries, want %d: %+v", len(ds), len(want), ds)
	}
	for i, e := range want {
		if ds[i].EndpointID == nil || *ds[i].EndpointID != e.ID {
			t.Fatalf("delivery %d went to %v, want %s", i, ds[i].EndpointID, e.PublicID)
		}
	}
}

// Without endpoints the event is still created and the callback acked.
func TestProcessWithoutEndpoints(t *testing.T) {
	f := newWebhookFixture(t)
	if err := f.process(t, f.conn, paid(f.payment.ExternalID, 5000)); err != nil {
		t.Fatal(err)
	}
	if len(f.hooks.AllEvents()) != 1 || len(f.hooks.AllDeliveries()) != 0 || len(f.enq.calls) != 0 {
		t.Fatalf("events=%d deliveries=%d enqueued=%d", len(f.hooks.AllEvents()), len(f.hooks.AllDeliveries()), len(f.enq.calls))
	}
	if f.paymentStatus(t, f.payment) != domain.PaymentStatusPaid || len(f.reports.All()) != 1 {
		t.Fatal("callback not applied")
	}
}

// v2 is the envelope, v1 the old flat body plus event_id; both
// are rendered from the event alone, so they are the same every time.
func TestDeliveryBodies(t *testing.T) {
	f := newWebhookFixture(t)
	v1 := f.addEndpoint(t, f.app, CreateWebhookEndpointInput{APIVersion: ptr("v1")})
	f.addEndpoint(t, f.app, CreateWebhookEndpointInput{})
	p := f.addPayment(t, f.app, f.conn, "kp_pay_body", 7000)
	cb := xenditCallback{ID: "inv_9", ExternalID: p.ExternalID, Status: "PAID", Amount: 7000, PaidAt: "2026-10-29T08:59:30Z"}
	if err := f.process(t, f.conn, cb); err != nil {
		t.Fatal(err)
	}
	ev := f.hooks.AllEvents()[0]
	ds := f.hooks.AllDeliveries()
	wantData := map[string]any{
		"id": p.PublicID, "order_id": p.OrderID, "reference": p.ExternalID, "status": "PAID", "amount": json.Number("7000"),
		"currency": "IDR", "metadata": map[string]any{}, "provider": "xendit", "connection_id": f.conn.PublicID,
		"paid_at": "2026-10-29T08:59:30Z", "settled_at": nil,
	}
	assertBody(t, ds[1].Payload, map[string]any{
		"id": ev.PublicID, "type": "payment.paid", "api_version": "v2", "created_at": "2026-10-29T09:00:00Z", "data": wantData,
	})
	assertBody(t, ds[0].Payload, map[string]any{
		"event": "payment.paid", "event_id": ev.PublicID, "payment_id": json.Number(jsonInt(p.ID)), "payment_public_id": p.PublicID,
		"order_id": p.OrderID, "external_id": p.ExternalID, "metadata": map[string]any{}, "status": "PAID",
		"amount": json.Number("7000"), "currency": "IDR", "paid_at": "2026-10-29T08:59:30Z", "timestamp": "2026-10-29T09:00:00Z",
	})
	if *ds[0].EndpointID != v1.Endpoint.ID {
		t.Fatal("v1 body went to the wrong endpoint")
	}
	for _, v := range []domain.WebhookAPIVersion{domain.WebhookAPIV1, domain.WebhookAPIV2} {
		a, _ := renderBody(&ev, v)
		b, _ := renderBody(&ev, v)
		if !bytes.Equal(a, b) {
			t.Fatalf("%s body is not deterministic", v)
		}
	}
}

func jsonInt(n int64) string { b, _ := json.Marshal(n); return string(b) }

func assertBody(t *testing.T, raw json.RawMessage, want map[string]any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var got map[string]any
	if err := dec.Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("body = %s\nwant  %v", raw, want)
	}
}

// Metadata reaches both bodies with its unicode and exact digits.
func TestDeliveryBodiesKeepMetadata(t *testing.T) {
	f := newWebhookFixture(t)
	f.addEndpoint(t, f.app, CreateWebhookEndpointInput{APIVersion: ptr("v1")})
	f.addEndpoint(t, f.app, CreateWebhookEndpointInput{})
	meta := domain.Metadata{"note": "héllo 🌏 你好", "tier": json.Number("12345678901234567890")}
	p := &domain.Payment{PublicID: "pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0D", AppID: f.app.ID, OrderID: "ord-ü", ExternalID: "kp_pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0D",
		ConnectionID: &f.conn.ID, Amount: 7000, Currency: "IDR", Status: domain.PaymentStatusPending, Metadata: meta}
	if err := f.payments.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if err := f.process(t, f.conn, paid(p.ExternalID, 7000)); err != nil {
		t.Fatal(err)
	}
	wantMeta := map[string]any{"note": "héllo 🌏 你好", "tier": json.Number("12345678901234567890")}
	for _, d := range f.hooks.AllDeliveries() {
		dec := json.NewDecoder(bytes.NewReader(d.Payload))
		dec.UseNumber()
		var body map[string]any
		if err := dec.Decode(&body); err != nil {
			t.Fatal(err)
		}
		meta := body["metadata"]
		if data, ok := body["data"].(map[string]any); ok {
			meta = data["metadata"]
		}
		if !reflect.DeepEqual(meta, wantMeta) {
			t.Fatalf("metadata = %#v in %s", meta, d.Payload)
		}
	}
}

// Every outcome of a verified callback is recorded with
// the right result and fields.
func TestProcessInboundResults(t *testing.T) {
	cases := []struct {
		name       string
		setup      func(f *webhookFixture)
		cb         func(f *webhookFixture) xenditCallback
		conn       func(f *webhookFixture) *domain.Connection
		wantErr    error
		result     domain.InboundResult
		detail     string
		hasPayment bool
		hasEvent   bool
	}{
		{name: "applied", cb: func(f *webhookFixture) xenditCallback { return paid(f.payment.ExternalID, 5000) },
			result: domain.InboundApplied, hasPayment: true, hasEvent: true},
		{name: "stale", setup: func(f *webhookFixture) {
			_ = f.payments.UpdateStatus(context.Background(), f.payment.ID, domain.PaymentStatusSettled, nil, nil, nil)
		}, cb: func(f *webhookFixture) xenditCallback { return paid(f.payment.ExternalID, 5000) },
			result: domain.InboundStale, hasPayment: true},
		{name: "mismatch amount", cb: func(f *webhookFixture) xenditCallback { return paid(f.payment.ExternalID, 1000) },
			wantErr: ErrCallbackMismatch, result: domain.InboundMismatch, detail: "amount or currency differs from the payment", hasPayment: true},
		{name: "mismatch currency", cb: func(f *webhookFixture) xenditCallback {
			cb := paid(f.payment.ExternalID, 5000)
			cb.Currency = "USD"
			return cb
		}, wantErr: ErrCallbackMismatch, result: domain.InboundMismatch, detail: "amount or currency differs from the payment", hasPayment: true},
		{name: "mismatch other connection", conn: func(f *webhookFixture) *domain.Connection { return f.addConnection(t, "xendit-b") },
			cb:      func(f *webhookFixture) xenditCallback { return paid(f.payment.ExternalID, 5000) },
			wantErr: ErrCallbackMismatch, result: domain.InboundMismatch, detail: "payment belongs to another connection", hasPayment: true},
		{name: "unmapped", cb: func(f *webhookFixture) xenditCallback {
			return xenditCallback{ID: "inv_1", ExternalID: f.payment.ExternalID, Status: "REFUNDED"}
		}, result: domain.InboundUnmapped, detail: `unknown gateway status "REFUNDED"`},
		{name: "error unknown reference", cb: func(f *webhookFixture) xenditCallback { return paid("kp_pay_ghost", 5000) },
			wantErr: domain.ErrNotFound, result: domain.InboundError, detail: "unknown payment reference"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newWebhookFixture(t)
			f.addEndpoint(t, f.app, CreateWebhookEndpointInput{})
			if tc.setup != nil {
				tc.setup(f)
			}
			conn := f.conn
			if tc.conn != nil {
				conn = tc.conn(f)
			}
			cb := tc.cb(f)
			err := f.process(t, conn, cb)
			if (tc.wantErr == nil && err != nil) || (tc.wantErr != nil && !errors.Is(err, tc.wantErr)) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			rows := f.hooks.AllInbound()
			if len(rows) != 1 {
				t.Fatalf("rows = %+v", rows)
			}
			in := rows[0]
			if in.Result != tc.result || in.ResultDetail != tc.detail || !in.SignatureValid || in.ConnectionID != conn.ID ||
				in.Source != "webhook" || in.Provider != "xendit" || !strings.HasPrefix(in.PublicID, "inb_") {
				t.Fatalf("inbound = %+v", in)
			}
			if *in.EventKey != cb.ID || *in.ProviderStatus != cb.Status || *in.Reference != cb.ExternalID ||
				!bytes.Equal(in.RawBody, cb.raw()) || in.BodyTruncated {
				t.Fatalf("callback fields = %+v", in)
			}
			if (in.PaymentID != nil) != tc.hasPayment || (in.EventID != nil) != tc.hasEvent {
				t.Fatalf("payment=%v event=%v", in.PaymentID, in.EventID)
			}
			if !tc.hasEvent && (len(f.hooks.AllEvents()) != 0 || len(f.hooks.AllDeliveries()) != 0 || len(f.enq.calls) != 0) {
				t.Fatal("a callback that was not applied created an event")
			}
		})
	}
}

// The same event key and status twice at one connection is a
// duplicate: recorded, nothing changes, nothing is sent again.
func TestProcessDuplicate(t *testing.T) {
	f := newWebhookFixture(t)
	f.addEndpoint(t, f.app, CreateWebhookEndpointInput{})
	cb := paid(f.payment.ExternalID, 5000)
	for range 2 {
		if err := f.process(t, f.conn, cb); err != nil {
			t.Fatal(err)
		}
	}
	rows := f.hooks.AllInbound()
	if len(rows) != 2 || rows[0].Result != domain.InboundApplied || rows[1].Result != domain.InboundDuplicate ||
		rows[1].PaymentID != nil || *rows[1].EventKey != "inv_1" {
		t.Fatalf("rows = %+v", rows)
	}
	if len(f.reports.All()) != 1 || len(f.hooks.AllDeliveries()) != 1 || len(f.enq.calls) != 1 || len(f.hooks.AllEvents()) != 1 {
		t.Fatal("duplicate processed again")
	}
}

// A mismatch at another connection claims the key only there, so the
// payment's own callback is still applied; a retry of the foreign one is a
// duplicate at its connection.
func TestProcessMismatchDoesNotBlockOwnConnection(t *testing.T) {
	f := newWebhookFixture(t)
	other := f.addConnection(t, "xendit-b")
	cb := paid(f.payment.ExternalID, 5000)
	err := f.process(t, other, cb)
	var mismatch *ConnectionMismatchError
	if !errors.As(err, &mismatch) || mismatch.ConnectionID != other.ID || *mismatch.PaymentConnectionID != f.conn.ID {
		t.Fatalf("err = %v, want a connection mismatch", err)
	}
	if err := f.process(t, f.conn, cb); err != nil {
		t.Fatalf("own callback: %v", err)
	}
	if f.paymentStatus(t, f.payment) != domain.PaymentStatusPaid || len(f.reports.All()) != 1 {
		t.Fatal("own callback not applied")
	}
	if err := f.process(t, other, cb); err != nil {
		t.Fatalf("retry of the foreign callback: %v", err)
	}
	results := []domain.InboundResult{}
	for _, r := range f.hooks.AllInbound() {
		results = append(results, r.Result)
	}
	if !reflect.DeepEqual(results, []domain.InboundResult{domain.InboundMismatch, domain.InboundApplied, domain.InboundDuplicate}) {
		t.Fatalf("results = %v", results)
	}
}

// A payment made before connections matches no connection.
func TestProcessPaymentWithoutConnectionIsMismatch(t *testing.T) {
	f := newWebhookFixture(t)
	legacy := f.addPayment(t, f.app, nil, "acme:legacy", 5000)
	if err := f.process(t, f.conn, paid(legacy.ExternalID, 5000)); !errors.Is(err, ErrCallbackMismatch) {
		t.Fatalf("err = %v", err)
	}
	if f.paymentStatus(t, legacy) != domain.PaymentStatusPending {
		t.Fatal("applied")
	}
}

// Regression: a failure after the claim rolls the claim back,
// leaves one error row, and the gateway's retry is processed in full.
func TestProcessRollbackReleasesClaim(t *testing.T) {
	f := newWebhookFixture(t)
	f.addEndpoint(t, f.app, CreateWebhookEndpointInput{})
	f.reports.UpsertErr = errors.New("reports down")
	cb := paid(f.payment.ExternalID, 5000)
	if err := f.process(t, f.conn, cb); err == nil {
		t.Fatal("expected the first attempt to fail")
	}
	rows := f.hooks.AllInbound()
	if len(rows) != 1 || rows[0].Result != domain.InboundError || rows[0].ResultDetail != "processing failed" ||
		strings.Contains(rows[0].ResultDetail, "reports") {
		t.Fatalf("rows = %+v", rows)
	}
	if f.paymentStatus(t, f.payment) != domain.PaymentStatusPending || len(f.hooks.AllEvents()) != 0 || len(f.hooks.AllDeliveries()) != 0 {
		t.Fatal("failed attempt left writes")
	}

	f.reports.UpsertErr = nil
	if err := f.process(t, f.conn, cb); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if f.paymentStatus(t, f.payment) != domain.PaymentStatusPaid || len(f.reports.All()) != 1 ||
		len(f.hooks.AllDeliveries()) != 1 || len(f.enq.calls) != 1 || f.lastInbound(t).Result != domain.InboundApplied {
		t.Fatal("retry not processed in full")
	}
}

// An unknown reference rolls back too, so the key is not burned if the
// payment row appears later.
func TestProcessUnknownReferenceCanBeRetried(t *testing.T) {
	f := newWebhookFixture(t)
	cb := paid("kp_pay_late", 5000)
	if err := f.process(t, f.conn, cb); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	late := f.addPayment(t, f.app, f.conn, "kp_pay_late", 5000)
	if err := f.process(t, f.conn, cb); err != nil {
		t.Fatal(err)
	}
	if f.paymentStatus(t, late) != domain.PaymentStatusPaid {
		t.Fatal("retry not applied")
	}
}

// When the queue is down after commit the deliveries stay for the sweeper and
// the caller gets ErrForwardNotQueued, so it can still ack.
func TestProcessEnqueueFailureKeepsDeliveries(t *testing.T) {
	f := newWebhookFixture(t)
	f.addEndpoint(t, f.app, CreateWebhookEndpointInput{})
	f.addEndpoint(t, f.app, CreateWebhookEndpointInput{})
	f.enq.err = errors.New("redis down")
	if err := f.process(t, f.conn, paid(f.payment.ExternalID, 5000)); !errors.Is(err, ErrForwardNotQueued) {
		t.Fatalf("err = %v", err)
	}
	if len(f.hooks.AllDeliveries()) != 2 || f.lastInbound(t).Result != domain.InboundApplied {
		t.Fatal("deliveries not committed")
	}
}

// Regression: callbacks arrive out of order. A late PAID after SETTLED is
// stale: no status change, no payment.paid event.
func TestProcessLatePaidAfterSettledIsStale(t *testing.T) {
	f := newWebhookFixture(t)
	f.addEndpoint(t, f.app, CreateWebhookEndpointInput{})
	settled := xenditCallback{ID: "inv_1", ExternalID: f.payment.ExternalID, Status: "SETTLED", Amount: 5000}
	if err := f.process(t, f.conn, settled); err != nil {
		t.Fatal(err)
	}
	if err := f.process(t, f.conn, paid(f.payment.ExternalID, 5000)); err != nil {
		t.Fatal(err)
	}
	if f.paymentStatus(t, f.payment) != domain.PaymentStatusSettled || f.lastInbound(t).Result != domain.InboundStale {
		t.Fatal("late PAID applied")
	}
	ds := f.hooks.AllDeliveries()
	if len(ds) != 1 || ds[0].EventType != "payment.settled" {
		t.Fatalf("deliveries = %+v", ds)
	}
}

// A payment captured after the checkout expired is still applied and sent.
func TestProcessPaidAfterExpiredIsApplied(t *testing.T) {
	f := newWebhookFixture(t)
	f.addEndpoint(t, f.app, CreateWebhookEndpointInput{})
	for _, st := range []string{"EXPIRED", "PAID"} {
		cb := xenditCallback{ID: "inv_1", ExternalID: f.payment.ExternalID, Status: st, Amount: 5000}
		if err := f.process(t, f.conn, cb); err != nil {
			t.Fatalf("%s: %v", st, err)
		}
	}
	ds := f.hooks.AllDeliveries()
	if f.paymentStatus(t, f.payment) != domain.PaymentStatusPaid || len(ds) != 2 || ds[1].EventType != "payment.paid" {
		t.Fatalf("deliveries = %+v", ds)
	}
}

// An unknown gateway status after PAID is recorded unmapped and never
// moves the payment.
func TestProcessUnknownStatusIsUnmapped(t *testing.T) {
	f := newWebhookFixture(t)
	for _, st := range []string{"PAID", "SOMETHING_NEW"} {
		cb := xenditCallback{ID: "inv_1", ExternalID: f.payment.ExternalID, Status: st, Amount: 5000}
		if err := f.process(t, f.conn, cb); err != nil {
			t.Fatalf("%s: %v", st, err)
		}
	}
	if f.paymentStatus(t, f.payment) != domain.PaymentStatusPaid || f.lastInbound(t).Result != domain.InboundUnmapped {
		t.Fatal("unknown status applied")
	}
	if len(f.hooks.AllEvents()) != 1 {
		t.Fatal("unknown status created an event")
	}
}

func TestProcessSettledRecordsSettlement(t *testing.T) {
	f := newWebhookFixture(t)
	for _, st := range []string{"PAID", "SETTLED"} {
		cb := xenditCallback{ID: "inv_1", ExternalID: f.payment.ExternalID, Status: st, Amount: 5000, Fee: i64(100),
			SettlementDate: "2026-01-02T03:04:05Z"}
		if err := f.process(t, f.conn, cb); err != nil {
			t.Fatalf("%s: %v", st, err)
		}
	}
	reports := f.reports.All()
	want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if len(reports) != 1 || reports[0].SettledAt == nil || !reports[0].SettledAt.Equal(want) || *reports[0].Fee != 100 ||
		reports[0].Status != domain.PaymentStatusSettled {
		t.Fatalf("reports = %+v", reports)
	}
	var data domain.PaymentEventData
	if err := json.Unmarshal(f.hooks.AllEvents()[1].Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Status != domain.PaymentStatusSettled || data.SettledAt == nil || !data.SettledAt.Equal(want) || data.PaidAt == nil {
		t.Fatalf("settled event data = %+v", data)
	}
}

// A disabled connection still applies its callbacks.
func TestProcessDisabledConnectionStillApplies(t *testing.T) {
	f := newWebhookFixture(t)
	disabled := *f.conn
	disabled.Status = domain.ConnectionDisabled
	if err := f.process(t, &disabled, paid(f.payment.ExternalID, 5000)); err != nil {
		t.Fatal(err)
	}
	if f.paymentStatus(t, f.payment) != domain.PaymentStatusPaid {
		t.Fatal("not applied")
	}
}

func TestProcessMissingKeyIsInvalidPayload(t *testing.T) {
	f := newWebhookFixture(t)
	for _, cb := range []xenditCallback{{ExternalID: f.payment.ExternalID, Status: "PAID"}, {ID: "inv_1", Status: "PAID"}} {
		if err := f.svc.Process(context.Background(), f.conn, cb.event(), cb.raw()); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("err = %v", err)
		}
		if in := f.lastInbound(t); in.Result != domain.InboundInvalidPayload || !in.SignatureValid || in.EventKey != nil {
			t.Fatalf("inbound = %+v", in)
		}
	}
}

// A body that failed verification is stored cut to 4096 bytes and an
// unparsable but verified one to 64 KiB; smaller bodies are kept whole.
func TestRecordRejected(t *testing.T) {
	f := newWebhookFixture(t)
	big := bytes.Repeat([]byte("a"), 10*1024)
	huge := bytes.Repeat([]byte("b"), 100*1024)
	for _, c := range []struct {
		raw    []byte
		result domain.InboundResult
	}{
		{big, domain.InboundInvalidSignature}, {big, domain.InboundInvalidPayload},
		{[]byte(`{}`), domain.InboundInvalidSignature}, {huge, domain.InboundInvalidPayload},
	} {
		if err := f.svc.RecordRejected(context.Background(), f.conn, c.raw, c.result, "rejected"); err != nil {
			t.Fatal(err)
		}
	}
	rows := f.hooks.AllInbound()
	if len(rows) != 4 {
		t.Fatalf("rows = %d", len(rows))
	}
	if rows[0].SignatureValid || len(rows[0].RawBody) != 4096 || !rows[0].BodyTruncated || rows[0].Result != domain.InboundInvalidSignature {
		t.Fatalf("invalid signature row: valid=%v len=%d truncated=%v", rows[0].SignatureValid, len(rows[0].RawBody), rows[0].BodyTruncated)
	}
	if !rows[1].SignatureValid || len(rows[1].RawBody) != len(big) || rows[1].BodyTruncated {
		t.Fatalf("invalid payload row: valid=%v len=%d", rows[1].SignatureValid, len(rows[1].RawBody))
	}
	if rows[2].BodyTruncated || string(rows[2].RawBody) != `{}` {
		t.Fatalf("short body = %+v", rows[2])
	}
	if len(rows[3].RawBody) != domain.MaxUnappliedBodyBytes || !rows[3].BodyTruncated {
		t.Fatalf("huge invalid payload: len=%d truncated=%v", len(rows[3].RawBody), rows[3].BodyTruncated)
	}
}

// withCallbackLimits turns the rejected-callback limits on.
func (f *webhookFixture) withCallbackLimits(perIP, perConn int) *memstore.Failures {
	limiter := memstore.NewFailures(f.clock.Now)
	f.svc.limiter, f.svc.limits = limiter, CallbackLimits{MaxRejectedPerIP: perIP, MaxRejectedPerConnection: perConn, Window: 15 * time.Minute}
	return limiter
}

// Rejected callbacks count per sender and per connection; at a limit they
// are refused without a row, while accepted callbacks are untouched.
func TestRecordRejectedBlockedAfterLimit(t *testing.T) {
	f := newWebhookFixture(t)
	limiter := f.withCallbackLimits(10, 2)
	other := f.addConnection(t, "xendit-b")
	ctx := WithClientMeta(context.Background(), ClientMeta{IP: "203.0.113.9", RequestID: "req-1"})
	for i := 0; i < 2; i++ {
		if err := f.svc.RecordRejected(ctx, f.conn, []byte(`x`), domain.InboundInvalidSignature, "bad token"); err != nil {
			t.Fatal(err)
		}
	}
	if limiter.Value("ip:203.0.113.9") != 2 || limiter.Value("con:"+f.conn.PublicID) != 2 {
		t.Fatalf("counted ip %d, connection %d", limiter.Value("ip:203.0.113.9"), limiter.Value("con:"+f.conn.PublicID))
	}
	err := f.svc.RecordRejected(ctx, f.conn, []byte(`x`), domain.InboundInvalidSignature, "bad token")
	var coded *domain.Error
	if !errors.As(err, &coded) || coded.Code != domain.CodeRateLimited || coded.Details["retry_after_seconds"].(int) < 1 {
		t.Fatalf("over the limit: %v", err)
	}
	if n := len(f.hooks.AllInbound()); n != 2 {
		t.Fatalf("rows = %d, the refused callback was recorded", n)
	}
	// The connection's limit is its own: another connection from the same
	// sender is still recorded, and a missing reference goes the same way.
	if err := f.svc.RecordRejected(ctx, other, []byte(`x`), domain.InboundInvalidPayload, "bad body"); err != nil {
		t.Fatal(err)
	}
	unkeyed := xenditCallback{ID: "", ExternalID: "", Status: "PAID", Amount: 5000, Currency: "IDR"}
	if err := f.process(t, f.conn, unkeyed); !errors.Is(err, domain.ErrRateLimited) {
		t.Fatalf("missing key on a blocked connection: %v", err)
	}
	// Accepted callbacks are never limited.
	if err := f.svc.Process(ctx, f.conn, paid(f.payment.ExternalID, 5000).event(), paid(f.payment.ExternalID, 5000).raw()); err != nil {
		t.Fatalf("accepted callback on a blocked connection: %v", err)
	}
}

// A limiter that does not answer neither blocks nor loses the record: the
// gateway's retries matter more than the limit.
func TestRecordRejectedLimiterDownStillRecords(t *testing.T) {
	f := newWebhookFixture(t)
	limiter := f.withCallbackLimits(1, 1)
	limiter.Err = errors.New("redis down")
	for i := 0; i < 3; i++ {
		if err := f.svc.RecordRejected(context.Background(), f.conn, []byte(`x`), domain.InboundInvalidSignature, "bad token"); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(f.hooks.AllInbound()); n != 3 {
		t.Fatalf("rows = %d", n)
	}
}

// Recording is best effort: a store failure is logged, not returned.
func TestRecordFailureIsNotReturned(t *testing.T) {
	f := newWebhookFixture(t)
	cb := paid(f.payment.ExternalID, 5000)
	if err := f.process(t, f.conn, cb); err != nil {
		t.Fatal(err)
	}
	f.hooks.InboundCreateErr = errors.New("db down")
	if err := f.process(t, f.conn, cb); err != nil {
		t.Fatalf("duplicate with a failing log: %v", err)
	}
	if err := f.svc.RecordRejected(context.Background(), f.conn, []byte(`x`), domain.InboundInvalidPayload, "bad body"); err != nil {
		t.Fatal(err)
	}
}

// A successful callback writes one report; SETTLED updates it in
// place; a repeated callback changes nothing.
func TestProcessWritesPaymentReport(t *testing.T) {
	f := newWebhookFixture(t)
	cb := paid(f.payment.ExternalID, 5000)
	cb.Fee, cb.PaidAt = i64(150), "2026-10-29T08:00:00Z"
	if err := f.process(t, f.conn, cb); err != nil {
		t.Fatal(err)
	}
	reports := f.reports.All()
	paidAt := time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC)
	if len(reports) != 1 {
		t.Fatalf("reports = %+v", reports)
	}
	r := reports[0]
	if r.PaymentID != f.payment.ID || r.AppID != f.app.ID || r.ConnectionID == nil || *r.ConnectionID != f.conn.ID ||
		r.Currency != "IDR" || r.Gross != 5000 || *r.Fee != 150 || *r.Net != 4850 || r.Status != domain.PaymentStatusPaid ||
		!r.PaidAt.Equal(paidAt) || r.SettledAt != nil {
		t.Fatalf("report = %+v", r)
	}

	settled := cb
	settled.Status, settled.SettlementDate = "SETTLED", "2026-10-30T01:00:00Z"
	if err := f.process(t, f.conn, settled); err != nil {
		t.Fatal(err)
	}
	if err := f.process(t, f.conn, cb); err != nil {
		t.Fatal(err)
	}
	reports = f.reports.All()
	want := time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)
	if len(reports) != 1 || reports[0].Status != domain.PaymentStatusSettled || reports[0].SettledAt == nil ||
		!reports[0].SettledAt.Equal(want) || !reports[0].PaidAt.Equal(paidAt) || *reports[0].Fee != 150 {
		t.Fatalf("reports after SETTLED = %+v", reports)
	}
}

// No fee in the callback is an unknown fee, filled once by a
// later callback that has one; 0 is a known fee.
func TestProcessReportFee(t *testing.T) {
	f := newWebhookFixture(t)
	if err := f.process(t, f.conn, paid(f.payment.ExternalID, 5000)); err != nil {
		t.Fatal(err)
	}
	if r := f.reports.All()[0]; r.Fee != nil || r.Net != nil {
		t.Fatalf("fee = %v net = %v, want unknown", r.Fee, r.Net)
	}
	settled := paid(f.payment.ExternalID, 5000)
	settled.Status, settled.Fee = "SETTLED", i64(99)
	if err := f.process(t, f.conn, settled); err != nil {
		t.Fatal(err)
	}
	if r := f.reports.All()[0]; r.Fee == nil || *r.Fee != 99 || *r.Net != 4901 {
		t.Fatalf("fee = %v net = %v, want 99 / 4901", r.Fee, r.Net)
	}

	zero := f.addPayment(t, f.app, f.conn, "acme:order-zero", 7000)
	cb := paid(zero.ExternalID, 7000)
	cb.ID, cb.Fee = "inv_zero", i64(0)
	if err := f.process(t, f.conn, cb); err != nil {
		t.Fatal(err)
	}
	for _, r := range f.reports.All() {
		if r.PaymentID == zero.ID && (r.Fee == nil || *r.Fee != 0 || *r.Net != 7000) {
			t.Fatalf("zero fee report = %+v", r)
		}
	}

	// A negative fee would break the report's constraints and with them the
	// whole callback, so the callback is applied with the fee unknown.
	negative := f.addPayment(t, f.app, f.conn, "acme:order-negative", 6000)
	cb = paid(negative.ExternalID, 6000)
	cb.ID, cb.Fee = "inv_negative", i64(-5)
	if err := f.process(t, f.conn, cb); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range f.reports.All() {
		if r.PaymentID != negative.ID {
			continue
		}
		found = true
		if r.Fee != nil || r.Net != nil || r.Gross != 6000 || r.Status != domain.PaymentStatusPaid {
			t.Fatalf("negative fee report = %+v", r)
		}
	}
	if !found {
		t.Fatal("negative fee callback wrote no report")
	}
}

// paid_amount 0 falls back to the payment's amount; a SETTLED callback
// for a payment never seen as PAID still gets a paid time.
func TestProcessReportFallbacks(t *testing.T) {
	f := newWebhookFixture(t)
	cb := xenditCallback{ID: "inv_1", ExternalID: f.payment.ExternalID, Status: "SETTLED", Amount: 5000, Currency: "IDR",
		SettlementDate: "2026-10-30T01:00:00Z"}
	if err := f.process(t, f.conn, cb); err != nil {
		t.Fatal(err)
	}
	r := f.reports.All()[0]
	settledAt := time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)
	if r.Gross != 5000 || r.Status != domain.PaymentStatusSettled || !r.PaidAt.Equal(settledAt) || !r.SettledAt.Equal(settledAt) {
		t.Fatalf("report = %+v", r)
	}
}

// A late success after EXPIRED is captured money and gets a
// report; a failed or expired callback gets none.
func TestProcessReportOnlyForSuccess(t *testing.T) {
	f := newWebhookFixture(t)
	expired := paid(f.payment.ExternalID, 5000)
	expired.Status = "EXPIRED"
	if err := f.process(t, f.conn, expired); err != nil {
		t.Fatal(err)
	}
	if len(f.reports.All()) != 0 {
		t.Fatal("expired payment has a report")
	}
	late := paid(f.payment.ExternalID, 5000)
	late.ID = "inv_late"
	if err := f.process(t, f.conn, late); err != nil {
		t.Fatal(err)
	}
	if reports := f.reports.All(); len(reports) != 1 || reports[0].Status != domain.PaymentStatusPaid {
		t.Fatalf("reports = %+v", reports)
	}
}
