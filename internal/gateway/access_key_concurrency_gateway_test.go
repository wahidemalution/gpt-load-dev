package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"gpt-load/internal/ratelimit"
	"gpt-load/internal/telemetry"
)

// gatedForwarder blocks the first Forward call until released, then succeeds.
// A second call returns immediately. Deterministic gating with channels only.
type gatedForwarder struct {
	entered     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
	mu          sync.Mutex
	calls       int
}

func newGatedForwarder() *gatedForwarder {
	return &gatedForwarder{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (forwarder *gatedForwarder) finish() {
	forwarder.releaseOnce.Do(func() { close(forwarder.release) })
}

func (forwarder *gatedForwarder) Forward(_ context.Context, _ ForwardInput) UpstreamResult {
	forwarder.mu.Lock()
	index := forwarder.calls
	forwarder.calls++
	forwarder.mu.Unlock()
	if index == 0 {
		close(forwarder.entered)
		<-forwarder.release
	}
	return completeScriptedUpstreamResult(UpstreamResult{
		StatusCode:     http.StatusOK,
		Header:         make(http.Header),
		Body:           []byte(`{"ok":true}`),
		RequestWritten: true,
	})
}

func (*gatedForwarder) ForwardStream(
	context.Context,
	ForwardInput,
	http.ResponseWriter,
) UpstreamResult {
	return completeScriptedUpstreamResult(UpstreamResult{Err: errors.New("stream not scripted")})
}

func TestHandlerConcurrencyLimitRejectsOverLimitAndReleasesSlot(t *testing.T) {
	forwarder := newGatedForwarder()
	engine, handler, _, _ := newRequestLogHandlerTestRuntime(
		t, forwarder, &recordingAccessKeyRPMLimiter{}, telemetry.NoopRequestLogSink{}, "sk-first",
	)
	handler.concurrency = ratelimit.NewAccessKeyConcurrency()
	limiter, _ := handler.concurrency.(*ratelimit.AccessKeyConcurrency)

	newRequest := func() *http.Request {
		request := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			strings.NewReader(`{"model":"gpt-4o"}`),
		)
		request.Header.Set("Authorization", "Bearer gl-client")
		return request
	}

	firstRecorder := httptest.NewRecorder()
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		engine.ServeHTTP(firstRecorder, newRequest())
	}()

	// Wait until the first request is genuinely in flight and holding the slot.
	<-forwarder.entered

	if got := limiter.InFlight(1); got != 1 {
		t.Fatalf("in-flight during first request = %d, want 1", got)
	}

	// Second request must be rejected while the slot is held.
	secondRecorder := httptest.NewRecorder()
	engine.ServeHTTP(secondRecorder, newRequest())
	assertGatewayReasonTest(t, secondRecorder, http.StatusTooManyRequests, reasonAccessKeyConcurrentLimited.Code)
	if got := secondRecorder.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want %q", got, "1")
	}

	// Release the first request and confirm the slot is freed.
	forwarder.finish()
	<-firstDone
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want 200; body=%s", firstRecorder.Code, firstRecorder.Body.String())
	}
	if got := limiter.InFlight(1); got != 0 {
		t.Fatalf("in-flight after first request = %d, want 0", got)
	}

	// A subsequent request must now succeed.
	thirdRecorder := httptest.NewRecorder()
	engine.ServeHTTP(thirdRecorder, newRequest())
	if thirdRecorder.Code != http.StatusOK {
		t.Fatalf("third request status = %d, want 200; body=%s", thirdRecorder.Code, thirdRecorder.Body.String())
	}
}
