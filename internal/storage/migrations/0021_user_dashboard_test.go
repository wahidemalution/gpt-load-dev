package migrations_test

import (
	"testing"

	"gpt-load/internal/storage/migrations"
)

func TestUserDashboardMigrationCreatesTablesAndUserIDColumn(t *testing.T) {
	t.Parallel()
	db := openInitialTestDatabase(t)
	if err := migrations.Up0001(db); err != nil {
		t.Fatalf("Up0001() error = %v", err)
	}

	if err := migrations.Up0021(db); err != nil {
		t.Fatalf("Up0021() error = %v", err)
	}
	for _, table := range []string{"users", "user_sessions", "user_balance_adjustments"} {
		if !db.Migrator().HasTable(table) {
			t.Errorf("Up0021 did not create table %q", table)
		}
	}
	if !db.Migrator().HasColumn("access_keys", "user_id") {
		t.Error("Up0021 did not add access_keys.user_id")
	}

	if err := migrations.Validate0021(db); err != nil {
		t.Errorf("Validate0021() error = %v", err)
	}

	if err := migrations.Up0021(db); err != nil {
		t.Errorf("Up0021() re-run error = %v, want idempotent", err)
	}
	if err := migrations.Validate0021(db); err != nil {
		t.Errorf("Validate0021() after re-run error = %v", err)
	}
}

func TestUserDashboardMigrationCheckConstraints(t *testing.T) {
	t.Parallel()
	db := openInitialTestDatabase(t)
	if err := migrations.Up0001(db); err != nil {
		t.Fatalf("Up0001() error = %v", err)
	}
	if err := migrations.Up0021(db); err != nil {
		t.Fatalf("Up0021() error = %v", err)
	}

	invalidStatements := []struct {
		name      string
		statement string
	}{
		{
			name: "invalid user status",
			statement: `INSERT INTO users (email, password_hash, status, created_at_ms, updated_at_ms)
				VALUES ('a@b.c', 'hash', 'banned', 1, 1)`,
		},
		{
			name: "negative user max api keys",
			statement: `INSERT INTO users (email, password_hash, max_api_keys, created_at_ms, updated_at_ms)
				VALUES ('a@b.c', 'hash', -1, 1, 1)`,
		},
		{
			name: "negative session expiry",
			statement: `INSERT INTO user_sessions (user_id, token_hash, expires_at_ms, created_at_ms)
				VALUES (1, 'hash', -1, 1)`,
		},
		{
			name: "invalid adjustment reason",
			statement: `INSERT INTO user_balance_adjustments (user_id, delta_nano_usd, reason, created_at_ms)
				VALUES (1, 100, 'gift', 1)`,
		},
	}
	for _, invalid := range invalidStatements {
		t.Run(invalid.name, func(t *testing.T) {
			if err := db.Exec(invalid.statement).Error; err == nil {
				t.Fatal("invalid user dashboard row was accepted")
			}
		})
	}

	if err := db.Exec(`INSERT INTO users (email, password_hash, created_at_ms, updated_at_ms)
		VALUES ('ok@b.c', 'hash', 1, 1)`).Error; err != nil {
		t.Fatalf("valid user row rejected: %v", err)
	}
	var row struct {
		Balance int64
		MaxKeys int64
	}
	if err := db.Raw(`SELECT balance_nano_usd AS balance, max_api_keys AS max_keys FROM users WHERE email = 'ok@b.c'`).
		Scan(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Balance != 0 || row.MaxKeys != 3 {
		t.Fatalf("user defaults balance=%d max_api_keys=%d, want 0 and 3", row.Balance, row.MaxKeys)
	}

	if err := db.Exec(`INSERT INTO user_balance_adjustments (user_id, delta_nano_usd, reason, created_at_ms)
		VALUES (1, 500, 'topup', 1)`).Error; err != nil {
		t.Fatalf("valid adjustment rejected: %v", err)
	}

	if err := db.Exec(`INSERT INTO access_keys (
		name, key_value, key_hash, key_suffix, status, user_id, created_at_ms, updated_at_ms
	) VALUES ('linked', 'cipher', 'user-linked-hash', '7f2a', 'active', 1, 1, 1)`).Error; err != nil {
		t.Fatalf("access_keys insert with user_id rejected: %v", err)
	}
	if err := db.Exec(`INSERT INTO access_keys (
		name, key_value, key_hash, key_suffix, status, created_at_ms, updated_at_ms
	) VALUES ('unlinked', 'cipher', 'user-unlinked-hash', '7f2b', 'active', 1, 1)`).Error; err != nil {
		t.Fatalf("access_keys insert without user_id rejected: %v", err)
	}
}
