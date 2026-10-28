// Package http wires the Gin engine, route groups, and middleware.
package http

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/transport/http/handler"
	"github.com/mydisha/keipay/backend/internal/transport/http/middleware"
)

// Deps holds the constructed handlers and middleware dependencies for routing.
type Deps struct {
	Log *zap.Logger

	TenantResolver  middleware.TenantResolver
	NonceStore      middleware.NonceStore
	SecretDecryptor middleware.SecretDecryptor
	JWTVerifier     middleware.JWTVerifier
	CallbackToken   string

	Payment *handler.PaymentHandler
	Webhook *handler.WebhookHandler
	Admin   *handler.AdminHandler

	// FrontendDir is the admin console build (frontend/dist). Empty disables it.
	FrontendDir string
}

// NewRouter builds the Gin engine with all routes mounted.
func NewRouter(d Deps) *gin.Engine {
	r := gin.New()
	r.Use(middleware.RequestID(), middleware.AccessLog(d.Log), middleware.Recover(d.Log))

	r.GET("/healthz", handler.Health)

	// Tenant-facing API: HMAC + timestamp + nonce replay protection.
	v1 := r.Group("/v1")
	v1.Use(middleware.TenantAuth(d.TenantResolver, d.NonceStore, d.SecretDecryptor, d.Log))

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

	// Admin/UI API.
	admin := r.Group("/admin")
	{
		admin.POST("/login", d.Admin.Login)

		auth := admin.Group("")
		auth.Use(middleware.AdminAuth(d.JWTVerifier, d.Log))

		{
			auth.POST("/change-password", d.Admin.ChangePassword)

			auth.POST("/tenants", d.Admin.CreateTenant)
			auth.GET("/tenants", d.Admin.ListTenants)
			auth.PUT("/tenants/:id", d.Admin.UpdateTenant)
			auth.POST("/tenants/:id/rotate", d.Admin.RotateTenantCredentials)

			auth.GET("/payments", d.Admin.ListPayments)
			auth.GET("/ledger", d.Admin.ListLedger)
			auth.GET("/webhook-deliveries", d.Admin.ListDeliveries)
			auth.POST("/webhook-deliveries/:id/replay", d.Admin.ReplayDelivery)
		}
	}

	// Admin console: everything that is not an API route.
	if d.FrontendDir != "" {
		r.NoRoute(handler.SPA(d.FrontendDir, "/v1", "/webhooks", "/admin", "/healthz"))
	}

	return r
}
