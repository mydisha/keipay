// Package service holds business logic, depending only on domain ports.
package service

import (
	"context"
	"fmt"
	"regexp"
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

// TenantService manages tenant lifecycle and credentials.
type TenantService struct {
	repo   domain.TenantRepository
	cache  TenantCacheInvalidator
	cipher SecretCipher
}

// NewTenantService constructs a TenantService. cache may be nil.
func NewTenantService(repo domain.TenantRepository, cache TenantCacheInvalidator, cipher SecretCipher) *TenantService {
	return &TenantService{repo: repo, cache: cache, cipher: cipher}
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
		return nil, nil, err
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
	if err := s.repo.Create(ctx, t); err != nil {
		return nil, nil, err
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
	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	t.Name = in.Name
	t.WebhookURL = in.WebhookURL
	t.RedirectSuccessURL = in.RedirectSuccessURL
	t.RedirectFailureURL = in.RedirectFailureURL
	t.Active = in.Active
	if err := s.repo.Update(ctx, t); err != nil {
		return nil, err
	}
	s.invalidate(ctx, t.APIKey)
	return t, nil
}

// RotateCredentials issues a new API key + secret for a tenant.
func (s *TenantService) RotateCredentials(ctx context.Context, id int64) (*domain.Tenant, *Credentials, error) {
	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	oldKey := t.APIKey
	apiKey := keygen.APIKey()
	secret := keygen.Secret()
	enc, err := s.cipher.Encrypt(secret)
	if err != nil {
		return nil, nil, err
	}
	t.APIKey = apiKey
	t.APISecretEnc = enc
	if err := s.repo.Update(ctx, t); err != nil {
		return nil, nil, err
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
