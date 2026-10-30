package dto

import (
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/provider"
	"github.com/mydisha/keipay/backend/internal/service"
)

// SchemaOption is one value of an enum field.
type SchemaOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// SchemaField is one form field of a provider schema.
type SchemaField struct {
	Key         string         `json:"key"`
	Type        string         `json:"type"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	Placeholder string         `json:"placeholder,omitempty"`
	Secret      bool           `json:"secret"`
	Required    bool           `json:"required"`
	Default     any            `json:"default"`
	Options     []SchemaOption `json:"options,omitempty"`
	MinLength   *int           `json:"min_length,omitempty"`
	MaxLength   *int           `json:"max_length,omitempty"`
	Pattern     string         `json:"pattern,omitempty"`
	PatternHint string         `json:"pattern_hint,omitempty"`
	Minimum     *int64         `json:"minimum,omitempty"`
	Maximum     *int64         `json:"maximum,omitempty"`
}

// ConfigSchema is an ordered list of fields.
type ConfigSchema struct {
	Fields []SchemaField `json:"fields"`
}

// ChannelInfo is a channel code with its display name.
type ChannelInfo struct {
	Code  domain.Channel `json:"code"`
	Label string         `json:"label"`
}

// ProviderCapabilities says what a provider can do.
type ProviderCapabilities struct {
	Channels            []ChannelInfo `json:"channels"`
	Currencies          []string      `json:"currencies"`
	CanRestrictChannels bool          `json:"can_restrict_channels"`
	CanCancel           bool          `json:"can_cancel"`
	ReportsFee          bool          `json:"reports_fee"`
	ListTransactions    bool          `json:"list_transactions"`
	ReadCheckout        bool          `json:"read_checkout"`
}

// ProviderWebhookSetup are the steps to paste a callback URL into the gateway.
type ProviderWebhookSetup struct {
	Instructions []string `json:"instructions"`
	DashboardURL string   `json:"dashboard_url,omitempty"`
}

// Provider describes one provider adapter for the console.
type Provider struct {
	Name             string               `json:"name"`
	DisplayName      string               `json:"display_name"`
	Description      string               `json:"description"`
	DocsURL          string               `json:"docs_url,omitempty"`
	CredentialSchema ConfigSchema         `json:"credential_schema"`
	SettingsSchema   ConfigSchema         `json:"settings_schema"`
	Capabilities     ProviderCapabilities `json:"capabilities"`
	WebhookSetup     ProviderWebhookSetup `json:"webhook_setup"`
}

// ProviderListResponse is the response for GET /admin/providers.
type ProviderListResponse struct {
	Data []Provider `json:"data"`
}

// ProviderFromDomain describes an adapter.
func ProviderFromDomain(p provider.Provider) Provider {
	d := p.Descriptor()
	caps := p.Capabilities()
	out := Provider{
		Name:             string(d.Name),
		DisplayName:      d.DisplayName,
		Description:      d.Description,
		DocsURL:          d.DocsURL,
		CredentialSchema: schemaFromDomain(p.CredentialSchema()),
		SettingsSchema:   schemaFromDomain(p.SettingsSchema()),
		Capabilities: ProviderCapabilities{
			Channels:            make([]ChannelInfo, 0, len(caps.Channels)),
			Currencies:          append([]string{}, caps.Currencies...),
			CanRestrictChannels: caps.CanRestrictChannels,
			CanCancel:           caps.CanCancel,
			ReportsFee:          caps.ReportsFee,
			ListTransactions:    caps.ListTransactions,
			ReadCheckout:        caps.ReadCheckout,
		},
		WebhookSetup: ProviderWebhookSetup{
			Instructions: append([]string{}, d.Webhook.Instructions...),
			DashboardURL: d.Webhook.DashboardURL,
		},
	}
	for _, ch := range caps.Channels {
		out.Capabilities.Channels = append(out.Capabilities.Channels, ChannelInfo{Code: ch, Label: ch.Label()})
	}
	return out
}

// ProviderListFromDomain describes every adapter, in the given order.
func ProviderListFromDomain(ps []provider.Provider) ProviderListResponse {
	out := ProviderListResponse{Data: make([]Provider, 0, len(ps))}
	for _, p := range ps {
		out.Data = append(out.Data, ProviderFromDomain(p))
	}
	return out
}

func schemaFromDomain(s provider.Schema) ConfigSchema {
	out := ConfigSchema{Fields: make([]SchemaField, 0, len(s.Fields))}
	for _, f := range s.Fields {
		sf := SchemaField{
			Key: f.Key, Type: string(f.Type), Label: f.Label, Description: f.Description, Placeholder: f.Placeholder,
			Secret: f.Secret, Required: f.Required, Default: f.Default,
			MinLength: f.MinLength, MaxLength: f.MaxLength, Pattern: f.Pattern, PatternHint: f.PatternHint,
			Minimum: f.Minimum, Maximum: f.Maximum,
		}
		if f.Secret {
			sf.Default = nil
		}
		for _, o := range f.Options {
			sf.Options = append(sf.Options, SchemaOption{Value: o.Value, Label: o.Label})
		}
		out.Fields = append(out.Fields, sf)
	}
	return out
}

// ConnectionSummary is the connection an app refers to.
type ConnectionSummary struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Provider    string                  `json:"provider"`
	Environment domain.Environment      `json:"environment"`
	Status      domain.ConnectionStatus `json:"status"`
}

// ConnectionSummaryFromDomain converts a summary; nil stays nil (JSON null).
func ConnectionSummaryFromDomain(s *domain.ConnectionSummary) *ConnectionSummary {
	if s == nil {
		return nil
	}
	return &ConnectionSummary{ID: s.PublicID, Name: s.Name, Provider: s.Provider, Environment: s.Environment, Status: s.Status}
}

// ConnectionListItem is a connection as lists show it: never credentials.
type ConnectionListItem struct {
	ID                 string                       `json:"id"`
	Name               string                       `json:"name"`
	Provider           string                       `json:"provider"`
	Environment        domain.Environment           `json:"environment"`
	Status             domain.ConnectionStatus      `json:"status"`
	LegacyWebhookAlias bool                         `json:"legacy_webhook_alias"`
	LastTestedAt       *time.Time                   `json:"last_tested_at"`
	LastTestStatus     *domain.ConnectionTestStatus `json:"last_test_status"`
	CreatedAt          time.Time                    `json:"created_at"`
	UpdatedAt          time.Time                    `json:"updated_at"`
}

// ConnectionListItemFromDomain converts a connection; timestamps in UTC.
func ConnectionListItemFromDomain(c *domain.Connection) ConnectionListItem {
	return ConnectionListItem{
		ID:                 c.PublicID,
		Name:               c.Name,
		Provider:           c.Provider,
		Environment:        c.Environment,
		Status:             c.Status,
		LegacyWebhookAlias: c.LegacyWebhookAlias,
		LastTestedAt:       utcPtr(c.LastTestedAt),
		LastTestStatus:     c.LastTestStatus,
		CreatedAt:          c.CreatedAt.UTC(),
		UpdatedAt:          c.UpdatedAt.UTC(),
	}
}

// CredentialHint shows what is stored for one credential: never the value.
type CredentialHint struct {
	Key   string  `json:"key"`
	Last4 *string `json:"last4"`
}

// ConnectionChannel is whether the connection offers one channel.
type ConnectionChannel struct {
	Channel domain.Channel `json:"channel"`
	Label   string         `json:"label"`
	Enabled bool           `json:"enabled"`
}

// Connection is a connection with everything except credential values.
type Connection struct {
	ConnectionListItem
	Credentials   []CredentialHint    `json:"credentials"`
	Settings      map[string]any      `json:"settings"`
	Channels      []ConnectionChannel `json:"channels"`
	WebhookURL    string              `json:"webhook_url"`
	LastTestError *string             `json:"last_test_error"`
}

// ConnectionFromService converts a connection detail.
func ConnectionFromService(d *service.ConnectionDetail) Connection {
	out := Connection{
		ConnectionListItem: ConnectionListItemFromDomain(d.Connection),
		Credentials:        make([]CredentialHint, 0, len(d.Credentials)),
		Settings:           d.Connection.Settings,
		Channels:           make([]ConnectionChannel, 0, len(d.Channels)),
		WebhookURL:         d.WebhookURL,
		LastTestError:      d.Connection.LastTestError,
	}
	if out.Settings == nil {
		out.Settings = map[string]any{}
	}
	for _, h := range d.Credentials {
		out.Credentials = append(out.Credentials, CredentialHint{Key: h.Key, Last4: h.Last4})
	}
	for _, ch := range d.Channels {
		out.Channels = append(out.Channels, ConnectionChannel{Channel: ch.Channel, Label: ch.Channel.Label(), Enabled: ch.Enabled})
	}
	return out
}

// ConnectionResponse wraps one connection.
type ConnectionResponse struct {
	Connection Connection `json:"connection"`
}

// ConnectionListResponse is the response for GET /admin/connections.
type ConnectionListResponse struct {
	Data       []ConnectionListItem `json:"data"`
	NextCursor *string              `json:"next_cursor"`
}

// ConnectionListFromDomain converts a page.
func ConnectionListFromDomain(page domain.PageResult[domain.Connection]) ConnectionListResponse {
	out := ConnectionListResponse{Data: make([]ConnectionListItem, 0, len(page.Items)), NextCursor: nextCursor(page.NextCursor)}
	for i := range page.Items {
		out.Data = append(out.Data, ConnectionListItemFromDomain(&page.Items[i]))
	}
	return out
}

// ConnectionTestResponse is the outcome of POST /admin/connections/{id}/test.
type ConnectionTestResponse struct {
	Result     domain.ConnectionTestStatus `json:"result"`
	Message    string                      `json:"message"`
	TestedAt   time.Time                   `json:"tested_at"`
	Connection Connection                  `json:"connection"`
}

// ConnectionTestFromService converts a test result.
func ConnectionTestFromService(r *service.ConnectionTestResult) ConnectionTestResponse {
	return ConnectionTestResponse{Result: r.Result, Message: r.Message, TestedAt: r.TestedAt.UTC(), Connection: ConnectionFromService(r.Detail)}
}

// ChannelChoice is one entry of PUT /admin/connections/{id}/channels.
type ChannelChoice struct {
	Channel string `json:"channel"`
	Enabled *bool  `json:"enabled" binding:"required"`
}

// ReplaceConnectionChannelsRequest is the body of PUT .../channels.
type ReplaceConnectionChannelsRequest struct {
	Channels []ChannelChoice `json:"channels" binding:"required,dive"`
}
