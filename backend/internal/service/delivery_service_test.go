package service

import (
	"context"
	"errors"
	"testing"

	"github.com/mydisha/keipay/backend/internal/domain"
)

type deliveryRepo struct {
	domain.WebhookRepository
	byID map[int64]domain.WebhookDelivery
}

func (r *deliveryRepo) GetDelivery(_ context.Context, id int64) (*domain.WebhookDelivery, error) {
	d, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &d, nil
}

type recordingReplayer struct {
	ids []int64
	err error
}

func (r *recordingReplayer) ReplayWebhookForward(_ context.Context, id int64) error {
	if r.err != nil {
		return r.err
	}
	r.ids = append(r.ids, id)
	return nil
}

func newDeliveryFixture() (*DeliveryService, *recordingReplayer, *memAuditRepo) {
	repo := &deliveryRepo{byID: map[int64]domain.WebhookDelivery{
		5: {ID: 5, Status: domain.WebhookDeliveryFailed, Attempts: 6},
	}}
	rep := &recordingReplayer{}
	audit := &memAuditRepo{}
	return NewDeliveryService(repo, rep, NewAuditService(audit)), rep, audit
}

func TestReplayRecordsEntry(t *testing.T) {
	svc, rep, audit := newDeliveryFixture()
	if err := svc.Replay(adminCtx(), 5); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(rep.ids) != 1 || rep.ids[0] != 5 {
		t.Fatalf("enqueued = %v, want [5]", rep.ids)
	}
	if len(audit.logs) != 1 {
		t.Fatalf("got %d entries, want 1", len(audit.logs))
	}
	l := audit.logs[0]
	if l.Action != "webhook_delivery.replay" || l.Target != (domain.AuditTarget{Type: "webhook_delivery", ID: "5"}) {
		t.Fatalf("action/target = %s %+v", l.Action, l.Target)
	}
	assertJSON(t, "before", l.Before, map[string]any{"status": "FAILED", "attempts": 6})
	assertJSON(t, "after", l.After, nil)
}

func TestReplayMissingDeliveryIsNotFound(t *testing.T) {
	svc, rep, audit := newDeliveryFixture()
	if err := svc.Replay(adminCtx(), 99); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if len(rep.ids) != 0 || len(audit.logs) != 0 {
		t.Fatalf("missing delivery enqueued=%v entries=%d", rep.ids, len(audit.logs))
	}
}

func TestReplayEnqueueFailureRecordsNothing(t *testing.T) {
	svc, rep, audit := newDeliveryFixture()
	rep.err = errors.New("redis down")
	err := svc.Replay(adminCtx(), 5)
	if err == nil || errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want an internal error", err)
	}
	if len(audit.logs) != 0 {
		t.Fatalf("failed enqueue recorded %d entries", len(audit.logs))
	}
}

func TestReplayWithoutActorFails(t *testing.T) {
	svc, _, audit := newDeliveryFixture()
	if err := svc.Replay(context.Background(), 5); !errors.Is(err, ErrNoActor) {
		t.Fatalf("err = %v, want ErrNoActor", err)
	}
	if len(audit.logs) != 0 {
		t.Fatal("entry written without actor")
	}
}
