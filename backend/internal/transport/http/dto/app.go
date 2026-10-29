package dto

import (
	"encoding/json"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/service"
)

// CreateAppRequest is the request body for POST /admin/apps. Field rules are
// checked by the service so they match the contract exactly. WebhookURL is
// only read to refuse the retired field, whatever its value.
type CreateAppRequest struct {
	Name               string             `json:"name"`
	Slug               string             `json:"slug"`
	Environment        domain.Environment `json:"environment"`
	WebhookURL         json.RawMessage    `json:"webhook_url"`
	RedirectSuccessURL string             `json:"redirect_success_url"`
	RedirectFailureURL string             `json:"redirect_failure_url"`
	Metadata           json.RawMessage    `json:"metadata"`
}

// CreateAPIKeyRequest is the request body for POST /admin/apps/{id}/keys.
type CreateAPIKeyRequest struct {
	Label     string     `json:"label"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// App is an app as the API shows it. The internal id never leaves the server.
type App struct {
	ID                 string             `json:"id"`
	Slug               string             `json:"slug"`
	Name               string             `json:"name"`
	Environment        domain.Environment `json:"environment"`
	Status             domain.AppStatus   `json:"status"`
	Metadata           domain.Metadata    `json:"metadata"`
	RedirectSuccessURL string             `json:"redirect_success_url"`
	RedirectFailureURL string             `json:"redirect_failure_url"`
	DefaultConnection  *ConnectionSummary `json:"default_connection"`
	CreatedAt          time.Time          `json:"created_at"`
	UpdatedAt          time.Time          `json:"updated_at"`
}

// AppFromDomain converts an app; timestamps are sent in UTC.
func AppFromDomain(a *domain.App) App {
	return App{
		ID:                 a.PublicID,
		Slug:               a.Slug,
		Name:               a.Name,
		Environment:        a.Environment,
		Status:             a.Status,
		Metadata:           a.Metadata,
		RedirectSuccessURL: a.RedirectSuccessURL,
		RedirectFailureURL: a.RedirectFailureURL,
		DefaultConnection:  ConnectionSummaryFromDomain(a.DefaultConnection),
		CreatedAt:          a.CreatedAt.UTC(),
		UpdatedAt:          a.UpdatedAt.UTC(),
	}
}

// AppSummary is the app a payment, payment report or delivery belongs to.
type AppSummary struct {
	ID          string             `json:"id"`
	Slug        string             `json:"slug"`
	Name        string             `json:"name"`
	Environment domain.Environment `json:"environment"`
}

// AppSummaryFromDomain converts an app summary.
func AppSummaryFromDomain(s domain.AppSummary) AppSummary {
	return AppSummary{ID: s.PublicID, Slug: s.Slug, Name: s.Name, Environment: s.Environment}
}

// AppResponse wraps one app.
type AppResponse struct {
	App App `json:"app"`
}

// AppListResponse is the response for GET /admin/apps.
type AppListResponse struct {
	Data       []App   `json:"data"`
	NextCursor *string `json:"next_cursor"`
}

// AppListFromDomain converts a page; data is [] when empty and next_cursor
// null on the last page.
func AppListFromDomain(page domain.PageResult[domain.App]) AppListResponse {
	out := AppListResponse{Data: make([]App, 0, len(page.Items)), NextCursor: nextCursor(page.NextCursor)}
	for i := range page.Items {
		out.Data = append(out.Data, AppFromDomain(&page.Items[i]))
	}
	return out
}

// AppCreatedResponse carries a new app, its first key and the secret shown once.
type AppCreatedResponse struct {
	App       App    `json:"app"`
	Key       APIKey `json:"key"`
	APIKey    string `json:"api_key"`
	APISecret string `json:"api_secret"`
}

// AppCreatedFromService converts a created app.
func AppCreatedFromService(c *service.AppCreated, now time.Time) AppCreatedResponse {
	return AppCreatedResponse{
		App:       AppFromDomain(c.App),
		Key:       APIKeyFromDomain(c.App, c.Key, now),
		APIKey:    c.APIKey,
		APISecret: c.APISecret,
	}
}

// APIKey is an API key as the API shows it: never the key, its hash or secret.
type APIKey struct {
	ID          string              `json:"id"`
	AppID       string              `json:"app_id"`
	Label       string              `json:"label"`
	Environment domain.Environment  `json:"environment"`
	Prefix      string              `json:"prefix"`
	Last4       string              `json:"last4"`
	Status      domain.APIKeyStatus `json:"status"`
	LastUsedAt  *time.Time          `json:"last_used_at"`
	ExpiresAt   *time.Time          `json:"expires_at"`
	RevokedAt   *time.Time          `json:"revoked_at"`
	CreatedAt   time.Time           `json:"created_at"`
}

// APIKeyFromDomain converts a key of app; its status is derived at now.
func APIKeyFromDomain(app *domain.App, k *domain.APIKey, now time.Time) APIKey {
	return APIKey{
		ID:          k.PublicID,
		AppID:       app.PublicID,
		Label:       k.Label,
		Environment: k.Environment,
		Prefix:      k.Prefix,
		Last4:       k.Last4,
		Status:      k.Status(now),
		LastUsedAt:  utcPtr(k.LastUsedAt),
		ExpiresAt:   utcPtr(k.ExpiresAt),
		RevokedAt:   utcPtr(k.RevokedAt),
		CreatedAt:   k.CreatedAt.UTC(),
	}
}

// APIKeyResponse wraps one key.
type APIKeyResponse struct {
	Key APIKey `json:"key"`
}

// APIKeyListResponse is the response for GET /admin/apps/{id}/keys.
type APIKeyListResponse struct {
	Data       []APIKey `json:"data"`
	NextCursor *string  `json:"next_cursor"`
}

// APIKeyListFromDomain converts a page of app's keys.
func APIKeyListFromDomain(app *domain.App, page domain.PageResult[domain.APIKey], now time.Time) APIKeyListResponse {
	out := APIKeyListResponse{Data: make([]APIKey, 0, len(page.Items)), NextCursor: nextCursor(page.NextCursor)}
	for i := range page.Items {
		out.Data = append(out.Data, APIKeyFromDomain(app, &page.Items[i], now))
	}
	return out
}

// APIKeyCreatedResponse carries a new key and its secret, shown once.
type APIKeyCreatedResponse struct {
	Key       APIKey `json:"key"`
	APIKey    string `json:"api_key"`
	APISecret string `json:"api_secret"`
}

// APIKeyCreatedFromService converts a created or rotated key.
func APIKeyCreatedFromService(c *service.KeyCreated, now time.Time) APIKeyCreatedResponse {
	return APIKeyCreatedResponse{Key: APIKeyFromDomain(c.App, c.Key, now), APIKey: c.APIKey, APISecret: c.APISecret}
}

// nextCursor is the wire form of a page's cursor: null on the last page.
func nextCursor(c string) *string {
	if c == "" {
		return nil
	}
	return &c
}
