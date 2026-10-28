package domain

import (
	"context"
	"encoding/json"
	"time"
)

// Actor types recorded in the audit log.
const (
	ActorAdmin     = "admin"
	ActorSystem    = "system"
	ActorAnonymous = "anonymous"
)

// Actor is who performed a mutation. Admin and anonymous actors carry the
// request origin; system actors (seed, CLI, setup, jobs) leave IP, UserAgent
// and RequestID empty.
type Actor struct {
	Type      string
	ID        string
	IP        string
	UserAgent string
	RequestID string
}

// SystemActor is the actor for non-interactive work, named by component
// (e.g. "seed", "cli").
func SystemActor(component string) Actor {
	return Actor{Type: ActorSystem, ID: component}
}

// AnonymousActor is the actor of an unauthenticated request, such as a failed
// sign-in.
func AnonymousActor(ip, userAgent, requestID string) Actor {
	return Actor{Type: ActorAnonymous, ID: ActorAnonymous, IP: ip, UserAgent: userAgent, RequestID: requestID}
}

// UserActor is the actor of a request made by a signed-in console user.
func UserActor(userPublicID, ip, userAgent, requestID string) Actor {
	return Actor{Type: ActorAdmin, ID: userPublicID, IP: ip, UserAgent: userAgent, RequestID: requestID}
}

type actorKey struct{}

// WithActor returns a copy of ctx carrying the actor for audit records.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// ActorFromContext returns the actor stored by WithActor.
func ActorFromContext(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(Actor)
	return a, ok
}

// AuditTarget identifies the entity a mutation changed.
type AuditTarget struct {
	Type string
	ID   string
}

// AuditLog is one recorded admin mutation. Before and After are JSON snapshots
// without secrets; nil means null.
type AuditLog struct {
	ID         int64
	PublicID   string
	OccurredAt time.Time
	Actor      Actor
	Action     string
	Target     AuditTarget
	Before     json.RawMessage
	After      json.RawMessage
}

// AuditRepository persists audit log entries. Create joins the transaction
// carried by ctx, so the entry commits or rolls back with the mutation.
type AuditRepository interface {
	Create(ctx context.Context, l *AuditLog) error
	// List returns entries newest first (occurred_at, then public id).
	List(ctx context.Context, p Page) (PageResult[AuditLog], error)
}
