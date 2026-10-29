package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/pkg/keygen"
)

// SecretCipher encrypts/decrypts secrets at rest. API and webhook secrets are
// HMAC keys, so they must be recoverable (encrypted, not hashed).
type SecretCipher interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(ciphertext string) (string, error)
}

// DefaultKeyLabel is the label of the key every new app starts with.
const DefaultKeyLabel = "Default"

// ErrKeyEnvironmentMismatch is the cause inside the unknown_api_key error
// returned when a key's environment disagrees with its app or its own prefix.
// The caller sees an unknown key; the server logs a security event.
var ErrKeyEnvironmentMismatch = errors.New("api key environment mismatch")

var (
	errTooManyKeys = &domain.Error{Code: domain.CodeValidationFailed,
		Message: fmt.Sprintf("an app can have at most %d usable API keys", domain.MaxUsableAPIKeys), Err: domain.ErrInvalidInput}
	errRotateRevoked = &domain.Error{Code: domain.CodeValidationFailed,
		Message: "a revoked API key cannot be rotated", Err: domain.ErrInvalidInput}
	errRotateExpired = &domain.Error{Code: domain.CodeValidationFailed,
		Message: "an expired API key cannot be rotated", Err: domain.ErrInvalidInput}
)

// AppService manages apps and their API keys, and resolves /v1 keys. Every
// mutation and its audit entries commit in one transaction.
type AppService struct {
	apps   domain.AppRepository
	keys   domain.APIKeyRepository
	conns  domain.ConnectionRepository
	cipher SecretCipher
	tx     domain.TxManager
	audit  Auditor
	now    func() time.Time
}

// NewAppService constructs an AppService. conns resolves default connections.
func NewAppService(apps domain.AppRepository, keys domain.APIKeyRepository, conns domain.ConnectionRepository,
	cipher SecretCipher, tx domain.TxManager, audit Auditor) *AppService {
	return &AppService{apps: apps, keys: keys, conns: conns, cipher: cipher, tx: tx, audit: audit, now: time.Now}
}

// CreateAppInput is a new app. Metadata is raw JSON; omitted or null is {}.
// WebhookURL records that the retired webhook_url field was sent, which is
// refused: webhook URLs are webhook endpoints now.
type CreateAppInput struct {
	Name               string
	Slug               string
	Environment        domain.Environment
	WebhookURL         bool
	RedirectSuccessURL string
	RedirectFailureURL string
	Metadata           json.RawMessage
}

// UpdateAppInput holds the fields present in a PATCH body; nil means absent.
// Slug, Environment and WebhookURL only record that the field was sent, which
// is refused. DefaultConnectionSet records that default_connection_id was
// sent; DefaultConnectionID is then the con_ id, or nil to clear it.
type UpdateAppInput struct {
	Name                 *string
	WebhookURL           bool
	RedirectSuccessURL   *string
	RedirectFailureURL   *string
	Metadata             json.RawMessage
	Slug                 bool
	Environment          bool
	DefaultConnectionSet bool
	DefaultConnectionID  *string
}

// AppCreated is a new app with its first key and the one-time secrets.
type AppCreated struct {
	App       *domain.App
	Key       *domain.APIKey
	APIKey    string
	APISecret string
}

// KeyCreated is a new key of app with its one-time key and secret.
type KeyCreated struct {
	App       *domain.App
	Key       *domain.APIKey
	APIKey    string
	APISecret string
}

// appSnapshot is the audited view of an app. It never holds a secret.
type appSnapshot struct {
	ID                 string             `json:"id"`
	Slug               string             `json:"slug"`
	Name               string             `json:"name"`
	Environment        domain.Environment `json:"environment"`
	Status             domain.AppStatus   `json:"status"`
	RedirectSuccessURL string             `json:"redirect_success_url"`
	RedirectFailureURL string             `json:"redirect_failure_url"`
	Metadata           domain.Metadata    `json:"metadata"`
}

func snapshotApp(a *domain.App) appSnapshot {
	return appSnapshot{
		ID: a.PublicID, Slug: a.Slug, Name: a.Name, Environment: a.Environment, Status: a.Status,
		RedirectSuccessURL: a.RedirectSuccessURL,
		RedirectFailureURL: a.RedirectFailureURL, Metadata: a.Metadata,
	}
}

// keySnapshot is the audited view of an API key: never the key, its hash or
// its secret.
type keySnapshot struct {
	ID          string             `json:"id"`
	AppID       string             `json:"app_id"`
	Label       string             `json:"label"`
	Environment domain.Environment `json:"environment"`
	Prefix      string             `json:"prefix"`
	Last4       string             `json:"last4"`
	ExpiresAt   *time.Time         `json:"expires_at"`
	RevokedAt   *time.Time         `json:"revoked_at"`
}

func snapshotKey(app *domain.App, k *domain.APIKey) keySnapshot {
	return keySnapshot{
		ID: k.PublicID, AppID: app.PublicID, Label: k.Label, Environment: k.Environment,
		Prefix: k.Prefix, Last4: k.Last4, ExpiresAt: k.ExpiresAt, RevokedAt: k.RevokedAt,
	}
}

type statusSnapshot struct {
	Status domain.AppStatus `json:"status"`
}

type revokedSnapshot struct {
	RevokedAt *time.Time `json:"revoked_at"`
}

func appTarget(a *domain.App) domain.AuditTarget {
	return domain.AuditTarget{Type: TargetApp, ID: a.PublicID}
}

func keyTarget(k *domain.APIKey) domain.AuditTarget {
	return domain.AuditTarget{Type: TargetAPIKey, ID: k.PublicID}
}

// timestamp is now at the precision Postgres stores.
func (s *AppService) timestamp() time.Time {
	return s.now().UTC().Truncate(time.Microsecond)
}

// List returns one page of apps, newest first.
func (s *AppService) List(ctx context.Context, f domain.AppFilter, req domain.PageRequest) (domain.PageResult[domain.App], error) {
	p, err := req.Page()
	if err != nil {
		return domain.PageResult[domain.App]{}, err
	}
	res, err := s.apps.List(ctx, f, p)
	if err != nil {
		return domain.PageResult[domain.App]{}, fmt.Errorf("list apps: %w", err)
	}
	return res, nil
}

// Get returns an app by public id.
func (s *AppService) Get(ctx context.Context, publicID string) (*domain.App, error) {
	a, err := s.apps.GetByPublicID(ctx, publicID)
	if err != nil {
		return nil, fmt.Errorf("get app: %w", err)
	}
	return a, nil
}

// Create registers an active app with its first key ("Default", no expiry)
// and returns the key's secret once.
func (s *AppService) Create(ctx context.Context, in CreateAppInput) (*AppCreated, error) {
	var fe fieldErrors
	name, nameRule := normalizeName(in.Name)
	fe.check("name", nameRule)
	slug, slugRule := normalizeSlug(in.Slug)
	fe.check("slug", slugRule)
	fe.check("environment", environmentRule(in.Environment))
	if in.WebhookURL {
		fe.check("webhook_url", RuleMoved)
	}
	successURL, rule := normalizeURL(in.RedirectSuccessURL)
	fe.check("redirect_success_url", rule)
	failureURL, rule := normalizeURL(in.RedirectFailureURL)
	fe.check("redirect_failure_url", rule)
	meta, metaErrs := parseMetadata(in.Metadata)
	fe = append(fe, metaErrs...)
	if err := fe.err(); err != nil {
		return nil, err
	}

	app := &domain.App{
		PublicID:           id.New(id.App),
		Slug:               slug,
		Name:               name,
		Environment:        in.Environment,
		Status:             domain.AppActive,
		Metadata:           meta,
		RedirectSuccessURL: successURL,
		RedirectFailureURL: failureURL,
	}
	key, apiKey, apiSecret, err := s.newKey(app, DefaultKeyLabel, nil)
	if err != nil {
		return nil, err
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.apps.Create(ctx, app); err != nil {
			return err
		}
		key.AppID = app.ID
		if err := s.keys.Create(ctx, key); err != nil {
			return err
		}
		if err := s.audit.Record(ctx, ActionAppCreate, appTarget(app), nil, snapshotApp(app)); err != nil {
			return err
		}
		return s.audit.Record(ctx, ActionAPIKeyCreate, keyTarget(key), nil, snapshotKey(app, key))
	})
	if err != nil {
		return nil, fmt.Errorf("create app: %w", err)
	}
	return &AppCreated{App: app, Key: key, APIKey: apiKey, APISecret: apiSecret}, nil
}

// newKey builds an unsaved key in the app's environment with a fresh key and
// encrypted secret, and returns the plaintext pair.
func (s *AppService) newKey(app *domain.App, label string, expiresAt *time.Time) (*domain.APIKey, string, string, error) {
	env := string(app.Environment)
	apiKey := keygen.APIKey(env)
	apiSecret := keygen.APISecret(env)
	enc, err := s.cipher.Encrypt(apiSecret)
	if err != nil {
		return nil, "", "", fmt.Errorf("encrypt api secret: %w", err)
	}
	k := &domain.APIKey{
		PublicID:    id.New(id.APIKey),
		AppID:       app.ID,
		Environment: app.Environment,
		Label:       label,
		Prefix:      keygen.KeyPrefixFor(env),
		Last4:       apiKey[len(apiKey)-4:],
		Hash:        keygen.HashAPIKey(apiKey),
		SecretEnc:   enc,
		ExpiresAt:   expiresAt,
	}
	return k, apiKey, apiSecret, nil
}

// Update changes the fields present in in; metadata replaces the whole
// object. A request that changes nothing writes and records nothing.
func (s *AppService) Update(ctx context.Context, publicID string, in UpdateAppInput) (*domain.App, error) {
	var fe fieldErrors
	if in.Slug {
		fe.check("slug", RuleImmutable)
	}
	if in.Environment {
		fe.check("environment", RuleImmutable)
	}
	if in.WebhookURL {
		fe.check("webhook_url", RuleMoved)
	}
	if !in.Slug && !in.Environment && !in.WebhookURL && in.Name == nil &&
		in.RedirectSuccessURL == nil && in.RedirectFailureURL == nil && in.Metadata == nil && !in.DefaultConnectionSet {
		return nil, domain.InvalidFields(domain.FieldError{Field: "body", Rule: RuleRequired})
	}
	changes := map[string]any{}
	if in.Name != nil {
		name, rule := normalizeName(*in.Name)
		fe.check("name", rule)
		changes["name"] = name
	}
	for field, v := range map[string]*string{
		"redirect_success_url": in.RedirectSuccessURL, "redirect_failure_url": in.RedirectFailureURL,
	} {
		if v != nil {
			u, rule := normalizeURL(*v)
			fe.check(field, rule)
			changes[field] = u
		}
	}
	if in.Metadata != nil {
		meta, errs := parseMetadata(in.Metadata)
		fe = append(fe, errs...)
		changes["metadata"] = meta
	}
	var connPublicID string
	if in.DefaultConnectionSet && in.DefaultConnectionID != nil {
		var err error
		if connPublicID, err = id.ParseWithPrefix(*in.DefaultConnectionID, id.Connection); err != nil {
			fe.check("default_connection_id", RulePublicID)
		}
	}
	if err := fe.sorted(appFieldOrder).err(); err != nil {
		return nil, err
	}

	var app *domain.App
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		app, err = s.apps.GetByPublicIDForUpdate(ctx, publicID)
		if err != nil {
			return err
		}
		before, after := applyAppChanges(app, changes)
		if in.DefaultConnectionSet {
			if err := s.applyDefaultConnection(ctx, app, connPublicID, before, after); err != nil {
				return err
			}
		}
		if len(after) == 0 {
			return nil
		}
		if err := s.apps.Update(ctx, app); err != nil {
			return err
		}
		return s.audit.Record(ctx, ActionAppUpdate, appTarget(app), before, after)
	})
	if err != nil {
		return nil, fmt.Errorf("update app: %w", err)
	}
	return app, nil
}

// appFieldOrder is the order field errors are reported in.
var appFieldOrder = []string{"name", "slug", "environment", "webhook_url", "redirect_success_url", "redirect_failure_url",
	"metadata", "default_connection_id"}

// applyDefaultConnection sets or, for an empty connPublicID, clears the app's
// default connection and records the change in before/after. The connection
// is share-locked, so it cannot be disabled before the transaction commits;
// the composite foreign key backs the environment and merchant rules.
func (s *AppService) applyDefaultConnection(ctx context.Context, app *domain.App, connPublicID string, before, after map[string]any) error {
	var next *domain.Connection
	if connPublicID != "" {
		c, err := s.conns.GetByPublicIDForShare(ctx, connPublicID)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.InvalidFields(domain.FieldError{Field: "default_connection_id", Rule: RuleExists})
		}
		if err != nil {
			return err
		}
		switch {
		case c.Environment != app.Environment:
			return domain.InvalidFields(domain.FieldError{Field: "default_connection_id", Rule: RuleEnvironment})
		case c.Status != domain.ConnectionActive:
			return domain.InvalidFields(domain.FieldError{Field: "default_connection_id", Rule: RuleStatus})
		}
		next = c
	}
	if (next == nil && app.DefaultConnectionID == nil) || (next != nil && app.DefaultConnectionID != nil && *app.DefaultConnectionID == next.ID) {
		return nil
	}
	before["default_connection_id"] = connectionPublicID(app.DefaultConnection)
	if next == nil {
		app.DefaultConnectionID, app.DefaultConnection = nil, nil
		after["default_connection_id"] = nil
		return nil
	}
	summary := next.Summary()
	app.DefaultConnectionID, app.DefaultConnection = &next.ID, &summary
	after["default_connection_id"] = next.PublicID
	return nil
}

func connectionPublicID(s *domain.ConnectionSummary) any {
	if s == nil {
		return nil
	}
	return s.PublicID
}

// applyAppChanges sets every changed field on a and returns the old and new
// values of only those fields.
func applyAppChanges(a *domain.App, changes map[string]any) (before, after map[string]any) {
	before, after = map[string]any{}, map[string]any{}
	fields := map[string]*string{
		"name": &a.Name, "redirect_success_url": &a.RedirectSuccessURL, "redirect_failure_url": &a.RedirectFailureURL,
	}
	for name, ptr := range fields {
		if v, ok := changes[name].(string); ok && v != *ptr {
			before[name], after[name] = *ptr, v
			*ptr = v
		}
	}
	if m, ok := changes["metadata"].(domain.Metadata); ok && !reflect.DeepEqual(map[string]any(m), map[string]any(orEmpty(a.Metadata))) {
		before["metadata"], after["metadata"] = orEmpty(a.Metadata), m
		a.Metadata = m
	}
	return before, after
}

func orEmpty(m domain.Metadata) domain.Metadata {
	if m == nil {
		return domain.Metadata{}
	}
	return m
}

// Disable makes every /v1 request of the app fail with tenant_inactive.
// Disabling a disabled app changes and records nothing.
func (s *AppService) Disable(ctx context.Context, publicID string) (*domain.App, error) {
	return s.setStatus(ctx, publicID, domain.AppDisabled, ActionAppDisable)
}

// Enable lets a disabled app call /v1 again. Enabling an active app changes
// and records nothing.
func (s *AppService) Enable(ctx context.Context, publicID string) (*domain.App, error) {
	return s.setStatus(ctx, publicID, domain.AppActive, ActionAppEnable)
}

func (s *AppService) setStatus(ctx context.Context, publicID string, status domain.AppStatus, action string) (*domain.App, error) {
	var app *domain.App
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		app, err = s.apps.GetByPublicIDForUpdate(ctx, publicID)
		if err != nil {
			return err
		}
		if app.Status == status {
			return nil
		}
		before := statusSnapshot{Status: app.Status}
		app.Status = status
		if err := s.apps.Update(ctx, app); err != nil {
			return err
		}
		return s.audit.Record(ctx, action, appTarget(app), before, statusSnapshot{Status: status})
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	return app, nil
}

// ListKeys returns one page of the app's keys, newest first, revoked and
// expired ones included.
func (s *AppService) ListKeys(ctx context.Context, appPublicID string, req domain.PageRequest) (*domain.App, domain.PageResult[domain.APIKey], error) {
	p, err := req.Page()
	if err != nil {
		return nil, domain.PageResult[domain.APIKey]{}, err
	}
	app, err := s.apps.GetByPublicID(ctx, appPublicID)
	if err != nil {
		return nil, domain.PageResult[domain.APIKey]{}, fmt.Errorf("get app: %w", err)
	}
	res, err := s.keys.List(ctx, app.ID, p)
	if err != nil {
		return nil, domain.PageResult[domain.APIKey]{}, fmt.Errorf("list api keys: %w", err)
	}
	return app, res, nil
}

// CreateKeyInput is a new key. A nil ExpiresAt never expires.
type CreateKeyInput struct {
	Label     string
	ExpiresAt *time.Time
}

// CreateKey issues a key in the app's environment, disabled apps included,
// within the usable-key limit.
func (s *AppService) CreateKey(ctx context.Context, appPublicID string, in CreateKeyInput) (*KeyCreated, error) {
	now := s.now()
	var fe fieldErrors
	label, rule := normalizeLabel(in.Label)
	fe.check("label", rule)
	fe.check("expires_at", expiresAtRule(in.ExpiresAt, now))
	if err := fe.err(); err != nil {
		return nil, err
	}
	var expiresAt *time.Time
	if in.ExpiresAt != nil {
		t := in.ExpiresAt.UTC().Truncate(time.Microsecond)
		expiresAt = &t
	}

	var out *KeyCreated
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		app, err := s.lockAppWithRoom(ctx, appPublicID, now)
		if err != nil {
			return err
		}
		key, apiKey, apiSecret, err := s.newKey(app, label, expiresAt)
		if err != nil {
			return err
		}
		if err := s.keys.Create(ctx, key); err != nil {
			return err
		}
		if err := s.audit.Record(ctx, ActionAPIKeyCreate, keyTarget(key), nil, snapshotKey(app, key)); err != nil {
			return err
		}
		out = &KeyCreated{App: app, Key: key, APIKey: apiKey, APISecret: apiSecret}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}
	return out, nil
}

// lockAppWithRoom locks the app row, which serializes key creation per app,
// and fails when the app already has the maximum of usable keys.
func (s *AppService) lockAppWithRoom(ctx context.Context, appPublicID string, now time.Time) (*domain.App, error) {
	app, err := s.apps.GetByPublicIDForUpdate(ctx, appPublicID)
	if err != nil {
		return nil, err
	}
	if err := s.requireRoom(ctx, app, now); err != nil {
		return nil, err
	}
	return app, nil
}

// requireRoom fails when the app already has the maximum of usable keys. The
// caller must hold the app's row lock.
func (s *AppService) requireRoom(ctx context.Context, app *domain.App, now time.Time) error {
	n, err := s.keys.CountUsable(ctx, app.ID, now)
	if err != nil {
		return err
	}
	if n >= domain.MaxUsableAPIKeys {
		return errTooManyKeys
	}
	return nil
}

// RotateKey issues a new key with the rotated key's label and no expiry. The
// rotated key is left untouched, so both work until it is revoked.
func (s *AppService) RotateKey(ctx context.Context, appPublicID, keyPublicID string) (*KeyCreated, error) {
	now := s.now()
	var out *KeyCreated
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		app, err := s.apps.GetByPublicIDForUpdate(ctx, appPublicID)
		if err != nil {
			return err
		}
		old, err := s.keys.GetByPublicIDForUpdate(ctx, app.ID, keyPublicID)
		if err != nil {
			return err
		}
		switch old.Status(now) {
		case domain.APIKeyRevoked:
			return errRotateRevoked
		case domain.APIKeyExpired:
			return errRotateExpired
		}
		if err := s.requireRoom(ctx, app, now); err != nil {
			return err
		}
		key, apiKey, apiSecret, err := s.newKey(app, old.Label, nil)
		if err != nil {
			return err
		}
		if err := s.keys.Create(ctx, key); err != nil {
			return err
		}
		if err := s.audit.Record(ctx, ActionAPIKeyRotate, keyTarget(key), snapshotKey(app, old), snapshotKey(app, key)); err != nil {
			return err
		}
		out = &KeyCreated{App: app, Key: key, APIKey: apiKey, APISecret: apiSecret}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("rotate api key: %w", err)
	}
	return out, nil
}

// RevokeKey stops a key at once. Revoking a revoked key changes and records
// nothing; expired keys and the app's last usable key may be revoked.
func (s *AppService) RevokeKey(ctx context.Context, appPublicID, keyPublicID string) (*domain.App, *domain.APIKey, error) {
	var app *domain.App
	var key *domain.APIKey
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		app, err = s.apps.GetByPublicID(ctx, appPublicID)
		if err != nil {
			return err
		}
		key, err = s.keys.GetByPublicIDForUpdate(ctx, app.ID, keyPublicID)
		if err != nil {
			return err
		}
		if key.RevokedAt != nil {
			return nil
		}
		now := s.timestamp()
		key.RevokedAt = &now
		if err := s.keys.Revoke(ctx, key); err != nil {
			return err
		}
		return s.audit.Record(ctx, ActionAPIKeyRevoke, keyTarget(key), revokedSnapshot{}, revokedSnapshot{RevokedAt: &now})
	})
	if err != nil {
		return nil, nil, fmt.Errorf("revoke api key: %w", err)
	}
	return app, key, nil
}

// Lookup resolves an X-Api-Key to its key and app. A key whose environment
// disagrees with its app or with its own prefix is reported as unknown, with
// ErrKeyEnvironmentMismatch as the cause. It does not check the key's state;
// see Authorize, which runs after the signature.
func (s *AppService) Lookup(ctx context.Context, rawKey string) (*domain.App, *domain.APIKey, error) {
	key, app, err := s.keys.GetByHashWithApp(ctx, keygen.HashAPIKey(rawKey))
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil, domain.ErrUnknownAPIKey
	}
	if err != nil {
		return nil, nil, fmt.Errorf("lookup api key: %w", err)
	}
	headerEnv, _ := keygen.ParseKeyEnvironment(rawKey)
	if key.Environment != app.Environment || (headerEnv != "" && domain.Environment(headerEnv) != key.Environment) {
		return app, key, &domain.Error{Code: domain.CodeUnknownAPIKey, Message: domain.ErrUnknownAPIKey.Message, Err: ErrKeyEnvironmentMismatch}
	}
	return app, key, nil
}

// Authorize checks the key's and the app's state.
func (s *AppService) Authorize(app *domain.App, key *domain.APIKey) error {
	switch key.Status(s.now()) {
	case domain.APIKeyRevoked:
		return domain.ErrAPIKeyRevoked
	case domain.APIKeyExpired:
		return domain.ErrAPIKeyExpired
	}
	if app.Status != domain.AppActive {
		return domain.ErrAppDisabled
	}
	return nil
}

// Touch records that key authenticated a request, at most once a minute.
func (s *AppService) Touch(ctx context.Context, key *domain.APIKey) error {
	now := s.timestamp()
	if key.LastUsedAt != nil && now.Sub(*key.LastUsedAt) < time.Minute {
		return nil
	}
	if err := s.keys.TouchLastUsed(ctx, key.ID, now); err != nil {
		return fmt.Errorf("touch api key %s: %w", key.PublicID, err)
	}
	return nil
}
