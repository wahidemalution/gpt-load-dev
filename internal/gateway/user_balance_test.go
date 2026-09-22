package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"gpt-load/internal/state"
	"gpt-load/internal/userdash"
)

func staticBalanceGate(balances map[uint]int64) AccessKeyBalanceGate {
	return userdash.NewBalanceGate(
		func(_ context.Context, userIDs []uint) (map[uint]int64, error) {
			out := make(map[uint]int64, len(userIDs))
			for _, id := range userIDs {
				if balance, ok := balances[id]; ok {
					out[id] = balance
				}
			}
			return out, nil
		},
		30*time.Second,
	)
}

func TestUserBalanceAuthenticateBlocksExhaustedOwner(t *testing.T) {
	snapshot := &state.ConfigSnapshot{AccessKeysByHash: map[string]state.AccessKeyView{
		"hash:owned": {ID: 7, Name: "owned", UserID: 7},
	}}
	request := &http.Request{
		Header:     http.Header{"Authorization": {"Bearer owned"}},
		URL:        &url.URL{},
		RemoteAddr: "192.0.2.10:1234",
	}

	got, ok, reason := authenticate(request, snapshot, testHasher{}, time.Now(), staticBalanceGate(map[uint]int64{7: 0}))
	if ok || reason != accessKeyAuthFailureNoBalance || got.ID != 7 {
		t.Fatalf("authenticate() = (%#v, %t, %q), want blocked with no-balance reason", got, ok, reason)
	}
}

func TestUserBalanceAuthenticateAllowsFundedOwnerAndPlatformKeys(t *testing.T) {
	snapshot := &state.ConfigSnapshot{AccessKeysByHash: map[string]state.AccessKeyView{
		"hash:owned":    {ID: 7, Name: "owned", UserID: 7},
		"hash:platform": {ID: 8, Name: "platform"},
	}}
	owned := &http.Request{
		Header:     http.Header{"Authorization": {"Bearer owned"}},
		URL:        &url.URL{},
		RemoteAddr: "192.0.2.10:1234",
	}
	platform := &http.Request{
		Header:     http.Header{"Authorization": {"Bearer platform"}},
		URL:        &url.URL{},
		RemoteAddr: "192.0.2.10:1234",
	}

	if _, ok, _ := authenticate(owned, snapshot, testHasher{}, time.Now(), staticBalanceGate(map[uint]int64{7: 500})); !ok {
		t.Fatal("authenticate() rejected funded owner key")
	}
	if _, ok, _ := authenticate(owned, snapshot, testHasher{}, time.Now(), nil); !ok {
		t.Fatal("authenticate() rejected owner key with nil gate")
	}
	if _, ok, _ := authenticate(platform, snapshot, testHasher{}, time.Now(), staticBalanceGate(map[uint]int64{})); !ok {
		t.Fatal("authenticate() blocked unowned platform key")
	}
}

func TestUserBalanceDataPlaneWrites402InsufficientBalance(t *testing.T) {
	forwarder := &scriptedForwarder{}
	engine, handler, manager, _ := newRequestLogHandlerTestRuntime(
		t, forwarder, &recordingAccessKeyRPMLimiter{}, &recordingRequestLogSink{}, "sk-first",
	)
	input := gatewayAccessQuotaCompileInput(handler, nil)
	input.AccessKeys[0].UserID = 7
	if _, err := manager.Publish(input); err != nil {
		t.Fatal(err)
	}
	handler.balanceGate = staticBalanceGate(map[uint]int64{7: 0})

	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.Header.Set("Authorization", "Bearer gl-client")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusPaymentRequired ||
		!strings.Contains(response.Body.String(), `"code":"insufficient_balance"`) {
		t.Fatalf("zero-balance response = %d %s", response.Code, response.Body.String())
	}
	if len(forwarder.inputs) != 0 {
		t.Fatalf("forward calls = %d, want 0", len(forwarder.inputs))
	}

	handler.balanceGate = staticBalanceGate(map[uint]int64{7: 500})
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.Header.Set("Authorization", "Bearer gl-client")
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("funded response = %d %s", response.Code, response.Body.String())
	}
}
