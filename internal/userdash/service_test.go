package userdash

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"gpt-load/internal/storage/models"
	"gpt-load/internal/testutil/sqlitetest"
)

// deterministicRandom yields a fixed 32-byte pattern so tokens are predictable.
func deterministicRandom() *bytes.Reader {
	return bytes.NewReader(make([]byte, 32))
}

// deterministicKeyRandom yields distinct bytes per Read so sessions and
// generated keys never collide under the unique key_hash index.
type deterministicKeyRandom struct{ next byte }

func (r *deterministicKeyRandom) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.next
	}
	r.next++
	return len(p), nil
}

type testEnv struct {
	db      *gorm.DB
	now     time.Time
	service *Service
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	db := sqlitetest.OpenMigrated(t)
	env := &testEnv{db: db, now: time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)}
	env.service = NewService(db, &deterministicKeyRandom{next: 0x5a}, func() time.Time { return env.now })
	return env
}

func (e *testEnv) advance(d time.Duration) { e.now = e.now.Add(d) }

func TestSignupCreatesActiveUser(t *testing.T) {
	env := newTestEnv(t)

	profile, err := env.service.Signup(context.Background(), "  Alice@Example.COM ", "password1")
	if err != nil {
		t.Fatalf("Signup() error = %v", err)
	}
	if profile.Email != "alice@example.com" {
		t.Errorf("email = %q, want normalized lowercase", profile.Email)
	}
	if profile.Status != models.UserStatusActive {
		t.Errorf("status = %q, want active", profile.Status)
	}
	if profile.BalanceNanoUSD != 0 {
		t.Errorf("balance = %d, want 0", profile.BalanceNanoUSD)
	}
	if profile.MaxAPIKeys != 3 {
		t.Errorf("max_api_keys = %d, want default 3", profile.MaxAPIKeys)
	}

	var row models.User
	if err := env.db.First(&row, profile.ID).Error; err != nil {
		t.Fatalf("load user row: %v", err)
	}
	if strings.Contains(strings.ToLower(row.PasswordHash), "password1") {
		t.Error("password hash looks like plaintext")
	}
}

func TestSignupDuplicateEmail(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	if _, err := env.service.Signup(ctx, "dupe@example.com", "password1"); err != nil {
		t.Fatalf("first Signup() error = %v", err)
	}
	if _, err := env.service.Signup(ctx, "DUPE@example.com", "password2"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("duplicate Signup() error = %v, want ErrEmailTaken", err)
	}
}

func TestSignupValidation(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	cases := []struct {
		name     string
		email    string
		password string
	}{
		{name: "short password", email: "a@b.c", password: "pass1"},
		{name: "no digit", email: "a@b.c", password: "passwordx"},
		{name: "no letter", email: "a@b.c", password: "12345678"},
		{name: "empty email", email: "", password: "password1"},
		{name: "no at sign", email: "nope", password: "password1"},
		{name: "space in email", email: "a b@c.d", password: "password1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := env.service.Signup(ctx, tc.email, tc.password); !errors.Is(err, ErrValidation) {
				t.Fatalf("Signup(%q, %q) error = %v, want ErrValidation", tc.email, tc.password, err)
			}
		})
	}
}

func TestLoginSuccess(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	created, err := env.service.Signup(ctx, "login@example.com", "password1")
	if err != nil {
		t.Fatalf("Signup() error = %v", err)
	}

	token, profile, expiresAt, err := env.service.Login(ctx, "LOGIN@example.com", "password1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if token == "" || len(token) != 64 {
		t.Errorf("token len = %d, want 64 hex chars", len(token))
	}
	if profile.ID != created.ID {
		t.Errorf("profile.ID = %d, want %d", profile.ID, created.ID)
	}
	wantExpiry := env.now.Add(sessionTTL)
	if !expiresAt.Equal(wantExpiry) {
		t.Errorf("expiresAt = %v, want %v", expiresAt, wantExpiry)
	}
	if _, err := env.service.Authenticate(ctx, token); err != nil {
		t.Fatalf("Authenticate() after login error = %v", err)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	if _, err := env.service.Signup(ctx, "wrongpw@example.com", "password1"); err != nil {
		t.Fatalf("Signup() error = %v", err)
	}
	if _, _, _, err := env.service.Login(ctx, "wrongpw@example.com", "password2"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
	}
}

func TestLoginUnknownEmail(t *testing.T) {
	env := newTestEnv(t)
	if _, _, _, err := env.service.Login(context.Background(), "ghost@example.com", "password1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
	}
}

func TestAuthenticateGarbageToken(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.service.Authenticate(context.Background(), "not-a-real-token"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Authenticate() error = %v, want ErrUnauthenticated", err)
	}
}

func TestAuthenticateExpiredSession(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	if _, err := env.service.Signup(ctx, "expiry@example.com", "password1"); err != nil {
		t.Fatalf("Signup() error = %v", err)
	}
	token, _, _, err := env.service.Login(ctx, "expiry@example.com", "password1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	if _, err := env.service.Authenticate(ctx, token); err != nil {
		t.Fatalf("Authenticate() before expiry error = %v", err)
	}
	env.advance(sessionTTL + time.Minute)
	if _, err := env.service.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Authenticate() after expiry error = %v, want ErrUnauthenticated", err)
	}
}

func TestAuthenticateDisabledUser(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	profile, err := env.service.Signup(ctx, "banned@example.com", "password1")
	if err != nil {
		t.Fatalf("Signup() error = %v", err)
	}
	token, _, _, err := env.service.Login(ctx, "banned@example.com", "password1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if err := env.db.Model(&models.User{}).Where("id = ?", profile.ID).
		Update("status", models.UserStatusDisabled).Error; err != nil {
		t.Fatalf("disable user: %v", err)
	}

	if _, err := env.service.Authenticate(ctx, token); !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("Authenticate() error = %v, want ErrUserDisabled", err)
	}
}

func TestLoginDisabledUser(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	profile, err := env.service.Signup(ctx, "nologin@example.com", "password1")
	if err != nil {
		t.Fatalf("Signup() error = %v", err)
	}
	if err := env.db.Model(&models.User{}).Where("id = ?", profile.ID).
		Update("status", models.UserStatusDisabled).Error; err != nil {
		t.Fatalf("disable user: %v", err)
	}
	if _, _, _, err := env.service.Login(ctx, "nologin@example.com", "password1"); !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("Login() error = %v, want ErrUserDisabled", err)
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	if _, err := env.service.Signup(ctx, "bye@example.com", "password1"); err != nil {
		t.Fatalf("Signup() error = %v", err)
	}
	token, _, _, err := env.service.Login(ctx, "bye@example.com", "password1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if err := env.service.Logout(ctx, token); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if _, err := env.service.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Authenticate() after logout error = %v, want ErrUnauthenticated", err)
	}

	var count int64
	env.db.Model(&models.UserSession{}).Count(&count)
	if count != 0 {
		t.Errorf("user_sessions rows = %d, want 0 after logout", count)
	}
}

func TestLogoutUnknownTokenIsNoop(t *testing.T) {
	env := newTestEnv(t)
	if err := env.service.Logout(context.Background(), "never-existed"); err != nil {
		t.Fatalf("Logout() with unknown token error = %v", err)
	}
}

func TestTokenStoredHashedOnly(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	if _, err := env.service.Signup(ctx, "hash@example.com", "password1"); err != nil {
		t.Fatalf("Signup() error = %v", err)
	}
	token, _, _, err := env.service.Login(ctx, "hash@example.com", "password1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	var session models.UserSession
	if err := env.db.First(&session).Error; err != nil {
		t.Fatalf("load session row: %v", err)
	}
	if session.TokenHash == token {
		t.Fatal("token stored in plaintext")
	}
	if len(session.TokenHash) != 64 {
		t.Errorf("token_hash len = %d, want 64 hex chars", len(session.TokenHash))
	}
	for _, c := range session.TokenHash {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Errorf("token_hash %q is not lowercase hex", session.TokenHash)
			break
		}
	}
	// sha256(token) must round-trip to the stored hash.
	if hashToken(token) != session.TokenHash {
		t.Error("token_hash is not sha256 of the issued token")
	}
}
