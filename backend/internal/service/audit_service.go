package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
)

// Audit actions recorded today. Actions are "{target_type}.{verb}".
const (
	ActionTenantCreate          = "tenant.create"
	ActionTenantUpdate          = "tenant.update"
	ActionTenantRotate          = "tenant.rotate_credentials"
	ActionWebhookDeliveryReplay = "webhook_delivery.replay"
	ActionUserSetup             = "user.setup"
	ActionUserCreate            = "user.create"
	ActionUserUpdate            = "user.update"
	ActionUserDisable           = "user.disable"
	ActionUserEnable            = "user.enable"
	ActionUserResetPassword     = "user.reset_password"
	ActionUserChangePassword    = "user.change_password"
	ActionUserLogin             = "user.login"
	ActionUserLoginFailed       = "user.login_failed"
	ActionUserLogout            = "user.logout"
)

// Audit target types.
const (
	TargetTenant          = "tenant"
	TargetUser            = "user"
	TargetWebhookDelivery = "webhook_delivery"
)

// Auditor records an admin mutation. It must be called with the ctx of the
// mutation's transaction so the entry commits or rolls back with it.
type Auditor interface {
	Record(ctx context.Context, action string, target domain.AuditTarget, before, after any) error
}

// ErrNoActor means a mutation ran without an actor in its context. Audited
// mutations fail closed rather than write an anonymous entry.
var ErrNoActor = errors.New("audit: no actor in context")

const redacted = "[REDACTED]"

// AuditService records and lists audit log entries.
type AuditService struct {
	repo domain.AuditRepository
	now  func() time.Time
}

// NewAuditService constructs an AuditService.
func NewAuditService(repo domain.AuditRepository) *AuditService {
	return &AuditService{repo: repo, now: time.Now}
}

// Record writes one entry for the actor in ctx. before and after are
// marshalled to JSON and secret-looking keys are redacted; nil means null.
func (s *AuditService) Record(ctx context.Context, action string, target domain.AuditTarget, before, after any) error {
	actor, ok := domain.ActorFromContext(ctx)
	if !ok {
		return ErrNoActor
	}
	if !validActorType(actor.Type) || actor.ID == "" {
		return fmt.Errorf("audit: invalid actor %q:%q", actor.Type, actor.ID)
	}
	b, err := snapshot(before)
	if err != nil {
		return fmt.Errorf("audit before snapshot: %w", err)
	}
	a, err := snapshot(after)
	if err != nil {
		return fmt.Errorf("audit after snapshot: %w", err)
	}
	return s.repo.Create(ctx, &domain.AuditLog{
		PublicID:   id.New(id.Audit),
		OccurredAt: s.now().UTC(),
		Actor:      actor,
		Action:     action,
		Target:     target,
		Before:     b,
		After:      a,
	})
}

func validActorType(t string) bool {
	switch t {
	case domain.ActorAdmin, domain.ActorSystem, domain.ActorAnonymous:
		return true
	default:
		return false
	}
}

// List returns one page of entries, newest first.
func (s *AuditService) List(ctx context.Context, req domain.PageRequest) (domain.PageResult[domain.AuditLog], error) {
	p, err := req.Page()
	if err != nil {
		return domain.PageResult[domain.AuditLog]{}, err
	}
	return s.repo.List(ctx, p)
}

// snapshot marshals v and redacts it. It returns nil for a nil value so the
// column stores SQL NULL.
func snapshot(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return nil, err
	}
	if tree == nil {
		return nil, nil
	}
	return json.Marshal(redact(tree))
}

// redact is the second line of defence behind the explicit snapshot structs:
// it masks values under secret-looking keys at any depth.
func redact(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if secretKey(k) && !harmless(val) {
				t[k] = redacted
				continue
			}
			t[k] = redact(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = redact(val)
		}
		return t
	default:
		return v
	}
}

func secretKey(k string) bool {
	k = strings.ToLower(k)
	return strings.Contains(k, "password") || strings.Contains(k, "secret") ||
		strings.Contains(k, "token") || strings.HasSuffix(k, "_enc")
}

// harmless reports whether a value cannot carry a secret. Flags such as
// must_change_password are part of the audited change and stay visible.
func harmless(v any) bool {
	switch v.(type) {
	case nil, bool:
		return true
	default:
		return false
	}
}
