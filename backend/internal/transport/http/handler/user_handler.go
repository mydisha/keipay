package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/dto"
	"github.com/mydisha/keipay/backend/internal/transport/http/middleware"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// UserHandler serves /admin/users. The router only lets users:manage through.
type UserHandler struct {
	users *service.UserService
}

// NewUserHandler constructs a UserHandler.
func NewUserHandler(users *service.UserService) *UserHandler {
	return &UserHandler{users: users}
}

// List handles GET /admin/users (cursor-paginated, newest first).
func (h *UserHandler) List(c *gin.Context) {
	req, err := pageRequest(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	page, err := h.users.List(c.Request.Context(), req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.UserListFromDomain(page))
}

// Create handles POST /admin/users.
func (h *UserHandler) Create(c *gin.Context) {
	var req dto.CreateUserRequest
	if err := bindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	u, password, err := h.users.Create(c.Request.Context(), service.CreateUserInput{
		Email: req.Email, Name: req.Name, Role: req.Role,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, dto.UserWithTemporaryPasswordResponse{User: dto.UserFromDomain(u), TemporaryPassword: password})
}

// Get handles GET /admin/users/{id}.
func (h *UserHandler) Get(c *gin.Context) {
	uid, err := pathUserID(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	u, err := h.users.Get(c.Request.Context(), uid)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.UserResponse{User: dto.UserFromDomain(u)})
}

// Update handles PATCH /admin/users/{id}.
func (h *UserHandler) Update(c *gin.Context) {
	uid, err := pathUserID(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	var req dto.UpdateUserRequest
	if err := bindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	u, err := h.users.Update(c.Request.Context(), uid, service.UpdateUserInput{
		Email: req.Email, Name: req.Name, Role: req.Role,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.UserResponse{User: dto.UserFromDomain(u)})
}

// Disable handles POST /admin/users/{id}/disable.
func (h *UserHandler) Disable(c *gin.Context) {
	h.act(c, func(uid string, actor *domain.User) (*domain.User, error) {
		return h.users.Disable(c.Request.Context(), uid, actor.PublicID)
	})
}

// Enable handles POST /admin/users/{id}/enable.
func (h *UserHandler) Enable(c *gin.Context) {
	h.act(c, func(uid string, _ *domain.User) (*domain.User, error) {
		return h.users.Enable(c.Request.Context(), uid)
	})
}

// ResetPassword handles POST /admin/users/{id}/reset-password.
func (h *UserHandler) ResetPassword(c *gin.Context) {
	uid, err := pathUserID(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	actor, _, ok := middleware.CurrentUser(c)
	if !ok {
		response.Error(c, domain.ErrSessionRequired)
		return
	}
	u, password, err := h.users.ResetPassword(c.Request.Context(), uid, actor.PublicID)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.UserWithTemporaryPasswordResponse{User: dto.UserFromDomain(u), TemporaryPassword: password})
}

// act runs a body-less action on the user named by the path.
func (h *UserHandler) act(c *gin.Context, fn func(uid string, actor *domain.User) (*domain.User, error)) {
	uid, err := pathUserID(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	actor, _, ok := middleware.CurrentUser(c)
	if !ok {
		response.Error(c, domain.ErrSessionRequired)
		return
	}
	u, err := fn(uid, actor)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, dto.UserResponse{User: dto.UserFromDomain(u)})
}
