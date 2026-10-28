package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/provider/xendit"
)

// LegacyConnectionName is the name of the connection imported from the
// deprecated XENDIT_* settings.
const LegacyConnectionName = "xendit-imported"

// ErrUnknownKeyPrefix is a XENDIT_SECRET_KEY that is neither a development
// nor a production key, so its environment is unknown.
var ErrUnknownKeyPrefix = errors.New("XENDIT_SECRET_KEY starts with neither xnd_development_ nor xnd_production_")

// LegacyImport is what ImportLegacyXendit did.
type LegacyImport struct {
	Connection *domain.Connection
	// Created is false when an earlier start already imported the settings;
	// then nothing below was changed.
	Created bool
	// Defaulted are the apps that got the connection as their default.
	Defaulted []domain.AppSummary
	// OtherEnvironment are apps without a default connection whose
	// environment differs from the connection's; they keep none.
	OtherEnvironment   []domain.AppSummary
	PaymentsBackfilled int64
}

// ImportLegacyXendit turns the deprecated XENDIT_SECRET_KEY and
// XENDIT_CALLBACK_TOKEN settings into a connection, once: it makes the
// connection the target of /webhooks/xendit, assigns every payment without a
// connection to it, and makes it the default of every app of its environment
// that has none. It does nothing (nil, nil) when either value is empty, and
// returns the earlier connection, unchanged, once one was imported. The
// default merchant's row lock makes concurrent starts import once.
func (s *ConnectionService) ImportLegacyXendit(ctx context.Context, secretKey, callbackToken string) (*LegacyImport, error) {
	if secretKey == "" || callbackToken == "" {
		return nil, nil
	}
	ctx = domain.WithActor(ctx, domain.SystemActor("import"))
	var res *LegacyImport
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		m, err := s.merchants.GetDefaultForUpdate(ctx)
		if err != nil {
			return err
		}
		if m.LegacyConnectionID != nil {
			c, err := s.conns.GetByID(ctx, *m.LegacyConnectionID)
			if err != nil {
				return err
			}
			res = &LegacyImport{Connection: c}
			return nil
		}
		env, ok := xendit.EnvironmentForKey(secretKey)
		if !ok {
			return ErrUnknownKeyPrefix
		}
		res, err = s.importConnection(ctx, m, env, secretKey, callbackToken)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("import xendit settings: %w", err)
	}
	return res, nil
}

func (s *ConnectionService) importConnection(ctx context.Context, m *domain.Merchant, env domain.Environment, secretKey, callbackToken string) (*LegacyImport, error) {
	publicID := id.New(id.Connection)
	name := LegacyConnectionName
	taken, err := s.conns.NameTaken(ctx, m.ID, name)
	if err != nil {
		return nil, err
	}
	if taken {
		name += "-" + strings.ToLower(publicID[len(publicID)-6:])
	}
	c, err := s.prepare(CreateConnectionInput{
		Provider:    string(xendit.Name),
		Name:        name,
		Environment: env,
		Credentials: map[string]any{"secret_key": secretKey, "callback_token": callbackToken},
	}, publicID)
	if err != nil {
		return nil, describeFieldErrors(err)
	}
	if err := s.insert(ctx, m, c); err != nil {
		return nil, err
	}
	if err := s.merchants.SetLegacyConnection(ctx, m.ID, c.ID); err != nil {
		return nil, err
	}
	c.LegacyWebhookAlias = true
	res := &LegacyImport{Connection: c, Created: true}
	if res.PaymentsBackfilled, err = s.payments.BackfillConnection(ctx, c.ID, c.Provider); err != nil {
		return nil, err
	}
	apps, err := s.apps.ListWithoutDefaultConnection(ctx)
	if err != nil {
		return nil, err
	}
	for i := range apps {
		a := &apps[i]
		if a.Environment != c.Environment {
			res.OtherEnvironment = append(res.OtherEnvironment, a.Summary())
			continue
		}
		a.DefaultConnectionID = &c.ID
		if err := s.apps.Update(ctx, a); err != nil {
			return nil, err
		}
		if err := s.audit.Record(ctx, ActionAppUpdate, appTarget(a),
			map[string]any{"default_connection_id": nil}, map[string]any{"default_connection_id": c.PublicID}); err != nil {
			return nil, err
		}
		res.Defaulted = append(res.Defaulted, a.Summary())
	}
	return res, nil
}

// describeFieldErrors names the failed fields of a validation error, so the
// startup log says what is wrong without repeating any value.
func describeFieldErrors(err error) error {
	var coded *domain.Error
	if !errors.As(err, &coded) {
		return err
	}
	fields, _ := coded.Details["fields"].([]domain.FieldError)
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, f.Field+" ("+f.Rule+")")
	}
	return fmt.Errorf("%w: invalid %s", domain.ErrInvalidInput, strings.Join(parts, ", "))
}
