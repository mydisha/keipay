package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/logger"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// Admin cookie and header names.
const (
	SessionCookie = "keipay_session"
	CSRFCookie    = "keipay_csrf"
	CSRFHeader    = "X-CSRF-Token"
)

const (
	ctxUser           = "admin_user"
	ctxSession        = "admin_session"
	ctxSessionToken   = "admin_session_token"
	ctxSessionExpired = "admin_session_expired"
)

// CookieConfig holds the attributes shared by the admin cookies.
type CookieConfig struct {
	Secure bool
}

// Authenticator resolves a session token to its user. It returns
// domain.ErrSessionExpired when the session cannot be used.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (*domain.User, *domain.Session, error)
}

// TokenGenerator returns a fresh random token.
type TokenGenerator func() (string, error)

// AdminContext puts an anonymous actor carrying the request origin in the
// request context and issues a keipay_csrf cookie when the request has none.
func AdminContext(cookies CookieConfig, newToken TokenGenerator) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(domain.WithActor(c.Request.Context(),
			domain.AnonymousActor(c.ClientIP(), c.Request.UserAgent(), c.GetString(ctxRequestID))))
		if v, err := c.Cookie(CSRFCookie); err != nil || v == "" {
			token, err := newToken()
			if err != nil {
				response.Error(c, err)
				return
			}
			SetCSRFCookie(c, cookies, token)
		}
		c.Next()
	}
}

// LoadSession resolves the keipay_session cookie, when present, to the
// signed-in user and makes them the request's actor. An unusable session is
// remembered for RequireSession and its cookie is cleared; the request goes on
// either way. Only infrastructure failures abort it.
func LoadSession(auth Authenticator, cookies CookieConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(SessionCookie)
		if err != nil || token == "" {
			c.Next()
			return
		}
		user, sess, err := auth.Authenticate(c.Request.Context(), token)
		if errors.Is(err, domain.ErrSessionExpired) {
			c.Set(ctxSessionExpired, true)
			ClearSessionCookie(c, cookies)
			c.Next()
			return
		}
		if err != nil {
			response.Error(c, err)
			return
		}
		c.Set(ctxUser, user)
		c.Set(ctxSession, sess)
		c.Set(ctxSessionToken, token)
		c.Request = c.Request.WithContext(domain.WithActor(c.Request.Context(),
			domain.UserActor(user.PublicID, c.ClientIP(), c.Request.UserAgent(), c.GetString(ctxRequestID))))
		c.Next()
	}
}

// CSRF requires X-CSRF-Token on unsafe methods to equal the keipay_csrf cookie
// and, when a session is loaded, the session's token.
func CSRF(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}
		header := c.GetHeader(CSRFHeader)
		cookie, _ := c.Cookie(CSRFCookie)
		reason := ""
		switch {
		case header == "":
			reason = "missing csrf header"
		case !service.SecureEqual(header, cookie):
			reason = "csrf header does not match cookie"
		default:
			if _, sess, ok := CurrentUser(c); ok && !service.SecureEqual(header, sess.CSRFToken) {
				reason = "csrf header does not match session"
			}
		}
		if reason != "" {
			logger.Security(log, logger.EventAdminCSRFFailed, logger.SeverityMedium,
				c.ClientIP(), c.GetString(ctxRequestID), reason, zap.String("route", c.FullPath()))
			response.Error(c, domain.ErrInvalidCSRFToken)
			return
		}
		c.Next()
	}
}

// RequireSession rejects requests without a valid session: session_required
// without a cookie, session_expired with an unusable one.
func RequireSession(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, _, ok := CurrentUser(c); ok {
			c.Next()
			return
		}
		ip, rid := c.ClientIP(), c.GetString(ctxRequestID)
		if c.GetBool(ctxSessionExpired) {
			logger.Security(log, logger.EventAdminSessionInvalid, logger.SeverityLow, ip, rid, "session expired or revoked",
				zap.String("route", c.FullPath()))
			response.Error(c, domain.ErrSessionExpired)
			return
		}
		logger.Security(log, logger.EventAdminSessionMissing, logger.SeverityLow, ip, rid, "no session cookie",
			zap.String("route", c.FullPath()))
		response.Error(c, domain.ErrSessionRequired)
	}
}

// RequirePasswordChanged blocks a user who must change their password. Use it
// only on routes that are not allowed while a change is pending.
func RequirePasswordChanged() gin.HandlerFunc {
	return func(c *gin.Context) {
		if u, _, ok := CurrentUser(c); ok && u.MustChangePassword {
			response.Error(c, domain.ErrPasswordChangeRequired)
			return
		}
		c.Next()
	}
}

// RequirePermission rejects users whose role lacks p.
func RequirePermission(p domain.Permission, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, _, ok := CurrentUser(c)
		if ok && u.Role.Can(p) {
			c.Next()
			return
		}
		userID := ""
		if ok {
			userID = u.PublicID
		}
		logger.Security(log, logger.EventAdminForbidden, logger.SeverityMedium, c.ClientIP(), c.GetString(ctxRequestID),
			"missing permission", zap.String("user_id", userID), zap.String("permission", string(p)),
			zap.String("route", c.Request.Method+" "+c.FullPath()))
		response.Error(c, domain.ErrForbidden)
	}
}

// CurrentUser returns the user and session loaded by LoadSession.
func CurrentUser(c *gin.Context) (*domain.User, *domain.Session, bool) {
	u, uok := c.Get(ctxUser)
	s, sok := c.Get(ctxSession)
	if !uok || !sok {
		return nil, nil, false
	}
	user, uok := u.(*domain.User)
	sess, sok := s.(*domain.Session)
	return user, sess, uok && sok
}

// SessionToken returns the token of the session loaded by LoadSession.
func SessionToken(c *gin.Context) string {
	return c.GetString(ctxSessionToken)
}

// SetSessionCookies sets keipay_session and keipay_csrf after sign-in, setup
// or a password change.
func SetSessionCookies(c *gin.Context, cookies CookieConfig, token, csrf string) {
	setCookie(c, &http.Cookie{Name: SessionCookie, Value: token, Path: "/", HttpOnly: true,
		Secure: cookies.Secure, SameSite: http.SameSiteLaxMode})
	SetCSRFCookie(c, cookies, csrf)
}

// SetCSRFCookie sets keipay_csrf, readable by the console's scripts.
func SetCSRFCookie(c *gin.Context, cookies CookieConfig, token string) {
	setCookie(c, &http.Cookie{Name: CSRFCookie, Value: token, Path: "/",
		Secure: cookies.Secure, SameSite: http.SameSiteLaxMode})
}

// ClearSessionCookie expires keipay_session.
func ClearSessionCookie(c *gin.Context, cookies CookieConfig) {
	setCookie(c, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: cookies.Secure, SameSite: http.SameSiteLaxMode})
}

// setCookie replaces any Set-Cookie for the same name already on the
// response, so a handler's cookie wins over one issued by middleware.
func setCookie(c *gin.Context, cookie *http.Cookie) {
	h := c.Writer.Header()
	prefix := cookie.Name + "="
	var kept []string
	for _, v := range h.Values("Set-Cookie") {
		if !strings.HasPrefix(v, prefix) {
			kept = append(kept, v)
		}
	}
	h.Del("Set-Cookie")
	for _, v := range kept {
		h.Add("Set-Cookie", v)
	}
	http.SetCookie(c.Writer, cookie)
}
