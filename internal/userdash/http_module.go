package userdash

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/platform/httproute"
	"gpt-load/internal/platform/i18n"
	"gpt-load/internal/platform/response"
	"gpt-load/internal/platform/utils"
)

// ContextProfileKey is the gin context key under which SessionAuth stores the
// authenticated Profile.
const ContextProfileKey = "userdash.profile"

const (
	rateFailureWindow   = 15 * time.Minute
	rateFailureLimit    = 10
	rateLockDuration    = 15 * time.Minute
	rateCleanupInterval = time.Minute
)

type rateEntry struct {
	failures    []time.Time
	lockedUntil time.Time
}

// failureLimiter is a minimal per-IP failure limiter shared by login+signup,
// mirroring control's authFailureLimiter semantics.
type failureLimiter struct {
	mu          sync.Mutex
	entries     map[string]rateEntry
	now         func() time.Time
	lastCleanup time.Time
}

func newFailureLimiter(now func() time.Time) *failureLimiter {
	if now == nil {
		now = time.Now
	}
	return &failureLimiter{entries: make(map[string]rateEntry), now: now}
}

func (l *failureLimiter) check(peer string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.cleanup(now)
	entry := l.entries[peer]
	if now.Before(entry.lockedUntil) {
		return entry.lockedUntil.Sub(now), true
	}
	return 0, false
}

func (l *failureLimiter) recordFailure(peer string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	entry := l.entries[peer]
	if now.Before(entry.lockedUntil) {
		return
	}
	entry.failures = retainRecent(entry.failures, now.Add(-rateFailureWindow))
	entry.failures = append(entry.failures, now)
	if len(entry.failures) >= rateFailureLimit {
		entry.failures = nil
		entry.lockedUntil = now.Add(rateLockDuration)
	}
	l.entries[peer] = entry
}

func (l *failureLimiter) reset(peer string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, peer)
}

func (l *failureLimiter) cleanup(now time.Time) {
	if !l.lastCleanup.IsZero() && now.Before(l.lastCleanup.Add(rateCleanupInterval)) {
		return
	}
	l.lastCleanup = now
	cutoff := now.Add(-rateFailureWindow)
	for peer, entry := range l.entries {
		if !entry.lockedUntil.IsZero() {
			if !now.Before(entry.lockedUntil) {
				delete(l.entries, peer)
			}
			continue
		}
		entry.failures = retainRecent(entry.failures, cutoff)
		if len(entry.failures) == 0 {
			delete(l.entries, peer)
			continue
		}
		l.entries[peer] = entry
	}
}

func retainRecent(values []time.Time, cutoff time.Time) []time.Time {
	retained := values[:0]
	for _, value := range values {
		if value.After(cutoff) {
			retained = append(retained, value)
		}
	}
	return retained
}

// HTTPModule serves the client-area REST surface (signup/login/logout/me/models).
type HTTPModule struct {
	service        *Service
	modelsProvider func(ctx context.Context) (any, error)
	failures       *failureLimiter
}

// NewHTTPModule wires the client-area HTTP module. modelsProvider may be nil
// only if GET /models is never called; the container injects the landing
// models query. The logger parameter is kept for signature stability with
// sibling modules; logging uses the standard logrus logger.
func NewHTTPModule(
	service *Service,
	_ *logrus.Logger,
	modelsProvider func(ctx context.Context) (any, error),
) *HTTPModule {
	now := time.Now
	if service != nil && service.now != nil {
		now = service.now
	}
	return &HTTPModule{
		service:        service,
		modelsProvider: modelsProvider,
		failures:       newFailureLimiter(now),
	}
}

// Module declares the client-area routes with in-module session auth.
func (m *HTTPModule) Module() httproute.Module {
	return httproute.Module{
		Name:              "clientarea",
		Owner:             httproute.OwnerWeb,
		Auth:              httproute.AuthNone,
		Prefix:            "/api/clientarea",
		NamespacePrefixes: []string{"/api/clientarea"},
		BeforeAuth:        gin.HandlersChain{i18n.Middleware()},
		Routes:            m.routes(),
	}
}

func (m *HTTPModule) routes() []httproute.Route {
	routes := []httproute.Route{
		{
			Name:     "clientarea.signup",
			Methods:  []string{http.MethodPost},
			Path:     "/signup",
			Handlers: gin.HandlersChain{m.handleSignup},
		},
		{
			Name:     "clientarea.login",
			Methods:  []string{http.MethodPost},
			Path:     "/login",
			Handlers: gin.HandlersChain{m.handleLogin},
		},
		{
			Name:     "clientarea.logout",
			Methods:  []string{http.MethodPost},
			Path:     "/logout",
			Handlers: gin.HandlersChain{m.sessionAuth(), m.handleLogout},
		},
		{
			Name:     "clientarea.me",
			Methods:  []string{http.MethodGet},
			Path:     "/me",
			Handlers: gin.HandlersChain{m.sessionAuth(), m.handleMe},
		},
		{
			Name:     "clientarea.models",
			Methods:  []string{http.MethodGet},
			Path:     "/models",
			Handlers: gin.HandlersChain{m.sessionAuth(), m.handleModels},
		},
		{
			Name:     "clientarea.keys.list",
			Methods:  []string{http.MethodGet},
			Path:     "/keys",
			Handlers: gin.HandlersChain{m.sessionAuth(), m.handleListKeys},
		},
		{
			Name:     "clientarea.keys.create",
			Methods:  []string{http.MethodPost},
			Path:     "/keys",
			Handlers: gin.HandlersChain{m.sessionAuth(), m.handleCreateKey},
		},
		{
			Name:     "clientarea.keys.delete",
			Methods:  []string{http.MethodDelete},
			Path:     "/keys/:id",
			Handlers: gin.HandlersChain{m.sessionAuth(), m.handleDeleteKey},
		},
		{
			Name:     "clientarea.usage",
			Methods:  []string{http.MethodGet},
			Path:     "/usage",
			Handlers: gin.HandlersChain{m.sessionAuth(), m.handleUsage},
		},
	}
	return routes
}

func mapServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrValidation):
		response.Error(c, app_errors.ErrValidation)
	case errors.Is(err, ErrEmailTaken):
		response.Error(c, app_errors.ErrDuplicateResource)
	case errors.Is(err, ErrInvalidCredentials):
		response.Error(c, app_errors.ErrUnauthorized)
	case errors.Is(err, ErrUnauthenticated):
		response.Error(c, app_errors.ErrUnauthorized)
	case errors.Is(err, ErrUserDisabled):
		response.Error(c, app_errors.ErrForbidden)
	default:
		logrus.WithError(err).Error("userdash: internal error")
		response.Error(c, app_errors.ErrInternalServer)
	}
}

func (m *HTTPModule) sessionAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		fields := strings.Fields(c.GetHeader("Authorization"))
		if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
			response.Error(c, app_errors.ErrUnauthorized)
			c.Abort()
			return
		}
		profile, err := m.service.Authenticate(c.Request.Context(), fields[1])
		if err != nil {
			mapServiceError(c, err)
			c.Abort()
			return
		}
		c.Set(ContextProfileKey, profile)
		c.Next()
	}
}

type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func bindCredentials(c *gin.Context) (credentialsRequest, bool) {
	var request credentialsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.Error(c, app_errors.ErrInvalidJSON)
		return request, false
	}
	return request, true
}

func (m *HTTPModule) writeRateLimited(c *gin.Context, retryAfter time.Duration) {
	seconds := retryAfterSeconds(retryAfter)
	c.Header("Retry-After", strconv.FormatInt(seconds, 10))
	response.ErrorI18nFromAPIError(c, app_errors.NewAPIErrorWithData(
		app_errors.ErrAuthLocked,
		rateLockedData{RetryAfterSeconds: seconds},
	), "auth.locked")
}

func retryAfterSeconds(remaining time.Duration) int64 {
	seconds := int64((remaining + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

type rateLockedData struct {
	RetryAfterSeconds int64 `json:"retry_after_seconds"`
}

func (m *HTTPModule) rateLimitPeer(c *gin.Context) (string, bool) {
	peer, err := utils.NormalizePeerIP(c.Request.RemoteAddr)
	if err != nil {
		response.Error(c, app_errors.ErrInternalServer)
		return "", true
	}
	if retryAfter, locked := m.failures.check(peer); locked {
		m.writeRateLimited(c, retryAfter)
		return peer, true
	}
	return peer, false
}

func (m *HTTPModule) handleSignup(c *gin.Context) {
	peer, limited := m.rateLimitPeer(c)
	if limited {
		return
	}
	request, ok := bindCredentials(c)
	if !ok {
		return
	}
	profile, err := m.service.Signup(c.Request.Context(), request.Email, request.Password)
	if err != nil {
		if errors.Is(err, ErrValidation) || errors.Is(err, ErrEmailTaken) {
			m.failures.recordFailure(peer)
		}
		mapServiceError(c, err)
		return
	}
	m.failures.reset(peer)
	response.SuccessI18n(c, "common.success", profile)
}

func (m *HTTPModule) handleLogin(c *gin.Context) {
	peer, limited := m.rateLimitPeer(c)
	if limited {
		return
	}
	request, ok := bindCredentials(c)
	if !ok {
		return
	}
	token, profile, expiresAt, err := m.service.Login(c.Request.Context(), request.Email, request.Password)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			m.failures.recordFailure(peer)
		}
		mapServiceError(c, err)
		return
	}
	m.failures.reset(peer)
	response.SuccessI18n(c, "common.success", gin.H{
		"token":         token,
		"profile":       profile,
		"expires_at_ms": expiresAt.UnixMilli(),
	})
}

func (m *HTTPModule) handleLogout(c *gin.Context) {
	fields := strings.Fields(c.GetHeader("Authorization"))
	if err := m.service.Logout(c.Request.Context(), fields[1]); err != nil {
		mapServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (m *HTTPModule) handleMe(c *gin.Context) {
	profile := c.MustGet(ContextProfileKey).(Profile)
	response.SuccessI18n(c, "common.success", profile)
}

func (m *HTTPModule) handleModels(c *gin.Context) {
	if m.modelsProvider == nil {
		response.Error(c, app_errors.ErrInternalServer)
		return
	}
	payload, err := m.modelsProvider(c.Request.Context())
	if err != nil {
		logrus.WithError(err).Error("userdash: models provider failed")
		response.Error(c, app_errors.ErrInternalServer)
		return
	}
	response.SuccessI18n(c, "common.success", payload)
}
