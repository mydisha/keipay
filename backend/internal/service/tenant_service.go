// Package service holds business logic, depending only on domain ports.
package service

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/keygen"
)

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$`)

// TenantCacheInvalidator drops cached tenant entries after mutations.
type TenantCacheInvalidator interface {
	Invalidate(ctx context.Context, apiKey string) error
}

// SecretCipher encrypts/decrypts tenant secrets at rest. The plaintext secret is
// the shared HMAC signing key, so it must be recoverable (encrypted, not hashed).
type SecretCipher interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(ciphertext string) (string, error)
}

// TenantService manages tenant lifecycle and credentials. Every mutation and
// its audit entry commit in one transaction.
type TenantService struct {
	repo   domain.TenantRepository
	cache  TenantCacheInvalidator
	cipher SecretCipher
	tx     domain.TxManager
	audit  Auditor
}

// NewTenantService constructs a TenantService. cache may be nil.
func NewTenantService(repo domain.TenantRepository, cache TenantCacheInvalidator, cipher SecretCipher, tx domain.TxManager, audit Auditor) *TenantService {
	return &TenantService{repo: repo, cache: cache, cipher: cipher, tx: tx, audit: audit}
}

// tenantSnapshot is the audited view of a tenant. It must never hold the API
// secret, its ciphertext or the full API key.
type tenantSnapshot struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	Slug               string `json:"slug"`
	APIKeyLast4        string `json:"api_key_last4"`
	WebhookURL         string `json:"webhook_url"`
	RedirectSuccessURL string `json:"redirect_success_url"`
	RedirectFailureURL string `json:"redirect_failure_url"`
	Active             bool   `json:"active"`
}

func snapshotTenant(t *domain.Tenant) tenantSnapshot {
	return tenantSnapshot{
		ID:                 t.ID,
		Name:               t.Name,
		Slug:               t.Slug,
		APIKeyLast4:        last4(t.APIKey),
		WebhookURL:         t.WebhookURL,
		RedirectSuccessURL: t.RedirectSuccessURL,
		RedirectFailureURL: t.RedirectFailureURL,
		Active:             t.Active,
	}
}

// apiKeySnapshot is the audited view of a credential rotation.
type apiKeySnapshot struct {
	APIKeyLast4 string `json:"api_key_last4"`
}

func last4(s string) string {
	if len(s) <= 4 {
		return s
	}
	return s[len(s)-4:]
}

func tenantTarget(id int64) domain.AuditTarget {
	return domain.AuditTarget{Type: TargetTenant, ID: strconv.FormatInt(id, 10)}
}

// CreateTenantInput is the request to register a new tenant.
type CreateTenantInput struct {
	Name               string
	Slug               string
	WebhookURL         string
	RedirectSuccessURL string
	RedirectFailureURL string
}

// Credentials is the one-time secret returned at create/rotate time.
type Credentials struct {
	APIKey    string `json:"api_key"`
	APISecret string `json:"api_secret"`
}

// Create registers a tenant and returns it plus one-time plaintext credentials.
func (s *TenantService) Create(ctx context.Context, in CreateTenantInput) (*domain.Tenant, *Credentials, error) {
	slug := strings.ToLower(strings.TrimSpace(in.Slug))
	if !slugRe.MatchString(slug) {
		return nil, nil, fmt.Errorf("%w: invalid slug", domain.ErrInvalidInput)
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, nil, fmt.Errorf("%w: name required", domain.ErrInvalidInput)
	}

	apiKey := keygen.APIKey()
	secret := keygen.Secret()
	enc, err := s.cipher.Encrypt(secret)
	if err != nil {
		return nil, nil, fmt.Errorf("encrypt secret: %w", err)
	}

	t := &domain.Tenant{
		Name:               in.Name,
		Slug:               slug,
		APIKey:             apiKey,
		APISecretEnc:       enc,
		WebhookURL:         in.WebhookURL,
		RedirectSuccessURL: in.RedirectSuccessURL,
		RedirectFailureURL: in.RedirectFailureURL,
		Active:             true,
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.repo.Create(ctx, t); err != nil {
			return err
		}
		return s.audit.Record(ctx, ActionTenantCreate, tenantTarget(t.ID), nil, snapshotTenant(t))
	})
	if err != nil {
		return nil, nil, fmt.Errorf("create tenant: %w", err)
	}
	return t, &Credentials{APIKey: apiKey, APISecret: secret}, nil
}

// UpdateTenantInput holds mutable tenant fields.
type UpdateTenantInput struct {
	Name               string
	WebhookURL         string
	RedirectSuccessURL string
	RedirectFailureURL string
	Active             bool
}

// Update persists mutable fields and invalidates cache.
func (s *TenantService) Update(ctx context.Context, id int64, in UpdateTenantInput) (*domain.Tenant, error) {
	var t *domain.Tenant
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		t, err = s.repo.GetByIDForUpdate(ctx, id)
		if err != nil {
			return err
		}
		before := snapshotTenant(t)
		t.Name = in.Name
		t.WebhookURL = in.WebhookURL
		t.RedirectSuccessURL = in.RedirectSuccessURL
		t.RedirectFailureURL = in.RedirectFailureURL
		t.Active = in.Active
		if err := s.repo.Update(ctx, t); err != nil {
			return err
		}
		return s.audit.Record(ctx, ActionTenantUpdate, tenantTarget(t.ID), before, snapshotTenant(t))
	})
	if err != nil {
		return nil, fmt.Errorf("update tenant %d: %w", id, err)
	}
	s.invalidate(ctx, t.APIKey)
	return t, nil
}

// RotateCredentials issues a new API key + secret for a tenant.
func (s *TenantService) RotateCredentials(ctx context.Context, id int64) (*domain.Tenant, *Credentials, error) {
	apiKey := keygen.APIKey()
	secret := keygen.Secret()
	enc, err := s.cipher.Encrypt(secret)
	if err != nil {
		return nil, nil, fmt.Errorf("encrypt secret: %w", err)
	}
	var t *domain.Tenant
	var oldKey string
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		t, err = s.repo.GetByIDForUpdate(ctx, id)
		if err != nil {
			return err
		}
		oldKey = t.APIKey
		t.APIKey = apiKey
		t.APISecretEnc = enc
		if err := s.repo.Update(ctx, t); err != nil {
			return err
		}
		return s.audit.Record(ctx, ActionTenantRotate, tenantTarget(t.ID),
			apiKeySnapshot{APIKeyLast4: last4(oldKey)}, apiKeySnapshot{APIKeyLast4: last4(apiKey)})
	})
	if err != nil {
		return nil, nil, fmt.Errorf("rotate tenant %d credentials: %w", id, err)
	}
	s.invalidate(ctx, oldKey)
	return t, &Credentials{APIKey: apiKey, APISecret: secret}, nil
}

// Get returns a tenant by id.
func (s *TenantService) Get(ctx context.Context, id int64) (*domain.Tenant, error) {
	return s.repo.GetByID(ctx, id)
}

// List returns all tenants.
func (s *TenantService) List(ctx context.Context) ([]domain.Tenant, error) {
	return s.repo.List(ctx)
}

func (s *TenantService) invalidate(ctx context.Context, apiKey string) {
	if s.cache != nil {
		_ = s.cache.Invalidate(ctx, apiKey)
	}
}
