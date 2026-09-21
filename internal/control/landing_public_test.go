package control

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"gpt-load/internal/channel"
	"gpt-load/internal/platform/config"
	"gpt-load/internal/platform/httproute"
	"gpt-load/internal/requestlog"
	"gpt-load/internal/storage/models"
)

type recordingLandingUsageReader struct {
	queries []requestlog.UsageQuery
	report  requestlog.UsageReport
	err     error
}

func (reader *recordingLandingUsageReader) QueryUsage(
	_ context.Context,
	query requestlog.UsageQuery,
) (requestlog.UsageReport, error) {
	reader.queries = append(reader.queries, query)
	if reader.err != nil {
		return requestlog.UsageReport{}, reader.err
	}
	return reader.report, nil
}

func newLandingPublicTestEngine(
	t *testing.T,
	server *Server,
) *gin.Engine {
	t.Helper()
	engine := gin.New()
	registry, err := httproute.NewRegistry(server.LandingPublicHTTPModule())
	if err != nil {
		t.Fatalf("NewRegistry(landing-public) error = %v", err)
	}
	if err := registry.Bind(engine); err != nil {
		t.Fatalf("Bind(landing-public) error = %v", err)
	}
	return engine
}

func performLandingPublicRequest(engine *gin.Engine) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/public/landing", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func TestLandingPublicReturnsModelsAndAllTimeStatsWithoutAuth(t *testing.T) {
	t.Parallel()
	initControlI18n(t)
	fixture := newServiceFixture(t)
	createPriceTestGroup(t, fixture.db, models.Group{
		Name: "landing-enabled", ChannelID: string(channel.OpenAICompatible), Params: models.JSON(`{"base_url":"https://enabled.example/v1"}`),
		Models: models.JSON(`[{"id":"upstream-a","alias":"gpt-5"},{"id":"upstream-b","alias":"claude-4"}]`), Overrides: models.JSON(`{}`), Enabled: true,
	})
	disabled := createPriceTestGroup(t, fixture.db, models.Group{
		Name: "landing-disabled", ChannelID: string(channel.Anthropic), Params: models.JSON(`{"base_url":"https://disabled.example"}`),
		Models: models.JSON(`[{"id":"upstream-c","alias":"gpt-5"},{"id":"upstream-d","alias":"hidden-model"}]`), Overrides: models.JSON(`{}`), Enabled: false,
	})
	if err := fixture.db.Model(&disabled).Update("enabled", false).Error; err != nil {
		t.Fatalf("disable landing group: %v", err)
	}
	mustEnsureInitialPrices(t, fixture)

	reader := &recordingLandingUsageReader{report: requestlog.UsageReport{
		Summary: requestlog.UsageAggregate{
			RequestCount: 7, SuccessCount: 7,
			UncachedInputTokens: 100, OutputTokens: 23,
		},
	}}
	fixture.service.usageStats = reader
	server := NewServer(&config.Config{AuthKey: authTestKey}, fixture.service)
	engine := newLandingPublicTestEngine(t, server)

	recorder := performLandingPublicRequest(engine)
	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"GET /api/public/landing = %d %s, want 200",
			recorder.Code,
			recorder.Body.String(),
		)
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
			Stats struct {
				TotalRequests int64 `json:"total_requests"`
				TotalTokens   int64 `json:"total_tokens"`
			} `json:"stats"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode landing response: %v", err)
	}
	if envelope.Code != 0 {
		t.Fatalf("landing response code = %d", envelope.Code)
	}
	if len(envelope.Data.Models) != 2 ||
		envelope.Data.Models[0].Name != "claude-4" ||
		envelope.Data.Models[1].Name != "gpt-5" {
		t.Fatalf("landing models = %#v", envelope.Data.Models)
	}
	if envelope.Data.Stats.TotalRequests != 7 || envelope.Data.Stats.TotalTokens != 123 {
		t.Fatalf("landing stats = %#v", envelope.Data.Stats)
	}
	if len(reader.queries) != 1 ||
		reader.queries[0].FromMS != 0 {
		t.Fatalf("QueryUsage calls = %#v", reader.queries)
	}
}

func TestLandingPublicCachesPayloadForTTL(t *testing.T) {
	t.Parallel()
	initControlI18n(t)
	fixture := newServiceFixture(t)
	createPriceTestGroup(t, fixture.db, models.Group{
		Name: "landing-cache", ChannelID: string(channel.OpenAICompatible), Params: models.JSON(`{"base_url":"https://cache.example/v1"}`),
		Models: models.JSON(`[{"id":"upstream-a","alias":"gpt-5"}]`), Overrides: models.JSON(`{}`), Enabled: true,
	})
	mustEnsureInitialPrices(t, fixture)

	now := time.Date(2026, time.September, 21, 12, 0, 0, 0, time.UTC)
	reader := &recordingLandingUsageReader{report: requestlog.UsageReport{
		Summary: requestlog.UsageAggregate{RequestCount: 1, UncachedInputTokens: 10},
	}}
	fixture.service.usageStats = reader
	server := NewServer(&config.Config{AuthKey: authTestKey}, fixture.service)
	server.now = func() time.Time { return now }
	engine := newLandingPublicTestEngine(t, server)

	first := performLandingPublicRequest(engine)
	second := performLandingPublicRequest(engine)
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("cached responses = %d/%d", first.Code, second.Code)
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("cached payload differs:\n%s\n%s", first.Body.String(), second.Body.String())
	}
	if len(reader.queries) != 1 {
		t.Fatalf("QueryUsage calls = %d, want 1 (cache hit)", len(reader.queries))
	}

	now = now.Add(landingPublicCacheTTL + time.Second)
	third := performLandingPublicRequest(engine)
	if third.Code != http.StatusOK {
		t.Fatalf("post-TTL response = %d %s", third.Code, third.Body.String())
	}
	if len(reader.queries) != 2 {
		t.Fatalf("QueryUsage calls after TTL = %d, want 2", len(reader.queries))
	}
}

func TestLandingPublicIsNotProtectedByControlAuth(t *testing.T) {
	t.Parallel()
	initControlI18n(t)
	fixture := newServiceFixture(t)
	reader := &recordingLandingUsageReader{report: requestlog.UsageReport{}}
	fixture.service.usageStats = reader
	server := NewServer(&config.Config{AuthKey: authTestKey}, fixture.service)
	engine := gin.New()
	registry, err := httproute.NewRegistry(
		server.LandingPublicHTTPModule(),
		server.HTTPModule(),
	)
	if err != nil {
		t.Fatalf("NewRegistry(landing-public+control) error = %v", err)
	}
	if err := registry.Bind(engine); err != nil {
		t.Fatalf("Bind(landing-public+control) error = %v", err)
	}

	// No Authorization header at all.
	recorder := performLandingPublicRequest(engine)
	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"GET /api/public/landing without auth = %d %s, want 200",
			recorder.Code,
			recorder.Body.String(),
		)
	}

	// Control route on the same engine still requires auth.
	request := httptest.NewRequest(http.MethodGet, "/api/home", nil)
	homeRecorder := httptest.NewRecorder()
	engine.ServeHTTP(homeRecorder, request)
	if homeRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/home without auth = %d, want 401", homeRecorder.Code)
	}
}
