package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/dto"
	"github.com/mydisha/keipay/backend/internal/transport/http/middleware"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// AuthHandler serves first-run setup, sign-in, sign-out and the signed-in
// user's own account. Session and CSRF tokens only ever travel in cookies.
type AuthHandler struct {
	auth     *service.AuthService
	cookies  middleware.CookieConfig
	newToken middleware.TokenGenerator
}

// NewAuthHandler constructs an AuthHandler.
func NewAuthHandler(auth *service.AuthService, cookies middleware.CookieConfig, newToken middleware.TokenGenerator) *AuthHandler {
	return &AuthHandler{auth: auth, cookies: cookies, newToken: newToken}
}

// SetupStatus handles GET /admin/setup.
func (h *AuthHandler) SetupStatus(c *gin.Context) {
	st, err := h.auth.SetupStatus(c.Request.Context())
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.SetupStatusResponse{SetupRequired: st.Required, SetupTokenRequired: st.TokenRequired})
}

// Setup handles POST /admin/setup: creates the first owner and signs them in.
func (h *AuthHandler) Setup(c *gin.Context) {
	var req dto.SetupRequest
	if err := bindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	res, err := h.auth.Setup(c.Request.Context(), service.SetupInput{
		Email:      req.Email,
		Name:       req.Name,
		Password:   req.Password,
		SetupToken: req.SetupToken,
	}, clientMeta(c))
	if err != nil {
		response.Error(c, err)
		return
	}
	h.signedIn(c, http.StatusCreated, res)
}

// Login handles POST /admin/login.
func (h *AuthHandler) Login(c *gin.Context) {
	var req dto.LoginRequest
	if err := bindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	previous, _ := c.Cookie(middleware.SessionCookie)
	res, err := h.auth.Login(c.Request.Context(), service.LoginInput{
		Email:         req.Email,
		Password:      req.Password,
		PreviousToken: previous,
	}, clientMeta(c))
	if err != nil {
		response.Error(c, err)
		return
	}
	h.signedIn(c, http.StatusOK, res)
}

// Logout handles POST /admin/logout. It always answers 204 unless the
// session store or database fails.
func (h *AuthHandler) Logout(c *gin.Context) {
	token, _ := c.Cookie(middleware.SessionCookie)
	if err := h.auth.Logout(c.Request.Context(), token, clientMeta(c)); err != nil {
		response.Error(c, err)
		return
	}
	csrf, err := h.newToken()
	if err != nil {
		response.Error(c, err)
		return
	}
	middleware.ClearSessionCookie(c, h.cookies)
	middleware.SetCSRFCookie(c, h.cookies, csrf)
	c.Status(http.StatusNoContent)
}

// Me handles GET /admin/me.
func (h *AuthHandler) Me(c *gin.Context) {
	u, _, ok := middleware.CurrentUser(c)
	if !ok {
		response.Error(c, domain.ErrSessionRequired)
		return
	}
	response.Success(c, dto.MeFromDomain(u))
}

// ChangePassword handles POST /admin/me/password. The session is replaced, so
// the response sets new cookies.
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	u, _, ok := middleware.CurrentUser(c)
	if !ok {
		response.Error(c, domain.ErrSessionRequired)
		return
	}
	var req dto.ChangePasswordRequest
	if err := bindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	res, err := h.auth.ChangePassword(c.Request.Context(), service.ChangePasswordInput{
		User:            u,
		Token:           middleware.SessionToken(c),
		CurrentPassword: req.CurrentPassword,
		NewPassword:     req.NewPassword,
	}, clientMeta(c))
	if err != nil {
		response.Error(c, err)
		return
	}
	h.signedIn(c, http.StatusOK, res)
}

func (h *AuthHandler) signedIn(c *gin.Context, status int, res *service.AuthResult) {
	middleware.SetSessionCookies(c, h.cookies, res.Token, res.Session.CSRFToken)
	c.JSON(status, dto.MeFromDomain(res.User))
}
