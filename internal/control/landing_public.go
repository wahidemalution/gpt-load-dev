package control

import (
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"gpt-load/internal/platform/httproute"
	"gpt-load/internal/platform/i18n"
	"gpt-load/internal/platform/response"
	"gpt-load/internal/requestlog"
)

const landingPublicCacheTTL = 60 * time.Second

type landingPublicModelResponse struct {
	Name string `json:"name"`
}

type landingPublicStatsResponse struct {
	TotalRequests int64 `json:"total_requests"`
	TotalTokens   int64 `json:"total_tokens"`
}

type landingPublicResponse struct {
	Models []landingPublicModelResponse `json:"models"`
	Stats  landingPublicStatsResponse   `json:"stats"`
}

type landingPublicCache struct {
	mu        sync.Mutex
	payload   *landingPublicResponse
	expiresAt time.Time
}

// LandingPublicHTTPModule declares the unauthenticated landing-page surface.
func (s *Server) LandingPublicHTTPModule() httproute.Module {
	return httproute.Module{
		Name:              "landing-public",
		Owner:             httproute.OwnerWeb,
		Auth:              httproute.AuthNone,
		Prefix:            "/api/public",
		NamespacePrefixes: []string{"/api/public"},
		BeforeAuth:        gin.HandlersChain{i18n.Middleware()},
		NotFound:          controlRouteNotFound,
		MethodNotAllowed:  controlMethodNotAllowed,
		Routes: []httproute.Route{
			{
				Name:     "landing.public",
				Methods:  []string{http.MethodGet},
				Path:     "/landing",
				Handlers: gin.HandlersChain{s.handleLandingPublic},
			},
		},
	}
}

func (s *Server) handleLandingPublic(c *gin.Context) {
	now := s.now()
	if payload, ok := s.landingPublicCache.get(now); ok {
		response.SuccessI18n(c, "common.success", payload)
		return
	}

	ctx := c.Request.Context()
	models, err := s.service.ListProjectModels(ctx, ProjectModelListQuery{
		GroupStatus:   ProjectModelGroupStatusEnabled,
		PricingStatus: ProjectModelPricingStatusAll,
		Page:          1,
		PageSize:      maxProjectModelPageSize,
	})
	if err != nil {
		writeServiceError(c, "landing_public", err)
		return
	}
	// ponytail: only the first 100 models are exposed; add page loop when a
	// deployment serves more than 100 distinct client models.
	seen := make(map[string]struct{}, len(models.Items))
	names := make([]string, 0, len(models.Items))
	for _, item := range models.Items {
		if _, exists := seen[item.ClientModel]; exists {
			continue
		}
		seen[item.ClientModel] = struct{}{}
		names = append(names, item.ClientModel)
	}
	sort.Strings(names)

	nowMS, err := safeEpochMilliseconds(now)
	if err != nil {
		writeServiceError(c, "landing_public", err)
		return
	}
	report, err := s.service.QueryUsage(ctx, requestlog.UsageQuery{FromMS: 0, ToMS: nowMS})
	if err != nil {
		writeServiceError(c, "landing_public", err)
		return
	}
	aggregate, err := mapUsageAggregate(report.Summary)
	if err != nil {
		writeServiceError(c, "landing_public", err)
		return
	}

	payload := &landingPublicResponse{
		Models: make([]landingPublicModelResponse, 0, len(names)),
		Stats: landingPublicStatsResponse{
			TotalRequests: aggregate.RequestCount,
			TotalTokens:   aggregate.TotalTokens,
		},
	}
	for _, name := range names {
		payload.Models = append(payload.Models, landingPublicModelResponse{Name: name})
	}
	s.landingPublicCache.set(now, payload)
	response.SuccessI18n(c, "common.success", payload)
}

func (cache *landingPublicCache) get(now time.Time) (*landingPublicResponse, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.payload == nil || !now.Before(cache.expiresAt) {
		return nil, false
	}
	return cache.payload, true
}

func (cache *landingPublicCache) set(now time.Time, payload *landingPublicResponse) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.payload = payload
	cache.expiresAt = now.Add(landingPublicCacheTTL)
}
