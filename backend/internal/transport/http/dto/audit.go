package dto

import (
	"encoding/json"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// AuditActor is who made an audited change.
type AuditActor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// AuditTarget is the entity an audited change touched.
type AuditTarget struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// AuditLog is one entry of GET /admin/audit-logs. Its id is the public aud_
// id; the internal row id is never exposed.
type AuditLog struct {
	ID         string          `json:"id"`
	OccurredAt time.Time       `json:"occurred_at"`
	Actor      AuditActor      `json:"actor"`
	Action     string          `json:"action"`
	Target     AuditTarget     `json:"target"`
	Before     json.RawMessage `json:"before"`
	After      json.RawMessage `json:"after"`
	IP         string          `json:"ip"`
	UserAgent  string          `json:"user_agent"`
	RequestID  string          `json:"request_id"`
}

// AuditLogListResponse is the response for GET /admin/audit-logs.
type AuditLogListResponse struct {
	Data       []AuditLog `json:"data"`
	NextCursor *string    `json:"next_cursor"`
}

// AuditLogListFromDomain converts a page of entries. An empty page encodes
// data as [] and the last page encodes next_cursor as null.
func AuditLogListFromDomain(page domain.PageResult[domain.AuditLog]) AuditLogListResponse {
	out := AuditLogListResponse{Data: make([]AuditLog, 0, len(page.Items))}
	for _, l := range page.Items {
		out.Data = append(out.Data, AuditLog{
			ID:         l.PublicID,
			OccurredAt: l.OccurredAt.UTC(),
			Actor:      AuditActor{Type: l.Actor.Type, ID: l.Actor.ID},
			Action:     l.Action,
			Target:     AuditTarget{Type: l.Target.Type, ID: l.Target.ID},
			Before:     nullIfEmpty(l.Before),
			After:      nullIfEmpty(l.After),
			IP:         l.Actor.IP,
			UserAgent:  l.Actor.UserAgent,
			RequestID:  l.Actor.RequestID,
		})
	}
	if page.NextCursor != "" {
		next := page.NextCursor
		out.NextCursor = &next
	}
	return out
}

// nullIfEmpty keeps an absent snapshot as JSON null; an empty RawMessage
// would otherwise make encoding fail.
func nullIfEmpty(b json.RawMessage) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return b
}
