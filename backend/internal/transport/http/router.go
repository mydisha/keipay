// Package http wires the Gin engine, route groups, and middleware.
package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/handler"
	"github.com/mydisha/keipay/backend/internal/transport/http/middleware"
)

// Deps holds the constructed handlers and middleware dependencies for routing.
type Deps struct {
	Log *zap.Logger

	AppResolver     middleware.AppResolver
	NonceStore      middleware.NonceStore
	SecretDecryptor middleware.SecretDecryptor
	CallbackToken   string

	// Sessions resolves admin session cookies.
	Sessions middleware.Authenticator
	Cookies  middleware.CookieConfig
	// NewToken issues CSRF cookies; nil uses service.NewToken.
	NewToken middleware.TokenGenerator
	// TrustedProxies may set X-Forwarded-For; empty trusts none, so the client
	// IP is the TCP peer.
	TrustedProxies []string

	Payment *handler.PaymentHandler
	Webhook *handler.WebhookHandler
	Admin   *handler.AdminHandler
	Apps    *handler.AppHandler
	Auth    *handler.AuthHandler
	Users   *handler.UserHandler

	// FrontendDir is the admin console build (frontend/dist). Empty disables it.
	FrontendDir string
}

// Access levels of admin routes that need no permission.
const (
	AccessPublic        = "public"
	AccessAuthenticated = "authenticated"
)

// adminRoute is one /admin endpoint and who may call it. access is
// AccessPublic, AccessAuthenticated or a domain.Permission.
type adminRoute struct {
	method  string
	path    string
	access  string
	handler gin.HandlerFunc
	// duringPasswordChange lets a user with must_change_password through.
	duringPasswordChange bool
}

func public(method, path string, h gin.HandlerFunc) adminRoute {
	return adminRoute{method: method, path: path, access: AccessPublic, handler: h}
}

func authenticated(method, path string, h gin.HandlerFunc) adminRoute {
	return adminRoute{method: method, path: path, access: AccessAuthenticated, handler: h}
}

func permitted(method, path string, p domain.Permission, h gin.HandlerFunc) adminRoute {
	return adminRoute{method: method, path: path, access: string(p), handler: h}
}

func (r adminRoute) allowedDuringPasswordChange() adminRoute {
	r.duringPasswordChange = true
	return r
}

// adminRoutes is the single list of /admin endpoints (paths relative to
// /admin). Each access level must equal x-keipay-permission in
// api/openapi.yaml; a test compares them.
func adminRoutes(d Deps) []adminRoute {
	return []adminRoute{
		public(http.MethodGet, "/setup", d.Auth.SetupStatus),
		public(http.MethodPost, "/setup", d.Auth.Setup),
		public(http.MethodPost, "/login", d.Auth.Login),
		public(http.MethodPost, "/logout", d.Auth.Logout),
		authenticated(http.MethodGet, "/me", d.Auth.Me).allowedDuringPasswordChange(),
		authenticated(http.MethodPost, "/me/password", d.Auth.ChangePassword).allowedDuringPasswordChange(),

		permitted(http.MethodGet, "/users", domain.PermUsersManage, d.Users.List),
		permitted(http.MethodPost, "/users", domain.PermUsersManage, d.Users.Create),
		permitted(http.MethodGet, "/users/:id", domain.PermUsersManage, d.Users.Get),
		permitted(http.MethodPatch, "/users/:id", domain.PermUsersManage, d.Users.Update),
		permitted(http.MethodPost, "/users/:id/disable", domain.PermUsersManage, d.Users.Disable),
		permitted(http.MethodPost, "/users/:id/enable", domain.PermUsersManage, d.Users.Enable),
		permitted(http.MethodPost, "/users/:id/reset-password", domain.PermUsersManage, d.Users.ResetPassword),

		permitted(http.MethodGet, "/apps", domain.PermAppsRead, d.Apps.List),
		permitted(http.MethodPost, "/apps", domain.PermAppsWrite, d.Apps.Create),
		permitted(http.MethodGet, "/apps/:id", domain.PermAppsRead, d.Apps.Get),
		permitted(http.MethodPatch, "/apps/:id", domain.PermAppsWrite, d.Apps.Update),
		permitted(http.MethodPost, "/apps/:id/disable", domain.PermAppsWrite, d.Apps.Disable),
		permitted(http.MethodPost, "/apps/:id/enable", domain.PermAppsWrite, d.Apps.Enable),
		permitted(http.MethodGet, "/apps/:id/keys", domain.PermAppsRead, d.Apps.ListKeys),
		permitted(http.MethodPost, "/apps/:id/keys", domain.PermAppsWrite, d.Apps.CreateKey),
		permitted(http.MethodPost, "/apps/:id/keys/:key_id/rotate", domain.PermAppsWrite, d.Apps.RotateKey),
		permitted(http.MethodPost, "/apps/:id/keys/:key_id/revoke", domain.PermAppsWrite, d.Apps.RevokeKey),

		permitted(http.MethodGet, "/payments", domain.PermPaymentsRead, d.Admin.ListPayments),
		permitted(http.MethodGet, "/ledger", domain.PermPaymentsRead, d.Admin.ListLedger),
		permitted(http.MethodGet, "/webhook-deliveries", domain.PermDeliveriesRead, d.Admin.ListDeliveries),
		permitted(http.MethodPost, "/webhook-deliveries/:id/replay", domain.PermDeliveriesReplay, d.Admin.ReplayDelivery),

		permitted(http.MethodGet, "/audit-logs", domain.PermAuditRead, d.Admin.ListAuditLogs),
	}
}

// NewRouter builds the Gin engine with all routes mounted.
func NewRouter(d Deps) *gin.Engine {
	r := gin.New()
	if err := r.SetTrustedProxies(d.TrustedProxies); err != nil {
		// Config validates the list, so this only guards against misuse:
		// trusting nobody is the safe fallback.
		d.Log.Error("invalid trusted proxies; trusting none", zap.Error(err))
		_ = r.SetTrustedProxies(nil)
	}
	r.Use(middleware.RequestID(), middleware.AccessLog(d.Log), middleware.Recover(d.Log))

	r.GET("/healthz", handler.Health)

	// App-facing API: API key + HMAC + timestamp + nonce replay protection.
	v1 := r.Group("/v1")
	v1.Use(middleware.AppAuth(d.AppResolver, d.NonceStore, d.SecretDecryptor, d.Log))

	{
		v1.POST("/payments", d.Payment.Create)
		v1.GET("/payments/:id", d.Payment.Get)
	}

	// Inbound Xendit callbacks: verified by x-callback-token.
	wh := r.Group("/webhooks")
	wh.Use(middleware.XenditCallbackToken(d.CallbackToken, d.Log))

	{
		wh.POST("/xendit", d.Webhook.Xendit)
	}

	// Admin/UI API: cookie session, CSRF on unsafe methods, permission per route.
	newToken := d.NewToken
	if newToken == nil {
		newToken = service.NewToken
	}
	admin := r.Group("/admin")
	admin.Use(
		middleware.AdminContext(d.Cookies, newToken),
		middleware.LoadSession(d.Sessions, d.Cookies),
		middleware.CSRF(d.Log),
	)
	for _, rt := range adminRoutes(d) {
		admin.Handle(rt.method, rt.path, append(guards(rt, d.Log), rt.handler)...)
	}

	// Admin console: everything that is not an API route. Unknown API routes,
	// and every unknown route when no console is built, get the JSON not_found
	// envelope. A wrong method on a known path is a 404 too (gin does not
	// answer 405 unless HandleMethodNotAllowed is set). Exactly /webhooks is
	// the console's Webhooks page, so only the paths below it are reserved.
	if d.FrontendDir != "" {
		r.NoRoute(handler.SPA(d.FrontendDir, "/v1", "/webhooks/", "/admin", "/healthz"))
	} else {
		r.NoRoute(handler.NotFound)
	}

	return r
}

// guards returns the middleware that enforces a route's access level.
func guards(rt adminRoute, log *zap.Logger) []gin.HandlerFunc {
	if rt.access == AccessPublic {
		return nil
	}
	chain := []gin.HandlerFunc{middleware.RequireSession(log)}
	if !rt.duringPasswordChange {
		chain = append(chain, middleware.RequirePasswordChanged())
	}
	if rt.access != AccessAuthenticated {
		chain = append(chain, middleware.RequirePermission(domain.Permission(rt.access), log))
	}
	return chain
}
