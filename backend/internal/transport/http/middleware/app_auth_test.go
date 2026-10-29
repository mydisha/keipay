package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/hmac"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/pkg/keygen"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/testutil/memstore"
)

type memNonces struct {
	seen map[string]bool
	err  error
}

func newMemNonces() *memNonces { return &memNonces{seen: map[string]bool{}} }

func (m *memNonces) Acquire(_ context.Context, key string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	if m.seen[key] {
		return false, nil
	}
	m.seen[key] = true
	return true, nil
}

type prefixCipher struct{ err error }

func (p prefixCipher) Encrypt(s string) (string, error) { return "enc:" + s, nil }
func (p prefixCipher) Decrypt(c string) (string, error) {
	if p.err != nil {
		return "", p.err
	}
	return strings.TrimPrefix(c, "enc:"), nil
}

// authFixture is AppAuth over a real AppService and in-memory stores.
type authFixture struct {
	apps   *memstore.Apps
	keys   *memstore.APIKeys
	svc    *service.AppService
	nonces *memNonces
	// limiter is nil unless a test sets it; limits apply then.
	limiter *memstore.Failures
	limits  AuthLimits
	logs    *observer.ObservedLogs
	log     *zap.Logger
	// seen records what the handler behind AppAuth found in the context.
	seenApp *domain.App
	seenKey *domain.APIKey
}

func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	f := &authFixture{apps: memstore.NewApps(time.Now), nonces: newMemNonces(), logs: logs, log: zap.New(core)}
	f.keys = memstore.NewAPIKeys(f.apps, time.Now)
	f.svc = service.NewAppService(f.apps, f.keys, nil, prefixCipher{}, memstore.NewTx(f.apps, f.keys), nil)
	return f
}

func (f *authFixture) auth(resolver AppResolver, cipher SecretDecryptor) gin.HandlerFunc {
	deps := AppAuthDeps{Resolver: resolver, Cipher: cipher, Limits: f.limits, Log: f.log}
	if f.nonces != nil {
		deps.Nonces = f.nonces
	}
	if f.limiter != nil {
		deps.Limiter = f.limiter
	}
	return AppAuth(deps)
}

func (f *authFixture) engine(resolver AppResolver, cipher SecretDecryptor) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID(), Recover(zap.NewNop()))
	r.Any("/x", f.auth(resolver, cipher), func(c *gin.Context) {
		f.seenApp, _ = AppFromContext(c)
		f.seenKey, _ = APIKeyFromContext(c)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.Any("/y", f.auth(resolver, cipher), func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	return r
}

type credentials struct {
	app            *domain.App
	key            *domain.APIKey
	apiKey, secret string
}

// addKey stores an app (once per slug) and a key for it.
func (f *authFixture) addKey(t *testing.T, slug string, env domain.Environment, mutate func(*domain.APIKey)) credentials {
	t.Helper()
	ctx := context.Background()
	app, err := f.apps.GetBySlug(ctx, env, slug)
	if err != nil {
		app = &domain.App{PublicID: id.New(id.App), Slug: slug, Name: slug, Environment: env, Status: domain.AppActive}
		if err := f.apps.Create(ctx, app); err != nil {
			t.Fatal(err)
		}
	}
	raw, secret := keygen.APIKey(string(env)), keygen.APISecret(string(env))
	k := domain.APIKey{PublicID: id.New(id.APIKey), AppID: app.ID, Environment: env, Label: "k", Prefix: keygen.KeyPrefixFor(string(env)),
		Last4: raw[len(raw)-4:], Hash: keygen.HashAPIKey(raw), SecretEnc: "enc:" + secret}
	if mutate != nil {
		mutate(&k)
	}
	f.keys.Put(k)
	stored, _, _ := f.keys.GetByHashWithApp(ctx, k.Hash)
	return credentials{app: app, key: stored, apiKey: raw, secret: secret}
}

func signed(apiKey, secret, ts, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set("X-Signature", hmac.Sign(secret, []byte(ts+"."+body)))
	return req
}

func serve(r *gin.Engine, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func now() string { return strconv.FormatInt(time.Now().Unix(), 10) }

type failingResolver struct{ AppResolver }

func (failingResolver) Lookup(context.Context, string) (*domain.App, *domain.APIKey, error) {
	return nil, nil, errors.New("db down")
}

// Checks run in the order api/openapi.yaml documents for /v1. Key and app
// state are only reported to a caller that signed with the key's secret.
func TestAppAuthOrder(t *testing.T) {
	f := newAuthFixture(t)
	past := time.Now().Add(-time.Minute)
	good := f.addKey(t, "acme", domain.EnvLive, nil)
	revoked := f.addKey(t, "acme", domain.EnvLive, func(k *domain.APIKey) { k.RevokedAt = &past })
	expired := f.addKey(t, "acme", domain.EnvLive, func(k *domain.APIKey) { k.ExpiresAt = &past })
	disabled := f.addKey(t, "off", domain.EnvLive, nil)
	disabled.app.Status = domain.AppDisabled
	if err := f.apps.Update(context.Background(), disabled.app); err != nil {
		t.Fatal(err)
	}
	r := f.engine(f.svc, prefixCipher{})
	old := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	wrong := strings.Repeat("0", 64)

	missing := signed(good.apiKey, good.secret, now(), `{}`)
	missing.Header.Del("X-Signature")
	badSig := func(c credentials) *http.Request {
		req := signed(c.apiKey, c.secret, now(), `{}`)
		req.Header.Set("X-Signature", wrong)
		return req
	}

	cases := []struct {
		name   string
		req    *http.Request
		status int
		code   domain.ErrorCode
	}{
		{"missing headers", missing, 401, domain.CodeMissingSignatureHeaders},
		{"invalid timestamp", signed(good.apiKey, good.secret, "yesterday", `{}`), 401, domain.CodeInvalidTimestamp},
		{"timestamp out of range, even for a revoked key", signed(revoked.apiKey, revoked.secret, old, `{}`), 401, domain.CodeTimestampOutOfRange},
		{"unknown api key", signed("kp_live_"+strings.Repeat("9", 32), good.secret, now(), `{}`), 401, domain.CodeUnknownAPIKey},
		{"revoked key, bad signature", badSig(revoked), 401, domain.CodeInvalidSignature},
		{"expired key, bad signature", badSig(expired), 401, domain.CodeInvalidSignature},
		{"disabled app, bad signature", badSig(disabled), 401, domain.CodeInvalidSignature},
		{"another key's secret", signed(good.apiKey, revoked.secret, now(), `{}`), 401, domain.CodeInvalidSignature},
		{"revoked key", signed(revoked.apiKey, revoked.secret, now(), `{}`), 401, domain.CodeAPIKeyRevoked},
		{"expired key", signed(expired.apiKey, expired.secret, now(), `{}`), 401, domain.CodeAPIKeyExpired},
		{"disabled app", signed(disabled.apiKey, disabled.secret, now(), `{}`), 403, domain.CodeTenantInactive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertEnvelope(t, serve(r, tc.req), tc.status, tc.code)
		})
	}
	if f.keys.Touches != 0 {
		t.Fatalf("refused requests touched keys: %d", f.keys.Touches)
	}

	// Re-enabling the app restores access.
	disabled.app.Status = domain.AppActive
	_ = f.apps.Update(context.Background(), disabled.app)
	if w := serve(r, signed(disabled.apiKey, disabled.secret, now(), `{}`)); w.Code != http.StatusOK {
		t.Fatalf("re-enabled: %d %s", w.Code, w.Body.String())
	}

	assertEnvelope(t, serve(f.engine(failingResolver{}, prefixCipher{}), signed(good.apiKey, good.secret, now(), `{}`)), 500, domain.CodeInternal)
	w := serve(f.engine(f.svc, prefixCipher{err: errors.New("bad key material")}), signed(good.apiKey, good.secret, now(), `{}`))
	assertEnvelope(t, w, 500, domain.CodeInternal)
	if strings.Contains(w.Body.String(), "bad key") {
		t.Fatalf("internal error text leaked: %s", w.Body.String())
	}
}

// A body over the route's limit is refused as 413 whatever its signature,
// without an auth security event: the limit is not an authentication failure.
func TestAppAuthBodyOverLimitIs413(t *testing.T) {
	f := newAuthFixture(t)
	c := f.addKey(t, "acme", domain.EnvLive, nil)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID(), Recover(zap.NewNop()), MaxBody(16))
	r.POST("/x", f.auth(f.svc, prefixCipher{}), func(c *gin.Context) {
		f.seenApp, _ = AppFromContext(c)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	req := signed(c.apiKey, c.secret, now(), `{"note":"`+strings.Repeat("x", 32)+`"}`)
	req.ContentLength = -1 // undeclared, so the limit trips inside AppAuth's read
	assertEnvelope(t, serve(r, req), 413, domain.CodeRequestTooLarge)
	if f.seenApp != nil {
		t.Fatal("the handler ran")
	}
	if n := f.logs.FilterField(zap.String("log_type", "security")).Len(); n != 0 {
		t.Fatalf("security events = %d, want none", n)
	}
}

// A replayed signature is refused and does not touch the key again; the app
// and key reach the handler.
func TestAppAuthReplayAndContext(t *testing.T) {
	f := newAuthFixture(t)
	c := f.addKey(t, "acme", domain.EnvTest, nil)
	r := f.engine(f.svc, prefixCipher{})
	ts := now()

	if w := serve(r, signed(c.apiKey, c.secret, ts, `{"a":1}`)); w.Code != http.StatusOK {
		t.Fatalf("first: %d %s", w.Code, w.Body.String())
	}
	if f.seenApp == nil || f.seenApp.ID != c.app.ID || f.seenKey == nil || f.seenKey.PublicID != c.key.PublicID {
		t.Fatalf("context = %+v %+v", f.seenApp, f.seenKey)
	}
	if f.keys.Touches != 1 || f.keys.Get(c.key.PublicID).LastUsedAt == nil {
		t.Fatalf("touches = %d", f.keys.Touches)
	}
	for _, k := range []string{"nonce:" + c.key.Hash + ":" + hmac.Sign(c.secret, []byte(ts+`.{"a":1}`))} {
		if !f.nonces.seen[k] {
			t.Fatalf("nonce %q not recorded: %v", k, f.nonces.seen)
		}
	}
	assertEnvelope(t, serve(r, signed(c.apiKey, c.secret, ts, `{"a":1}`)), 401, domain.CodeReplayedRequest)
	if f.keys.Touches != 1 {
		t.Fatalf("replay touched the key: %d", f.keys.Touches)
	}
}

// A failing touch is logged and never fails the request.
func TestAppAuthTouchFailureIsLogged(t *testing.T) {
	f := newAuthFixture(t)
	c := f.addKey(t, "acme", domain.EnvLive, nil)
	f.keys.TouchErr = errors.New("db down")
	if w := serve(f.engine(f.svc, prefixCipher{}), signed(c.apiKey, c.secret, now(), `{}`)); w.Code != http.StatusOK {
		t.Fatalf("status %d %s", w.Code, w.Body.String())
	}
	if n := f.logs.FilterField(zap.String("event", "api_key.touch_failed")).Len(); n != 1 {
		t.Fatalf("touch_failed logs = %d", n)
	}
}

// A key whose row or header environment disagrees with its app looks unknown
// to the caller and is a high-severity security event.
func TestAppAuthEnvironmentMismatch(t *testing.T) {
	f := newAuthFixture(t)
	rowMismatch := f.addKey(t, "acme", domain.EnvLive, func(k *domain.APIKey) {
		k.Environment = domain.EnvTest
		k.Prefix = "kp_test_"
	})
	live := f.addKey(t, "acme", domain.EnvLive, nil)
	headerMismatch := "kp_test_" + strings.Repeat("3", 32)
	f.keys.Put(domain.APIKey{PublicID: id.New(id.APIKey), AppID: live.app.ID, Environment: domain.EnvLive, Prefix: "kp_live_",
		Last4: "3333", Hash: keygen.HashAPIKey(headerMismatch), SecretEnc: "enc:" + live.secret})
	r := f.engine(f.svc, prefixCipher{})

	for _, req := range []*http.Request{
		signed(rowMismatch.apiKey, rowMismatch.secret, now(), `{}`),
		signed(headerMismatch, live.secret, now(), `{}`),
	} {
		assertEnvelope(t, serve(r, req), 401, domain.CodeUnknownAPIKey)
	}
	events := f.logs.FilterField(zap.String("event", "auth.api_key_env_mismatch")).All()
	if len(events) != 2 || events[0].Level != zapcore.ErrorLevel || events[0].ContextMap()["severity"] != "high" {
		t.Fatalf("events = %+v", events)
	}
	// A legacy kp_ key without an environment works in its app's environment.
	legacy := "kp_" + strings.Repeat("ab", 16)
	f.keys.Put(domain.APIKey{PublicID: id.New(id.APIKey), AppID: live.app.ID, Environment: domain.EnvLive, Prefix: "kp_",
		Last4: "abab", Hash: keygen.HashAPIKey(legacy), SecretEnc: "enc:legacy-secret"})
	if w := serve(r, signed(legacy, "legacy-secret", now(), `{}`)); w.Code != http.StatusOK {
		t.Fatalf("legacy key: %d %s", w.Code, w.Body.String())
	}
}

// Security logs name the app and key by public id and hint, never the full
// key, its hash, the secret or the body.
func TestAppAuthLogsNoSecrets(t *testing.T) {
	f := newAuthFixture(t)
	past := time.Now().Add(-time.Minute)
	c := f.addKey(t, "acme", domain.EnvLive, func(k *domain.APIKey) { k.RevokedAt = &past })
	r := f.engine(f.svc, prefixCipher{})
	body := `{"card":"secret-body"}`
	bad := signed(c.apiKey, c.secret, now(), body)
	bad.Header.Set("X-Signature", strings.Repeat("1", 64))
	serve(r, bad)
	serve(r, signed(c.apiKey, c.secret, now(), body))

	events := []string{"auth.bad_signature", "auth.api_key_revoked"}
	for _, event := range events {
		entries := f.logs.FilterField(zap.String("event", event)).All()
		if len(entries) != 1 {
			t.Fatalf("%s: %d entries", event, len(entries))
		}
		fields := entries[0].ContextMap()
		if fields["app_id"] != c.app.PublicID || fields["api_key_id"] != c.key.PublicID || fields["api_key_hint"] != "kp_live_…"+c.key.Last4 {
			t.Fatalf("%s fields = %v", event, fields)
		}
	}
	for _, e := range f.logs.All() {
		line := e.Message
		for k, v := range e.ContextMap() {
			line += " " + k + "=" + toString(v)
		}
		for _, secret := range []string{c.apiKey, c.secret, c.key.Hash, "secret-body"} {
			if strings.Contains(line, secret) {
				t.Fatalf("log line leaks %q: %s", secret[:6], line)
			}
		}
	}
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// signedV2 signs a request the v2 way: timestamp, client nonce, method and
// path are all covered.
func signedV2(method, target, apiKey, secret, ts, nonce, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set("X-Signature-Version", SignatureV2)
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Signature", hmac.Sign(secret, SigningInput(SignatureV2, ts, nonce, method, target, []byte(body))))
	return req
}

func TestSigningInput(t *testing.T) {
	if got := string(SigningInput(SignatureV1, "17", "n", "POST", "/x", []byte(`{}`))); got != "17.{}" {
		t.Fatalf("v1 = %q", got)
	}
	if got := string(SigningInput(SignatureV2, "17", "nonce-value-0001", "GET", "/v1/payments/pay_1", nil)); got != "17.nonce-value-0001.GET./v1/payments/pay_1." {
		t.Fatalf("v2 = %q", got)
	}
}

// Replay protection is not optional: without an answer from the nonce
// store the request is refused, and nothing else of it happens.
func TestAppAuthNonceStoreDownIs503(t *testing.T) {
	f := newAuthFixture(t)
	c := f.addKey(t, "acme", domain.EnvLive, nil)
	f.nonces.err = errors.New("redis down")
	r := f.engine(f.svc, prefixCipher{})
	w := serve(r, signed(c.apiKey, c.secret, now(), `{}`))
	assertEnvelope(t, w, 503, domain.CodeAuthUnavailable)
	if strings.Contains(w.Body.String(), "redis down") {
		t.Fatalf("cause leaked: %s", w.Body.String())
	}
	if f.seenApp != nil || f.keys.Touches != 0 {
		t.Fatalf("request went on: app %v, touches %d", f.seenApp, f.keys.Touches)
	}
	events := f.logs.FilterField(zap.String("event", "auth.store_unavailable")).All()
	if len(events) != 1 || events[0].ContextMap()["app_id"] != c.app.PublicID {
		t.Fatalf("events = %+v", events)
	}

	f.nonces = nil
	assertEnvelope(t, serve(f.engine(f.svc, prefixCipher{}), signed(c.apiKey, c.secret, now(), `{}`)), 503, domain.CodeAuthUnavailable)
}

// Version 2 covers method and path and takes a client nonce, so a captured
// signature fits no other request and identical requests do not collide.
func TestAppAuthSignatureV2(t *testing.T) {
	f := newAuthFixture(t)
	c := f.addKey(t, "acme", domain.EnvLive, nil)
	r := f.engine(f.svc, prefixCipher{})
	ts := now()
	nonce := func(i int) string { return "nonce-" + strconv.Itoa(i) + strings.Repeat("0", 16) }

	t.Run("authenticates and records the nonce", func(t *testing.T) {
		if w := serve(r, signedV2(http.MethodPost, "/x", c.apiKey, c.secret, ts, nonce(1), `{"a":1}`)); w.Code != http.StatusOK {
			t.Fatalf("v2 post: %d %s", w.Code, w.Body.String())
		}
		if f.seenKey == nil || f.seenKey.PublicID != c.key.PublicID {
			t.Fatalf("context key = %+v", f.seenKey)
		}
		if !f.nonces.seen["nonce:"+c.key.Hash+":"+nonce(1)] {
			t.Fatalf("v2 nonce not recorded: %v", f.nonces.seen)
		}
	})

	t.Run("identical requests in one second collide only in v1", func(t *testing.T) {
		v1 := func() *http.Request {
			req := signed(c.apiKey, c.secret, ts, "")
			req.Method = http.MethodGet
			return req
		}
		if w := serve(r, v1()); w.Code != http.StatusOK {
			t.Fatalf("v1 get: %d %s", w.Code, w.Body.String())
		}
		assertEnvelope(t, serve(r, v1()), 401, domain.CodeReplayedRequest)
		for i := 2; i < 4; i++ {
			if w := serve(r, signedV2(http.MethodGet, "/x", c.apiKey, c.secret, ts, nonce(i), "")); w.Code != http.StatusOK {
				t.Fatalf("v2 get %d: %d %s", i, w.Code, w.Body.String())
			}
		}
	})

	t.Run("ties the signature to method, path and nonce", func(t *testing.T) {
		moved := signedV2(http.MethodGet, "/x", c.apiKey, c.secret, ts, nonce(4), "")
		moved.URL.Path = "/y"
		assertEnvelope(t, serve(r, moved), 401, domain.CodeInvalidSignature)
		changed := signedV2(http.MethodGet, "/x", c.apiKey, c.secret, ts, nonce(5), "")
		changed.Method = http.MethodPost
		assertEnvelope(t, serve(r, changed), 401, domain.CodeInvalidSignature)
		assertEnvelope(t, serve(r, signedV2(http.MethodGet, "/x", c.apiKey, c.secret, ts, nonce(2), "")), 401, domain.CodeReplayedRequest)
	})

	t.Run("header rules", func(t *testing.T) {
		noNonce := signedV2(http.MethodGet, "/x", c.apiKey, c.secret, ts, nonce(6), "")
		noNonce.Header.Del("X-Nonce")
		assertEnvelope(t, serve(r, noNonce), 401, domain.CodeMissingSignatureHeaders)
		for _, bad := range []string{"short", strings.Repeat("n", 65), "has space" + strings.Repeat("x", 10), "tab\there" + strings.Repeat("x", 10)} {
			w := serve(r, signedV2(http.MethodGet, "/x", c.apiKey, c.secret, ts, bad, ""))
			assertEnvelope(t, w, 400, domain.CodeInvalidRequest)
			if !strings.Contains(w.Body.String(), `"field":"X-Nonce"`) {
				t.Fatalf("%q: %s", bad, w.Body.String())
			}
		}
		v3 := signedV2(http.MethodGet, "/x", c.apiKey, c.secret, ts, nonce(7), "")
		v3.Header.Set("X-Signature-Version", "3")
		w := serve(r, v3)
		assertEnvelope(t, w, 400, domain.CodeInvalidRequest)
		if !strings.Contains(w.Body.String(), `"field":"X-Signature-Version"`) {
			t.Fatalf("version: %s", w.Body.String())
		}
		explicit := signed(c.apiKey, c.secret, now(), `{"v":1}`)
		explicit.Header.Set("X-Signature-Version", "1")
		if w := serve(r, explicit); w.Code != http.StatusOK {
			t.Fatalf("explicit v1: %d %s", w.Code, w.Body.String())
		}
	})
}

// withLimits turns the failure limits on for a fixture.
func (f *authFixture) withLimits(perIP, perKey int) {
	f.limiter = memstore.NewFailures(time.Now)
	f.limits = AuthLimits{MaxFailuresPerIP: perIP, MaxFailuresPerKey: perKey, Window: 15 * time.Minute}
}

// Only failed authentications count: unknown keys against the sender's IP,
// bad signatures and replays against the IP and the key. At the limit the
// caller gets 429 before the key or its signature is looked at.
func TestAppAuthFailureLimits(t *testing.T) {
	f := newAuthFixture(t)
	f.withLimits(3, 2)
	c := f.addKey(t, "acme", domain.EnvLive, nil)
	r := f.engine(f.svc, prefixCipher{})
	wrong := strings.Repeat("0", 64)
	ipKey, keyKey := "ip:192.0.2.1", "key:"+c.key.Hash+":192.0.2.1"
	badSig := func() *http.Request {
		req := signed(c.apiKey, c.secret, now(), `{}`)
		req.Header.Set("X-Signature", wrong)
		return req
	}

	// Successes never count, however many.
	for i := 0; i < 10; i++ {
		if w := serve(r, signed(c.apiKey, c.secret, now(), `{"i":`+strconv.Itoa(i)+`}`)); w.Code != http.StatusOK {
			t.Fatalf("success %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	// Nor do refusals that only the secret's holder can get.
	past := time.Now().Add(-time.Minute)
	revoked := f.addKey(t, "acme", domain.EnvLive, func(k *domain.APIKey) { k.RevokedAt = &past })
	assertEnvelope(t, serve(r, signed(revoked.apiKey, revoked.secret, now(), `{}`)), 401, domain.CodeAPIKeyRevoked)
	if f.limiter.Value(ipKey) != 0 || f.limiter.Value(keyKey) != 0 {
		t.Fatalf("counted %d/%d after successes and a revoked key", f.limiter.Value(ipKey), f.limiter.Value(keyKey))
	}

	assertEnvelope(t, serve(r, signed("kp_live_"+strings.Repeat("9", 32), c.secret, now(), `{}`)), 401, domain.CodeUnknownAPIKey)
	if f.limiter.Value(ipKey) != 1 || f.limiter.Value(keyKey) != 0 {
		t.Fatalf("unknown key: ip %d, key %d", f.limiter.Value(ipKey), f.limiter.Value(keyKey))
	}
	assertEnvelope(t, serve(r, badSig()), 401, domain.CodeInvalidSignature)
	if f.limiter.Value(ipKey) != 2 || f.limiter.Value(keyKey) != 1 {
		t.Fatalf("bad signature: ip %d, key %d", f.limiter.Value(ipKey), f.limiter.Value(keyKey))
	}
	ts := now()
	serve(r, signed(c.apiKey, c.secret, ts, `{"r":1}`))
	assertEnvelope(t, serve(r, signed(c.apiKey, c.secret, ts, `{"r":1}`)), 401, domain.CodeReplayedRequest)
	if f.limiter.Value(ipKey) != 3 || f.limiter.Value(keyKey) != 2 {
		t.Fatalf("replay: ip %d, key %d", f.limiter.Value(ipKey), f.limiter.Value(keyKey))
	}

	// Both limits are reached: even a correct request is refused, with a
	// Retry-After, and the key is not touched.
	touches := f.keys.Touches
	w := serve(r, signed(c.apiKey, c.secret, now(), `{}`))
	assertEnvelope(t, w, 429, domain.CodeRateLimited)
	if w.Header().Get("Retry-After") == "" || f.keys.Touches != touches {
		t.Fatalf("blocked: Retry-After %q, touches %d", w.Header().Get("Retry-After"), f.keys.Touches-touches)
	}
	if n := f.logs.FilterField(zap.String("event", "auth.rate_limited")).Len(); n != 1 {
		t.Fatalf("rate_limited events = %d", n)
	}
	// A blocked IP does not learn whether a key exists.
	assertEnvelope(t, serve(r, signed("kp_live_"+strings.Repeat("8", 32), c.secret, now(), `{}`)), 429, domain.CodeRateLimited)
}

// The key counter is per sender: a stranger who knows an app's key but not
// its secret exhausts only their own allowance, and the app itself keeps
// working from its own address. A blocked sender is refused before the
// signature is checked.
func TestAppAuthKeyLimitIsPerSender(t *testing.T) {
	f := newAuthFixture(t)
	f.withLimits(100, 1)
	c := f.addKey(t, "acme", domain.EnvLive, nil)
	r := f.engine(f.svc, prefixCipher{})
	bad := signed(c.apiKey, c.secret, now(), `{}`)
	bad.Header.Set("X-Signature", strings.Repeat("0", 64))
	assertEnvelope(t, serve(r, bad), 401, domain.CodeInvalidSignature)
	assertEnvelope(t, serve(r, signed(c.apiKey, c.secret, now(), `{}`)), 429, domain.CodeRateLimited)
	if n := f.logs.FilterField(zap.String("event", "auth.bad_signature")).Len(); n != 1 {
		t.Fatalf("bad_signature events = %d, the blocked request was verified", n)
	}
	elsewhere := signed(c.apiKey, c.secret, now(), `{}`)
	elsewhere.RemoteAddr = "198.51.100.7:1234"
	if w := serve(r, elsewhere); w.Code != http.StatusOK {
		t.Fatalf("the app's own request from another address: %d %s", w.Code, w.Body.String())
	}
}

func TestAppAuthLimiterDownIs503(t *testing.T) {
	f := newAuthFixture(t)
	f.withLimits(3, 2)
	f.limiter.Err = errors.New("redis down")
	c := f.addKey(t, "acme", domain.EnvLive, nil)
	assertEnvelope(t, serve(f.engine(f.svc, prefixCipher{}), signed(c.apiKey, c.secret, now(), `{}`)), 503, domain.CodeAuthUnavailable)
}
