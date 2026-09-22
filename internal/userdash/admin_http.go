package userdash

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/platform/httproute"
	"gpt-load/internal/platform/i18n"
	"gpt-load/internal/platform/response"
)

// AdminHTTPModule serves the admin user-management surface under
// /api/admin/users. It reuses control's admin authenticator verbatim so the
// admin credential contract (digest comparison, peer lockout, access-key
// principal restrictions) is identical for both modules.
type AdminHTTPModule struct {
	service      *Service
	authenticate gin.HandlerFunc
}

// NewAdminHTTPModule wires the admin module. The authenticate handler is
// control's authenticate() middleware, injected by the container to avoid an
// import cycle; in tests it may be a stub.
func NewAdminHTTPModule(service *Service, authenticate gin.HandlerFunc) *AdminHTTPModule {
	return &AdminHTTPModule{service: service, authenticate: authenticate}
}

// Module declares the admin user routes with AuthControl policy.
func (m *AdminHTTPModule) Module() httproute.Module {
	route := func(name, method, path string, handlers ...gin.HandlerFunc) httproute.Route {
		return httproute.Route{
			Name:     name,
			Methods:  []string{method},
			Path:     path,
			Handlers: gin.HandlersChain(handlers),
		}
	}
	return httproute.Module{
		Name:              "adminusers",
		Owner:             httproute.OwnerControl,
		Auth:              httproute.AuthControl,
		Prefix:            "/api/admin",
		NamespacePrefixes: []string{"/api/admin"},
		BeforeAuth:        gin.HandlersChain{i18n.Middleware()},
		Authenticate:      m.authenticate,
		Routes: []httproute.Route{
			route("adminusers.users.list", http.MethodGet, "/users", m.handleListUsers),
			route("adminusers.users.create", http.MethodPost, "/users", m.handleCreateUser),
			route("adminusers.users.get", http.MethodGet, "/users/:id", m.handleGetUser),
			route("adminusers.users.patch", http.MethodPatch, "/users/:id", m.handlePatchUser),
			route("adminusers.adjustments.create", http.MethodPost, "/users/:id/adjustments", m.handleCreateAdjustment),
			route("adminusers.adjustments.list", http.MethodGet, "/users/:id/adjustments", m.handleListAdjustments),
		},
	}
}

func adminUserIDParam(c *gin.Context) (uint, bool) {
	parsed, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || parsed == 0 {
		response.Error(c, app_errors.ErrResourceNotFound)
		return 0, false
	}
	return uint(parsed), true
}

func (m *AdminHTTPModule) handleListUsers(c *gin.Context) {
	query, err := ParseAdminListQuery(c.Query("search"), c.Query("page"), c.Query("page_size"))
	if err != nil {
		response.Error(c, app_errors.ErrValidation)
		return
	}
	result, err := m.service.ListAdminUsers(c.Request.Context(), query)
	if err != nil {
		mapServiceError(c, err)
		return
	}
	response.SuccessI18n(c, "common.success", result)
}

type adminCreateUserRequest struct {
	Email          string `json:"email"`
	Password       string `json:"password"`
	MaxAPIKeys     int    `json:"max_api_keys"`
	BalanceNanoUSD int64  `json:"balance_nano_usd"`
}

type adminCreateUserResponse struct {
	Profile         AdminUserView `json:"profile"`
	InitialPassword string        `json:"initial_password,omitempty"`
}

func (m *AdminHTTPModule) handleCreateUser(c *gin.Context) {
	var request adminCreateUserRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.Error(c, app_errors.ErrInvalidJSON)
		return
	}
	profile, initialPassword, err := m.service.CreateAdminUser(c.Request.Context(), AdminCreateInput{
		Email:          request.Email,
		Password:       request.Password,
		MaxAPIKeys:     request.MaxAPIKeys,
		BalanceNanoUSD: request.BalanceNanoUSD,
	})
	if err != nil {
		mapServiceError(c, err)
		return
	}
	view, err := m.service.GetAdminUser(c.Request.Context(), profile.ID)
	if err != nil {
		mapServiceError(c, err)
		return
	}
	response.SuccessI18n(c, "common.success", adminCreateUserResponse{
		Profile:         view.AdminUserView,
		InitialPassword: initialPassword,
	})
}

type adminPatchUserRequest struct {
	Status     *string `json:"status"`
	MaxAPIKeys *int    `json:"max_api_keys"`
}

func (m *AdminHTTPModule) handlePatchUser(c *gin.Context) {
	id, ok := adminUserIDParam(c)
	if !ok {
		return
	}
	var request adminPatchUserRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.Error(c, app_errors.ErrInvalidJSON)
		return
	}
	view, err := m.service.PatchAdminUser(c.Request.Context(), id, AdminPatchInput{
		Status:     request.Status,
		MaxAPIKeys: request.MaxAPIKeys,
	})
	if err != nil {
		mapServiceError(c, err)
		return
	}
	response.SuccessI18n(c, "common.success", view)
}

func (m *AdminHTTPModule) handleGetUser(c *gin.Context) {
	id, ok := adminUserIDParam(c)
	if !ok {
		return
	}
	detail, err := m.service.GetAdminUser(c.Request.Context(), id)
	if err != nil {
		mapServiceError(c, err)
		return
	}
	response.SuccessI18n(c, "common.success", detail)
}

type adminAdjustmentRequest struct {
	DeltaNanoUSD int64  `json:"delta_nano_usd"`
	Reason       string `json:"reason"`
	Note         string `json:"note"`
}

func (m *AdminHTTPModule) handleCreateAdjustment(c *gin.Context) {
	id, ok := adminUserIDParam(c)
	if !ok {
		return
	}
	var request adminAdjustmentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.Error(c, app_errors.ErrInvalidJSON)
		return
	}
	if _, err := m.service.CreateAdminAdjustment(c.Request.Context(), id, AdminAdjustmentInput{
		DeltaNanoUSD: request.DeltaNanoUSD,
		Reason:       request.Reason,
		Note:         request.Note,
	}); err != nil {
		mapServiceError(c, err)
		return
	}
	view, err := m.service.GetAdminUser(c.Request.Context(), id)
	if err != nil {
		mapServiceError(c, err)
		return
	}
	response.SuccessI18n(c, "common.success", view.AdminUserView)
}

func (m *AdminHTTPModule) handleListAdjustments(c *gin.Context) {
	id, ok := adminUserIDParam(c)
	if !ok {
		return
	}
	page, err := ParseAdminListQuery("", c.Query("page"), c.Query("page_size"))
	if err != nil {
		response.Error(c, app_errors.ErrValidation)
		return
	}
	rows, err := m.service.ListAdminAdjustments(c.Request.Context(), id, AdminUserPage{
		Page:     page.Page,
		PageSize: page.PageSize,
	})
	if err != nil {
		mapServiceError(c, err)
		return
	}
	response.SuccessI18n(c, "common.success", rows)
}
