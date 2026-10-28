package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// memAuditRepo stores entries in memory and drops them when the fake
// transaction rolls back.
type memAuditRepo struct {
	logs []domain.AuditLog
	err  error
	page domain.Page
}

func (m *memAuditRepo) Create(ctx context.Context, l *domain.AuditLog) error {
	if m.err != nil {
		return m.err
	}
	m.logs = append(m.logs, *l)
	onRollback(ctx, func() { m.logs = m.logs[:len(m.logs)-1] })
	return nil
}

func (m *memAuditRepo) List(_ context.Context, p domain.Page) (domain.PageResult[domain.AuditLog], error) {
	m.page = p
	return domain.PageResult[domain.AuditLog]{Items: m.logs}, nil
}

func adminCtx() context.Context {
	return domain.WithActor(context.Background(), domain.Actor{
		Type: domain.ActorAdmin, ID: "admin", IP: "203.0.113.7", UserAgent: "test-agent", RequestID: "req-1",
	})
}

// assertJSON compares a stored snapshot with the expected value, both decoded
// into generic JSON so field order does not matter.
func assertJSON(t *testing.T, label string, got json.RawMessage, want any) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Fatalf("%s = %s, want null", label, got)
		}
		return
	}
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("%s: decode %s: %v", label, got, err)
	}
	wb, _ := json.Marshal(want)
	_ = json.Unmarshal(wb, &w)
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("%s = %s, want %s", label, got, wb)
	}
}

var errAuditDown = errors.New("audit insert failed")
