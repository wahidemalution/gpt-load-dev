package userdash

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"gpt-load/internal/platform/httproute"
	"gpt-load/internal/platform/i18n"
)

type httpTestEnv struct {
	*testEnv
	engine       *gin.Engine
	t            *testing.T
	modelsCalls  int
	modelsResult any
}

func newHTTPTestEnv(t *testing.T) *httpTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := i18n.Init(); err != nil {
		t.Fatalf("i18n.Init() error = %v", err)
	}
	env := &httpTestEnv{
		testEnv:      newTestEnv(t),
		t:            t,
		modelsResult: []map[string]any{{"name": "gpt-5"}},
	}
	module := NewHTTPModule(env.service, nil, func(_ context.Context) (any, error) {
		env.modelsCalls++
		return env.modelsResult, nil
	})
	engine := gin.New()
	registry, err := httproute.NewRegistry(module.Module())
	if err != nil {
		t.Fatalf("NewRegistry(clientarea) error = %v", err)
	}
	if err := registry.Bind(engine); err != nil {
		t.Fatalf("Bind(clientarea) error = %v", err)
	}
	env.engine = engine
	return env
}

func (e *httpTestEnv) do(method, path, token string, body any) *httptest.ResponseRecorder {
	t := e.t
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	e.engine.ServeHTTP(recorder, request)
	return recorder
}

type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func decodeEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) envelope {
	t.Helper()
	var decoded envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return decoded
}

func TestHTTPSignup(t *testing.T) {
	env := newHTTPTestEnv(t)

	recorder := env.do(http.MethodPost, "/api/clientarea/signup", "", map[string]string{
		"email": "User@Example.com", "password": "password1",
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /signup = %d %s, want 200", recorder.Code, recorder.Body.String())
	}
	payload := decodeEnvelope(t, recorder)
	var profile struct {
		ID             uint   `json:"id"`
		Email          string `json:"email"`
		BalanceNanoUSD int64  `json:"balance_nano_usd"`
	}
	if err := json.Unmarshal(payload.Data, &profile); err != nil {
		t.Fatalf("decode signup profile: %v", err)
	}
	if profile.ID == 0 || profile.Email != "user@example.com" || profile.BalanceNanoUSD != 0 {
		t.Fatalf("signup profile = %#v", profile)
	}

	duplicate := env.do(http.MethodPost, "/api/clientarea/signup", "", map[string]string{
		"email": "user@example.com", "password": "password2",
	})
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate signup = %d %s, want 409", duplicate.Code, duplicate.Body.String())
	}

	badPassword := env.do(http.MethodPost, "/api/clientarea/signup", "", map[string]string{
		"email": "other@example.com", "password": "short",
	})
	if badPassword.Code != http.StatusBadRequest {
		t.Fatalf("bad password signup = %d %s, want 400", badPassword.Code, badPassword.Body.String())
	}
}

func TestHTTPLogin(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	created, err := env.service.Signup(ctx, "login-http@example.com", "password1")
	if err != nil {
		t.Fatalf("Signup() error = %v", err)
	}

	recorder := env.do(http.MethodPost, "/api/clientarea/login", "", map[string]string{
		"email": "login-http@example.com", "password": "password1",
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /login = %d %s, want 200", recorder.Code, recorder.Body.String())
	}
	payload := decodeEnvelope(t, recorder)
	var login struct {
		Token       string  `json:"token"`
		Profile     Profile `json:"profile"`
		ExpiresAtMS int64   `json:"expires_at_ms"`
	}
	if err := json.Unmarshal(payload.Data, &login); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if login.Token == "" {
		t.Fatal("login token is empty")
	}
	if login.Profile.ID != created.ID || login.Profile.Email != created.Email {
		t.Fatalf("login profile = %#v, want id %d", login.Profile, created.ID)
	}
	if login.ExpiresAtMS != env.now.Add(sessionTTL).UnixMilli() {
		t.Fatalf("expires_at_ms = %d, want %d", login.ExpiresAtMS, env.now.Add(sessionTTL).UnixMilli())
	}

	wrongPassword := env.do(http.MethodPost, "/api/clientarea/login", "", map[string]string{
		"email": "login-http@example.com", "password": "wrongpass1",
	})
	if wrongPassword.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password login = %d %s, want 401", wrongPassword.Code, wrongPassword.Body.String())
	}
}

func TestHTTPMe(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	created, err := env.service.Signup(ctx, "me@example.com", "password1")
	if err != nil {
		t.Fatalf("Signup() error = %v", err)
	}
	token, _, _, err := env.service.Login(ctx, "me@example.com", "password1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	withoutToken := env.do(http.MethodGet, "/api/clientarea/me", "", nil)
	if withoutToken.Code != http.StatusUnauthorized {
		t.Fatalf("GET /me without token = %d %s, want 401", withoutToken.Code, withoutToken.Body.String())
	}

	withToken := env.do(http.MethodGet, "/api/clientarea/me", token, nil)
	if withToken.Code != http.StatusOK {
		t.Fatalf("GET /me with token = %d %s, want 200", withToken.Code, withToken.Body.String())
	}
	payload := decodeEnvelope(t, withToken)
	var profile Profile
	if err := json.Unmarshal(payload.Data, &profile); err != nil {
		t.Fatalf("decode /me profile: %v", err)
	}
	if profile.ID != created.ID || profile.Email != created.Email {
		t.Fatalf("/me profile = %#v, want %#v", profile, created)
	}
}

func TestHTTPLogout(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	if _, err := env.service.Signup(ctx, "logout@example.com", "password1"); err != nil {
		t.Fatalf("Signup() error = %v", err)
	}
	token, _, _, err := env.service.Login(ctx, "logout@example.com", "password1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	recorder := env.do(http.MethodPost, "/api/clientarea/logout", token, nil)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("POST /logout = %d %s, want 204", recorder.Code, recorder.Body.String())
	}

	after := env.do(http.MethodGet, "/api/clientarea/me", token, nil)
	if after.Code != http.StatusUnauthorized {
		t.Fatalf("GET /me after logout = %d %s, want 401", after.Code, after.Body.String())
	}
}

func TestHTTPModels(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	unauthenticated := env.do(http.MethodGet, "/api/clientarea/models", "", nil)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("GET /models without token = %d %s, want 401", unauthenticated.Code, unauthenticated.Body.String())
	}

	if _, err := env.service.Signup(ctx, "models@example.com", "password1"); err != nil {
		t.Fatalf("Signup() error = %v", err)
	}
	token, _, _, err := env.service.Login(ctx, "models@example.com", "password1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	recorder := env.do(http.MethodGet, "/api/clientarea/models", token, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /models = %d %s, want 200", recorder.Code, recorder.Body.String())
	}
	payload := decodeEnvelope(t, recorder)
	var entries []map[string]any
	if err := json.Unmarshal(payload.Data, &entries); err != nil {
		t.Fatalf("decode /models payload: %v", err)
	}
	if len(entries) != 1 || entries[0]["name"] != "gpt-5" {
		t.Fatalf("/models entries = %#v", entries)
	}
	if env.modelsCalls != 1 {
		t.Fatalf("models provider calls = %d, want 1", env.modelsCalls)
	}
}

func TestHTTPRateLimit(t *testing.T) {
	env := newHTTPTestEnv(t)

	for attempt := 1; attempt <= 10; attempt++ {
		recorder := env.do(http.MethodPost, "/api/clientarea/login", "", map[string]string{
			"email": "ratelimit@example.com", "password": "wrongpass1",
		})
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf(
				"failed login #%d = %d %s, want 401",
				attempt, recorder.Code, recorder.Body.String(),
			)
		}
	}

	locked := env.do(http.MethodPost, "/api/clientarea/login", "", map[string]string{
		"email": "ratelimit@example.com", "password": "wrongpass1",
	})
	if locked.Code != http.StatusTooManyRequests {
		t.Fatalf("11th failed login = %d %s, want 429", locked.Code, locked.Body.String())
	}
	if locked.Header().Get("Retry-After") == "" {
		t.Error("429 response missing Retry-After header")
	}

	signupLocked := env.do(http.MethodPost, "/api/clientarea/signup", "", map[string]string{
		"email": "fresh@example.com", "password": "password1",
	})
	if signupLocked.Code != http.StatusTooManyRequests {
		t.Fatalf("signup while locked = %d %s, want 429", signupLocked.Code, signupLocked.Body.String())
	}
}
