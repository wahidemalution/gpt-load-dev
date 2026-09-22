package control

import (
	"context"
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

type landingPublicPriceSlots struct {
	Input      *string `json:"input"`
	Output     *string `json:"output"`
	CacheRead  *string `json:"cache_read"`
	CacheWrite *string `json:"cache_write"`
}

type landingPublicModelResponse struct {
	Name    string                  `json:"name"`
	Context *int64                  `json:"context"`
	Prices  landingPublicPriceSlots `json:"prices"`
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

// PublicLandingModels returns the deduplicated, price-annotated model list
// shown on the public landing page. Reused by the client-area /models route.
func (s *Server) PublicLandingModels(ctx context.Context) ([]landingPublicModelResponse, error) {
	models, err := s.service.ListProjectModels(ctx, ProjectModelListQuery{
		GroupStatus:   ProjectModelGroupStatusEnabled,
		PricingStatus: ProjectModelPricingStatusAll,
		Page:          1,
		PageSize:      maxProjectModelPageSize,
	})
	if err != nil {
		return nil, err
	}
	// ponytail: only the first 100 models are exposed; add page loop when a
	// deployment serves more than 100 distinct client models.
	seen := make(map[string]struct{}, len(models.Items))
	byName := make(map[string]*landingPublicModelResponse, len(models.Items))
	for _, item := range models.Items {
		if _, exists := seen[item.ClientModel]; exists {
			continue
		}
		seen[item.ClientModel] = struct{}{}
		entry := &landingPublicModelResponse{Name: item.ClientModel}
		if len(item.UpstreamModels) > 0 {
			upstream := item.UpstreamModels[0]
			entry.Prices = landingPublicPriceSlots{
				Input:      upstream.Price.Prices.Input,
				Output:     upstream.Price.Prices.Output,
				CacheRead:  upstream.Price.Prices.CacheRead,
				CacheWrite: upstream.Price.Prices.CacheWrite,
			}
			if reference := upstream.CatalogReference; reference != nil {
				entry.Context = reference.Model.Limits.Context
			}
		}
		byName[item.ClientModel] = entry
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	payload := make([]landingPublicModelResponse, 0, len(names))
	for _, name := range names {
		payload = append(payload, *byName[name])
	}
	return payload, nil
}

func (s *Server) handleLandingPublic(c *gin.Context) {
	now := s.now()
	if payload, ok := s.landingPublicCache.get(now); ok {
		response.SuccessI18n(c, "common.success", payload)
		return
	}

	ctx := c.Request.Context()
	landingModels, err := s.PublicLandingModels(ctx)
	if err != nil {
		writeServiceError(c, "landing_public", err)
		return
	}

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
		Models: landingModels,
		Stats: landingPublicStatsResponse{
			TotalRequests: aggregate.RequestCount,
			TotalTokens:   aggregate.TotalTokens,
		},
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
