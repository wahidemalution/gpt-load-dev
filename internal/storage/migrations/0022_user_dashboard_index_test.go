package migrations_test

import (
	"testing"

	"gpt-load/internal/storage/migrations"
)

func TestUserDashboardIndexCreatedAndIdempotent(t *testing.T) {
	t.Parallel()
	db := openInitialTestDatabase(t)
	if err := migrations.Up0001(db); err != nil {
		t.Fatalf("Up0001() error = %v", err)
	}
	if err := migrations.Up0021(db); err != nil {
		t.Fatalf("Up0021() error = %v", err)
	}
	// Up0021 already creates the index on a fresh install; drop it so this
	// test exercises 0022 creating it from scratch (the deployment path where
	// 0021 was applied before the index existed).
	if err := db.Migrator().DropIndex("access_keys", "idx_access_keys_user_id"); err != nil {
		t.Fatalf("DropIndex() error = %v", err)
	}
	if db.Migrator().HasIndex("access_keys", "idx_access_keys_user_id") {
		t.Fatal("index still present after DropIndex")
	}
	if err := migrations.Validate0022(db); err == nil {
		t.Fatal("Validate0022 accepted a missing index")
	}

	if err := migrations.Up0022(db); err != nil {
		t.Fatalf("Up0022() error = %v", err)
	}
	if !db.Migrator().HasIndex("access_keys", "idx_access_keys_user_id") {
		t.Fatal("Up0022 did not create the access_keys.user_id index")
	}
	if err := migrations.Validate0022(db); err != nil {
		t.Fatalf("Validate0022() error = %v", err)
	}

	if err := migrations.Up0022(db); err != nil {
		t.Fatalf("Up0022() re-run error = %v, want idempotent", err)
	}
	if err := migrations.Validate0022(db); err != nil {
		t.Fatalf("Validate0022() after re-run error = %v", err)
	}
}

func TestUserDashboardIndexRecoverableRequiresTable(t *testing.T) {
	t.Parallel()
	db := openInitialTestDatabase(t)
	if err := migrations.ValidateRecoverable0022(db); err == nil {
		t.Fatal("ValidateRecoverable0022 accepted a missing access_keys table")
	}
	if err := migrations.Up0001(db); err != nil {
		t.Fatalf("Up0001() error = %v", err)
	}
	// With the table present but the index absent, recovery must be allowed.
	if err := migrations.ValidateRecoverable0022(db); err != nil {
		t.Fatalf("ValidateRecoverable0022() error = %v, want nil before index exists", err)
	}
}
