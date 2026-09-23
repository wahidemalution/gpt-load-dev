package migrations_test

import (
	"testing"

	"gpt-load/internal/storage/migrations"
)

type accessKeyConcurrencyColumn0023 struct {
	MaxConcurrent int64 `gorm:"column:max_concurrent"`
}

func (accessKeyConcurrencyColumn0023) TableName() string { return "access_keys" }

func TestUp0023AddsMaxConcurrent(t *testing.T) {
	t.Parallel()
	db := openInitialTestDatabase(t)
	if err := migrations.Up0001(db); err != nil {
		t.Fatalf("Up0001() error = %v", err)
	}
	if err := migrations.Up0023(db); err != nil {
		t.Fatalf("Up0023() error = %v", err)
	}
	if !db.Migrator().HasColumn("access_keys", "max_concurrent") {
		t.Fatal("max_concurrent column missing")
	}
	if err := migrations.Validate0023(db); err != nil {
		t.Fatalf("Validate0023() error = %v", err)
	}
	// Idempotent: running again must not error.
	if err := migrations.Up0023(db); err != nil {
		t.Fatalf("Up0023() second run error = %v", err)
	}
	// Recoverable accepts a not-yet-migrated table (simulate by dropping the column).
	if err := db.Migrator().DropColumn(&accessKeyConcurrencyColumn0023{}, "max_concurrent"); err != nil {
		t.Fatalf("DropColumn() error = %v", err)
	}
	if err := migrations.ValidateRecoverable0023(db); err != nil {
		t.Fatalf("ValidateRecoverable0023() pre-column error = %v", err)
	}
}

func TestUp0023RejectsNegativeMaxConcurrent(t *testing.T) {
	t.Parallel()
	db := openInitialTestDatabase(t)
	if err := migrations.Up0001(db); err != nil {
		t.Fatalf("Up0001() error = %v", err)
	}
	if err := migrations.Up0023(db); err != nil {
		t.Fatalf("Up0023() error = %v", err)
	}
	err := db.Exec(`INSERT INTO access_keys (
		name, key_value, key_hash, key_suffix, status, max_concurrent, created_at_ms, updated_at_ms
	) VALUES ('negative', 'cipher', 'concurrency-negative-hash', '9f2a', 'active', -1, 1, 1)`).Error
	if err == nil {
		t.Fatal("negative max_concurrent was accepted")
	}
}

func TestUp0023RecoverableRequiresTable(t *testing.T) {
	t.Parallel()
	db := openInitialTestDatabase(t)
	if err := migrations.ValidateRecoverable0023(db); err == nil {
		t.Fatal("ValidateRecoverable0023() accepted a missing access_keys table")
	}
}
