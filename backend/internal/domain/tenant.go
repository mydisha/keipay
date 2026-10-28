package domain

import "time"

// Tenant is a client application that consumes KeiPay.
type Tenant struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name"`
	Slug               string    `json:"slug"`
	APIKey             string    `json:"api_key"`
	APISecretEnc       string    `json:"-"`
	WebhookURL         string    `json:"webhook_url"`
	RedirectSuccessURL string    `json:"redirect_success_url"`
	RedirectFailureURL string    `json:"redirect_failure_url"`
	Active             bool      `json:"active"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}
