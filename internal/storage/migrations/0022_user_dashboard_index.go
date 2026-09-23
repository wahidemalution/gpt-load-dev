package migrations

import (
	"fmt"

	"gorm.io/gorm"
)

const ID0022 = "0022_user_dashboard_index"
const accessKeyUserIDIndexName0022 = "idx_access_keys_user_id"

// accessKeyUserIDIndex0022 mirrors the access_keys.user_id column with the index
// declared, so the migrator can resolve the index by name on every dialect.
type accessKeyUserIDIndex0022 struct {
	UserID *uint `gorm:"column:user_id;index:idx_access_keys_user_id"`
}

func (accessKeyUserIDIndex0022) TableName() string { return "access_keys" }

// Up0022 creates the index on access_keys.user_id. Migration 0021 added the
// column through Migrator.AddColumn, which never creates the index declared on
// its mirror model, so databases that already applied 0021 have an unindexed
// user_id. This migration is idempotent via HasIndex.
func Up0022(db *gorm.DB) error {
	if err := ValidateRecoverable0022(db); err != nil {
		return err
	}
	if !db.Migrator().HasIndex(&accessKeyUserIDIndex0022{}, accessKeyUserIDIndexName0022) {
		if err := db.Migrator().CreateIndex(&accessKeyUserIDIndex0022{}, accessKeyUserIDIndexName0022); err != nil {
			return fmt.Errorf("create access keys user id index: %w", err)
		}
	}
	return Validate0022(db)
}

func ValidateRecoverable0022(db *gorm.DB) error {
	if !db.Migrator().HasTable(&accessKeyUserIDIndex0022{}) {
		return fmt.Errorf("access keys user id index: access_keys table is missing")
	}
	if db.Migrator().HasIndex(&accessKeyUserIDIndex0022{}, accessKeyUserIDIndexName0022) {
		return Validate0022(db)
	}
	return nil
}

func Validate0022(db *gorm.DB) error {
	if !db.Migrator().HasIndex(&accessKeyUserIDIndex0022{}, accessKeyUserIDIndexName0022) {
		return fmt.Errorf("access_keys.user_id index is missing")
	}
	return nil
}
