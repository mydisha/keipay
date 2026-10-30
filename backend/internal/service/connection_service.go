package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/provider"
)

// Field rules for connections, reported in details.fields.
const (
	RuleDuplicate   = "duplicate"
	RuleMinEnabled  = "min_enabled"
	RuleExists      = "exists"
	RuleEnvironment = "environment"
	RuleStatus      = "status"
	RulePublicID    = "public_id"
)

// Connection test messages. They are fixed strings, so a gateway answer or a
// credential can never reach the console through them.
const (
	TestMessageOK          = "Credentials accepted."
	TestMessageRejected    = "The provider rejected the credentials."
	TestMessageUnreachable = "Could not reach the provider. Try again later."
	TestMessageUnexpected  = "The test failed unexpectedly."
)

// WarningTransactionsUnavailable is the warning of a passed test whose
// credentials the gateway refuses for listing transactions, which
// reconciliation needs.
const WarningTransactionsUnavailable = "transactions_unavailable"

// warningMessages are the fixed messages of the test warnings.
var warningMessages = map[string]string{
	WarningTransactionsUnavailable: "The provider refused to list transactions with these credentials, so reconciliation runs will fail. Give the key read access to transactions.",
}

// ConnectionTestWarning is something a passed test found that will make a
// later feature fail.
type ConnectionTestWarning struct {
	Code    string
	Message string
}

// DefaultConnectionTestTimeout bounds one credential test.
const DefaultConnectionTestTimeout = 10 * time.Second

// minLast4Runes is the shortest credential whose last four characters are
// shown; four characters of a shorter value give too much of it away.
const minLast4Runes = 12

var connectionNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$`)

// CredentialSealer keeps connection credentials sealed at rest, bound to aad
// (the connection's public id).
type CredentialSealer interface {
	Seal(plaintext []byte, aad string) (ciphertext, dataKeyEnc string, err error)
	Open(ciphertext, dataKeyEnc, aad string) ([]byte, error)
}

// PaymentBackfiller assigns payments made before connections to one.
type PaymentBackfiller interface {
	BackfillConnection(ctx context.Context, connectionID int64, provider string) (int64, error)
}

// ErrUnknownConnection is a callback URL naming no connection of that
// provider.
var ErrUnknownConnection = errors.New("unknown connection")

// ConnectionDeps are the collaborators of a ConnectionService.
type ConnectionDeps struct {
	Connections domain.ConnectionRepository
	Merchants   domain.MerchantRepository
	Apps        domain.AppRepository
	Payments    PaymentBackfiller
	Registry    *provider.Registry
	Sealer      CredentialSealer
	Tx          domain.TxManager
	Audit       Auditor
	Log         *zap.Logger
	// BaseURL is HTTP_BASE_URL; callback URLs are built below it.
	BaseURL string
}

// ConnectionService manages gateway connections. Every admin mutation and its
// audit entry commit in one transaction; credentials never leave it in
// plain text except towards the provider adapter.
type ConnectionService struct {
	conns       domain.ConnectionRepository
	merchants   domain.MerchantRepository
	apps        domain.AppRepository
	payments    PaymentBackfiller
	registry    *provider.Registry
	sealer      CredentialSealer
	tx          domain.TxManager
	audit       Auditor
	log         *zap.Logger
	baseURL     string
	now         func() time.Time
	testTimeout time.Duration
}

// NewConnectionService constructs a ConnectionService.
func NewConnectionService(d ConnectionDeps) *ConnectionService {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &ConnectionService{
		conns: d.Connections, merchants: d.Merchants, apps: d.Apps, payments: d.Payments,
		registry: d.Registry, sealer: d.Sealer, tx: d.Tx, audit: d.Audit, log: log,
		baseURL: strings.TrimRight(d.BaseURL, "/"), now: time.Now, testTimeout: DefaultConnectionTestTimeout,
	}
}

// CreateConnectionInput is a new connection. Credentials and Settings are
// JSON objects decoded with json.Number; nil Settings means all defaults.
type CreateConnectionInput struct {
	Provider    string
	Name        string
	Environment domain.Environment
	Credentials map[string]any
	Settings    map[string]any
}

// UpdateConnectionInput holds the fields present in a PATCH body; nil means
// absent. Provider, Environment and Credentials only record that the field
// was sent, which is refused.
type UpdateConnectionInput struct {
	Name        *string
	Settings    map[string]any
	Provider    bool
	Environment bool
	Credentials bool
}

// ChannelChoice is one entry of a channel list replacement.
type ChannelChoice struct {
	Channel string
	Enabled bool
}

// CredentialHint is what is shown of one stored credential.
type CredentialHint struct {
	Key   string
	Last4 *string
}

// ChannelState is whether a connection offers one channel of its provider.
type ChannelState struct {
	Channel domain.Channel `json:"channel"`
	Enabled bool           `json:"enabled"`
}

// ConnectionDetail is a connection with everything the console shows.
type ConnectionDetail struct {
	Connection  *domain.Connection
	Credentials []CredentialHint
	Channels    []ChannelState
	WebhookURL  string
}

// ConnectionTestResult is the outcome of a credential test.
type ConnectionTestResult struct {
	Result   domain.ConnectionTestStatus
	Message  string
	Warnings []ConnectionTestWarning
	TestedAt time.Time
	Detail   *ConnectionDetail
}

// connectionSnapshot is the audited view of a connection: never credentials
// or their hints.
type connectionSnapshot struct {
	ID          string                  `json:"id"`
	Provider    string                  `json:"provider"`
	Name        string                  `json:"name"`
	Environment domain.Environment      `json:"environment"`
	Status      domain.ConnectionStatus `json:"status"`
	Settings    map[string]any          `json:"settings"`
}

func snapshotConnection(c *domain.Connection) connectionSnapshot {
	return connectionSnapshot{ID: c.PublicID, Provider: c.Provider, Name: c.Name, Environment: c.Environment,
		Status: c.Status, Settings: orEmptyObject(c.Settings)}
}

func connectionTarget(c *domain.Connection) domain.AuditTarget {
	return domain.AuditTarget{Type: TargetConnection, ID: c.PublicID}
}

func orEmptyObject(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func (s *ConnectionService) timestamp() time.Time {
	return s.now().UTC().Truncate(time.Microsecond)
}

// Providers returns every registered provider adapter, sorted by name.
func (s *ConnectionService) Providers() []provider.Provider { return s.registry.All() }

// KnownProvider reports whether name is a registered provider.
func (s *ConnectionService) KnownProvider(name string) bool {
	return s.registry.Has(provider.Name(name))
}

// List returns one page of connections, newest first. It never decrypts.
func (s *ConnectionService) List(ctx context.Context, f domain.ConnectionFilter, req domain.PageRequest) (domain.PageResult[domain.Connection], error) {
	p, err := req.Page()
	if err != nil {
		return domain.PageResult[domain.Connection]{}, err
	}
	res, err := s.conns.List(ctx, f, p)
	if err != nil {
		return domain.PageResult[domain.Connection]{}, fmt.Errorf("list connections: %w", err)
	}
	return res, nil
}

// Get returns a connection with its credential hints, channels and callback URL.
func (s *ConnectionService) Get(ctx context.Context, publicID string) (*ConnectionDetail, error) {
	c, err := s.conns.GetByPublicID(ctx, publicID)
	if err != nil {
		return nil, fmt.Errorf("get connection: %w", err)
	}
	return s.detail(ctx, c)
}

// Create validates and stores an active, untested connection. All field
// errors come back in one invalid_request.
func (s *ConnectionService) Create(ctx context.Context, in CreateConnectionInput) (*ConnectionDetail, error) {
	c, err := s.prepare(in, id.New(id.Connection))
	if err != nil {
		return nil, err
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		m, err := s.merchants.GetDefault(ctx)
		if err != nil {
			return err
		}
		return s.insert(ctx, m, c)
	})
	if err != nil {
		return nil, fmt.Errorf("create connection: %w", err)
	}
	return s.detail(ctx, c)
}

// insert stores c for merchant m and records connection.create.
func (s *ConnectionService) insert(ctx context.Context, m *domain.Merchant, c *domain.Connection) error {
	c.MerchantID = m.ID
	if err := s.conns.Create(ctx, c); err != nil {
		return err
	}
	return s.audit.Record(ctx, ActionConnectionCreate, connectionTarget(c), nil, snapshotConnection(c))
}

// prepare validates in and returns the unsaved connection with its
// credentials sealed under publicID.
func (s *ConnectionService) prepare(in CreateConnectionInput, publicID string) (*domain.Connection, error) {
	var fe fieldErrors
	var prov provider.Provider
	providerName := strings.TrimSpace(in.Provider)
	if providerName == "" {
		fe.check("provider", RuleRequired)
	} else if p, err := s.registry.Get(provider.Name(providerName)); err != nil {
		fe.check("provider", RuleOneOf)
	} else {
		prov = p
	}
	name, rule := normalizeConnectionName(in.Name)
	fe.check("name", rule)
	fe.check("environment", environmentRule(in.Environment))
	var creds provider.Credentials
	var settings map[string]any
	if prov != nil {
		var errs []domain.FieldError
		creds, errs = validateCredentials(prov, in.Environment, in.Credentials)
		fe = append(fe, errs...)
		settings, errs = provider.ValidateValues(prov.SettingsSchema(), in.Settings, "settings")
		fe = append(fe, errs...)
	}
	if err := fe.err(); err != nil {
		return nil, err
	}
	c := &domain.Connection{
		PublicID:    publicID,
		Provider:    providerName,
		Name:        name,
		Environment: in.Environment,
		Status:      domain.ConnectionActive,
		Settings:    settings,
	}
	if err := s.seal(c, creds); err != nil {
		return nil, err
	}
	return c, nil
}

// validateCredentials runs the schema and, when it passes and env is valid,
// the provider's own rules.
func validateCredentials(prov provider.Provider, env domain.Environment, in map[string]any) (provider.Credentials, []domain.FieldError) {
	values, errs := provider.ValidateValues(prov.CredentialSchema(), in, "credentials")
	creds := make(provider.Credentials, len(values))
	for k, v := range values {
		s, _ := v.(string)
		creds[k] = s
	}
	if len(errs) == 0 && env.Valid() {
		errs = prov.ValidateAccount(env, creds)
	}
	return creds, errs
}

// seal stores creds on c, encrypted with a fresh data key bound to its
// public id, and recomputes the hints.
func (s *ConnectionService) seal(c *domain.Connection, creds provider.Credentials) error {
	plain, err := json.Marshal(creds)
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	enc, dek, err := s.sealer.Seal(plain, c.PublicID)
	if err != nil {
		return fmt.Errorf("seal credentials: %w", err)
	}
	c.CredentialsEnc, c.CredentialsDEKEnc, c.KeyVersion = enc, dek, 1
	c.CredentialsLast4 = make(map[string]*string, len(creds))
	for k, v := range creds {
		c.CredentialsLast4[k] = last4(v)
	}
	return nil
}

// last4 is the last four characters of v, or nil when v is too short to
// show any of it.
func last4(v string) *string {
	if utf8.RuneCountInString(v) < minLast4Runes {
		return nil
	}
	r := []rune(v)
	tail := string(r[len(r)-4:])
	return &tail
}

// openAccount decrypts a connection's credentials into the account every
// gateway call takes.
func openAccount(sealer CredentialSealer, c *domain.Connection) (provider.Account, error) {
	plain, err := sealer.Open(c.CredentialsEnc, c.CredentialsDEKEnc, c.PublicID)
	if err != nil {
		return provider.Account{}, fmt.Errorf("open credentials of %s: %w", c.PublicID, err)
	}
	var creds provider.Credentials
	if err := json.Unmarshal(plain, &creds); err != nil {
		return provider.Account{}, fmt.Errorf("decode credentials of %s: %w", c.PublicID, err)
	}
	return provider.Account{Environment: c.Environment, Credentials: creds, Settings: provider.Settings(c.Settings)}, nil
}

// normalizeConnectionName trims and lower-cases a name and returns it with
// the failed rule, or "".
func normalizeConnectionName(s string) (string, string) {
	name := strings.ToLower(strings.TrimSpace(s))
	if name == "" {
		return "", RuleRequired
	}
	if !connectionNameRe.MatchString(name) {
		return name, RuleSlug
	}
	return name, ""
}

// providerOf returns the adapter of c; a connection whose adapter is gone is
// an internal error.
func (s *ConnectionService) providerOf(c *domain.Connection) (provider.Provider, error) {
	p, err := s.registry.Get(provider.Name(c.Provider))
	if err != nil {
		return nil, domain.Internal(fmt.Errorf("connection %s: %w", c.PublicID, err))
	}
	return p, nil
}

// detail adds hints, channels and the callback URL to c.
func (s *ConnectionService) detail(ctx context.Context, c *domain.Connection) (*ConnectionDetail, error) {
	rows, err := s.conns.Channels(ctx, c.ID)
	if err != nil {
		return nil, fmt.Errorf("load channels: %w", err)
	}
	d := &ConnectionDetail{
		Connection:  c,
		Credentials: []CredentialHint{},
		Channels:    []ChannelState{},
		WebhookURL:  s.webhookURL(c),
	}
	prov, err := s.registry.Get(provider.Name(c.Provider))
	if err != nil {
		return d, nil
	}
	for _, f := range prov.CredentialSchema().Fields {
		d.Credentials = append(d.Credentials, CredentialHint{Key: f.Key, Last4: c.CredentialsLast4[f.Key]})
	}
	d.Channels = effectiveChannels(prov.Capabilities().Channels, rows)
	return d, nil
}

func (s *ConnectionService) webhookURL(c *domain.Connection) string {
	return s.baseURL + "/webhooks/" + c.Provider + "/" + c.PublicID
}

// effectiveChannels says, for every channel of the provider in its order,
// whether the connection offers it: all of them while nothing is stored,
// else exactly the stored enabled rows.
func effectiveChannels(caps []domain.Channel, rows []domain.ConnectionChannel) []ChannelState {
	stored := make(map[domain.Channel]bool, len(rows))
	for _, r := range rows {
		stored[r.Channel] = r.Enabled
	}
	out := make([]ChannelState, 0, len(caps))
	for _, ch := range caps {
		out = append(out, ChannelState{Channel: ch, Enabled: len(rows) == 0 || stored[ch]})
	}
	return out
}

// Update changes the name or the whole settings object. A request that
// changes nothing writes and records nothing.
func (s *ConnectionService) Update(ctx context.Context, publicID string, in UpdateConnectionInput) (*ConnectionDetail, error) {
	if in.Name == nil && in.Settings == nil && !in.Provider && !in.Environment && !in.Credentials {
		return nil, domain.InvalidFields(domain.FieldError{Field: "body", Rule: RuleRequired})
	}
	var fe fieldErrors
	var name string
	if in.Name != nil {
		var rule string
		name, rule = normalizeConnectionName(*in.Name)
		fe.check("name", rule)
	}
	if in.Provider {
		fe.check("provider", RuleImmutable)
	}
	if in.Environment {
		fe.check("environment", RuleImmutable)
	}
	if in.Credentials {
		fe.check("credentials", RuleImmutable)
	}

	var c *domain.Connection
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if c, err = s.conns.GetByPublicIDForUpdate(ctx, publicID); err != nil {
			return err
		}
		prov, err := s.providerOf(c)
		if err != nil {
			return err
		}
		var settings map[string]any
		if in.Settings != nil {
			var errs []domain.FieldError
			settings, errs = provider.ValidateValues(prov.SettingsSchema(), in.Settings, "settings")
			fe = append(fe, errs...)
		}
		if err := fe.err(); err != nil {
			return err
		}
		before, after := map[string]any{}, map[string]any{}
		if in.Name != nil && name != c.Name {
			before["name"], after["name"] = c.Name, name
			c.Name = name
		}
		if in.Settings != nil && !sameJSON(orEmptyObject(c.Settings), settings) {
			before["settings"], after["settings"] = orEmptyObject(c.Settings), settings
			c.Settings = settings
		}
		if len(after) == 0 {
			return nil
		}
		if err := s.conns.Update(ctx, c); err != nil {
			return err
		}
		return s.audit.Record(ctx, ActionConnectionUpdate, connectionTarget(c), before, after)
	})
	if err != nil {
		return nil, fmt.Errorf("update connection: %w", err)
	}
	return s.detail(ctx, c)
}

// sameJSON compares two values by their JSON encoding, so json.Number and
// int64 holding the same integer are equal.
func sameJSON(a, b any) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ja, jb)
}

// UpdateCredentials replaces the credential keys present in creds, keeps the
// others, validates the merged set like Create, re-seals it and clears the
// last test result. The audit entry names the replaced keys only.
func (s *ConnectionService) UpdateCredentials(ctx context.Context, publicID string, creds map[string]any) (*ConnectionDetail, error) {
	if len(creds) == 0 {
		return nil, domain.InvalidFields(domain.FieldError{Field: "credentials", Rule: RuleRequired})
	}
	var c *domain.Connection
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if c, err = s.conns.GetByPublicIDForUpdate(ctx, publicID); err != nil {
			return err
		}
		prov, err := s.providerOf(c)
		if err != nil {
			return err
		}
		current, err := openAccount(s.sealer, c)
		if err != nil {
			return domain.Internal(err)
		}
		merged := make(map[string]any, len(current.Credentials)+len(creds))
		for k, v := range current.Credentials {
			merged[k] = v
		}
		for k, v := range creds {
			merged[k] = v
		}
		next, errs := validateCredentials(prov, c.Environment, merged)
		if len(errs) > 0 {
			return domain.InvalidFields(errs...)
		}
		if err := s.seal(c, next); err != nil {
			return err
		}
		c.LastTestedAt, c.LastTestStatus, c.LastTestError = nil, nil, nil
		if err := s.conns.Update(ctx, c); err != nil {
			return err
		}
		changed := make([]string, 0, len(creds))
		for k := range creds {
			changed = append(changed, k)
		}
		slices.Sort(changed)
		return s.audit.Record(ctx, ActionConnectionUpdateCredentials, connectionTarget(c), nil,
			map[string]any{"changed_keys": changed})
	})
	if err != nil {
		return nil, fmt.Errorf("update connection credentials: %w", err)
	}
	return s.detail(ctx, c)
}

// ReplaceChannels stores the connection's channel list; an empty list resets
// it to every channel of the provider. A request whose effective channels
// equal the current ones writes and records nothing.
func (s *ConnectionService) ReplaceChannels(ctx context.Context, publicID string, choices []ChannelChoice) (*ConnectionDetail, error) {
	var c *domain.Connection
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if c, err = s.conns.GetByPublicIDForUpdate(ctx, publicID); err != nil {
			return err
		}
		prov, err := s.providerOf(c)
		if err != nil {
			return err
		}
		caps := prov.Capabilities().Channels
		rows, err := channelRows(caps, choices)
		if err != nil {
			return err
		}
		stored, err := s.conns.Channels(ctx, c.ID)
		if err != nil {
			return err
		}
		before, after := effectiveChannels(caps, stored), effectiveChannels(caps, rows)
		if slices.Equal(before, after) {
			return nil
		}
		if err := s.conns.ReplaceChannels(ctx, c.ID, rows); err != nil {
			return err
		}
		return s.audit.Record(ctx, ActionConnectionUpdateChannels, connectionTarget(c),
			map[string]any{"channels": before}, map[string]any{"channels": after})
	})
	if err != nil {
		return nil, fmt.Errorf("replace connection channels: %w", err)
	}
	return s.detail(ctx, c)
}

// channelRows validates a channel list against the provider's channels.
func channelRows(caps []domain.Channel, choices []ChannelChoice) ([]domain.ConnectionChannel, error) {
	var fe fieldErrors
	seen := make(map[domain.Channel]bool, len(choices))
	rows := make([]domain.ConnectionChannel, 0, len(choices))
	anyEnabled := false
	for i, choice := range choices {
		field := fmt.Sprintf("channels.%d.channel", i)
		ch := domain.Channel(choice.Channel)
		switch {
		case !slices.Contains(caps, ch):
			fe.check(field, RuleOneOf)
		case seen[ch]:
			fe.check(field, RuleDuplicate)
		default:
			seen[ch] = true
			anyEnabled = anyEnabled || choice.Enabled
			rows = append(rows, domain.ConnectionChannel{Channel: ch, Enabled: choice.Enabled})
		}
	}
	if len(choices) > 0 && !anyEnabled {
		fe.check("channels", RuleMinEnabled)
	}
	if err := fe.err(); err != nil {
		return nil, err
	}
	return rows, nil
}

// Disable stops a connection from taking new checkouts. A connection that is
// any app's default cannot be disabled; disabling a disabled one changes and
// records nothing.
func (s *ConnectionService) Disable(ctx context.Context, publicID string) (*ConnectionDetail, error) {
	return s.setStatus(ctx, publicID, domain.ConnectionDisabled, ActionConnectionDisable)
}

// Enable lets a disabled connection take checkouts again.
func (s *ConnectionService) Enable(ctx context.Context, publicID string) (*ConnectionDetail, error) {
	return s.setStatus(ctx, publicID, domain.ConnectionActive, ActionConnectionEnable)
}

type connectionStatusSnapshot struct {
	Status domain.ConnectionStatus `json:"status"`
}

func (s *ConnectionService) setStatus(ctx context.Context, publicID string, status domain.ConnectionStatus, action string) (*ConnectionDetail, error) {
	var c *domain.Connection
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		// The row lock waits for app updates that hold a shared lock on
		// this connection, so the in-use check sees their outcome.
		if c, err = s.conns.GetByPublicIDForUpdate(ctx, publicID); err != nil {
			return err
		}
		if c.Status == status {
			return nil
		}
		if status == domain.ConnectionDisabled {
			apps, n, err := s.conns.AppsUsing(ctx, c.ID, domain.MaxInUseApps)
			if err != nil {
				return err
			}
			if n > 0 {
				return domain.ConnectionInUse(apps, n)
			}
		}
		before := connectionStatusSnapshot{Status: c.Status}
		c.Status = status
		if err := s.conns.Update(ctx, c); err != nil {
			return err
		}
		return s.audit.Record(ctx, action, connectionTarget(c), before, connectionStatusSnapshot{Status: status})
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	return s.detail(ctx, c)
}

// Test checks the stored credentials with the provider and stores the
// outcome. A gateway failure is a failed result, not an error; failing to
// decrypt or to store is.
func (s *ConnectionService) Test(ctx context.Context, publicID string) (*ConnectionTestResult, error) {
	c, err := s.conns.GetByPublicID(ctx, publicID)
	if err != nil {
		return nil, fmt.Errorf("test connection: %w", err)
	}
	prov, err := s.providerOf(c)
	if err != nil {
		return nil, err
	}
	acc, err := openAccount(s.sealer, c)
	if err != nil {
		return nil, domain.Internal(err)
	}
	tctx, cancel := context.WithTimeout(ctx, s.testTimeout)
	testErr := prov.TestCredentials(tctx, acc)
	cancel()
	result, message := testOutcome(testErr)
	if message == TestMessageUnexpected {
		s.log.Error("connection test failed unexpectedly",
			zap.String("connection_id", c.PublicID), zap.String("provider", c.Provider), zap.Error(testErr))
	}
	warnings := []ConnectionTestWarning{}
	if result == domain.ConnectionTestOK {
		warnings = s.testWarnings(ctx, c, prov, acc)
	}
	warningCodes := make([]string, 0, len(warnings))
	for _, w := range warnings {
		warningCodes = append(warningCodes, w.Code)
	}

	testedAt := s.timestamp()
	var stored *domain.Connection
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if stored, err = s.conns.GetByPublicIDForUpdate(ctx, publicID); err != nil {
			return err
		}
		stored.LastTestedAt, stored.LastTestStatus, stored.LastTestError = &testedAt, &result, nil
		if result == domain.ConnectionTestFailed {
			stored.LastTestError = &message
		}
		if err := s.conns.Update(ctx, stored); err != nil {
			return err
		}
		return s.audit.Record(ctx, ActionConnectionTest, connectionTarget(stored), nil,
			map[string]any{"result": result, "tested_at": testedAt, "warnings": warningCodes})
	})
	if err != nil {
		return nil, fmt.Errorf("store connection test: %w", err)
	}
	d, err := s.detail(ctx, stored)
	if err != nil {
		return nil, err
	}
	return &ConnectionTestResult{Result: result, Message: message, Warnings: warnings, TestedAt: testedAt, Detail: d}, nil
}

// testWarnings checks the features beyond checkout that the provider
// declares. A provider that lists transactions is asked for one page of the
// last hour; only a refusal is a warning, since anything else may work on
// the next try.
func (s *ConnectionService) testWarnings(ctx context.Context, c *domain.Connection, prov provider.Provider, acc provider.Account) []ConnectionTestWarning {
	warnings := []ConnectionTestWarning{}
	lister, ok := prov.(provider.TransactionLister)
	if !ok || !prov.Capabilities().ListTransactions {
		return warnings
	}
	to := s.now().UTC()
	tctx, cancel := context.WithTimeout(ctx, s.testTimeout)
	defer cancel()
	_, err := lister.ListTransactions(tctx, acc, provider.TransactionQuery{From: to.Add(-time.Hour), To: to, Limit: 1})
	if errors.Is(err, provider.ErrRejected) {
		s.log.Info("connection cannot list transactions",
			zap.String("connection_id", c.PublicID), zap.String("provider", c.Provider), zap.Error(err))
		warnings = append(warnings, ConnectionTestWarning{Code: WarningTransactionsUnavailable,
			Message: warningMessages[WarningTransactionsUnavailable]})
	}
	return warnings
}

// testOutcome turns a TestCredentials error into the stored result and its
// fixed message.
func testOutcome(err error) (domain.ConnectionTestStatus, string) {
	switch {
	case err == nil:
		return domain.ConnectionTestOK, TestMessageOK
	case errors.Is(err, provider.ErrRejected):
		if code, ok := provider.StatusCode(err); ok {
			return domain.ConnectionTestFailed, fmt.Sprintf("The provider rejected the credentials (HTTP %d).", code)
		}
		return domain.ConnectionTestFailed, TestMessageRejected
	case errors.Is(err, provider.ErrRetryable), errors.Is(err, provider.ErrUnknownOutcome),
		errors.Is(err, context.DeadlineExceeded):
		return domain.ConnectionTestFailed, TestMessageUnreachable
	default:
		return domain.ConnectionTestFailed, TestMessageUnexpected
	}
}
