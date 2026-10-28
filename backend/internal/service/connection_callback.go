package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/provider"
	"github.com/mydisha/keipay/backend/internal/provider/xendit"
)

// CallbackConnection resolves the connection a callback URL names. An
// unknown connection, or one of another provider, is ErrUnknownConnection.
// It needs no request body, so a URL naming no connection is refused before
// the body is read.
func (s *ConnectionService) CallbackConnection(ctx context.Context, providerName, publicID string) (*domain.Connection, error) {
	c, err := s.conns.GetByPublicID(ctx, publicID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, ErrUnknownConnection
	}
	if err != nil {
		return nil, fmt.Errorf("load connection: %w", err)
	}
	if c.Provider != providerName {
		return nil, ErrUnknownConnection
	}
	return c, nil
}

// LegacyCallbackConnection is CallbackConnection for /webhooks/xendit: the
// connection imported from the deprecated settings, ErrUnknownConnection when
// none was.
func (s *ConnectionService) LegacyCallbackConnection(ctx context.Context) (*domain.Connection, error) {
	m, err := s.merchants.GetDefault(ctx)
	if err != nil {
		return nil, fmt.Errorf("load merchant: %w", err)
	}
	if m.LegacyConnectionID == nil {
		return nil, ErrUnknownConnection
	}
	c, err := s.conns.GetByID(ctx, *m.LegacyConnectionID)
	if err != nil {
		return nil, fmt.Errorf("load legacy connection: %w", err)
	}
	if c.Provider != string(xendit.Name) {
		return nil, ErrUnknownConnection
	}
	return c, nil
}

// VerifyCallback checks a callback with c's credentials only, then parses
// it. A failed check is domain.ErrInvalidCallbackToken; a body the provider
// cannot read is invalid_request.
func (s *ConnectionService) VerifyCallback(c *domain.Connection, h http.Header, raw []byte) (*provider.InboundEvent, error) {
	prov, err := s.providerOf(c)
	if err != nil {
		return nil, err
	}
	acc, err := openAccount(s.sealer, c)
	if err != nil {
		return nil, err
	}
	if err := prov.VerifyWebhook(acc, h, raw); err != nil {
		return nil, domain.ErrInvalidCallbackToken
	}
	ev, err := prov.ParseWebhook(raw)
	if err != nil {
		return nil, fmt.Errorf("parse callback: %w: %w", domain.InvalidFields(), err)
	}
	return ev, nil
}
