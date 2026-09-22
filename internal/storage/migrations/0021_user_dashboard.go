package migrations

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

const ID0021 = "0021_user_dashboard"

type user0021 struct {
	ID             uint   `gorm:"primaryKey;autoIncrement"`
	Email          string `gorm:"type:varchar(255);not null;uniqueIndex"`
	PasswordHash   string `gorm:"type:varchar(255);not null"`
	Status         string `gorm:"type:varchar(32);not null;default:'active';check:chk_user_status,status IN ('active','disabled')"`
	BalanceNanoUSD int64  `gorm:"column:balance_nano_usd;not null;default:0"`
	MaxAPIKeys     int    `gorm:"column:max_api_keys;not null;default:3;check:chk_user_max_api_keys,max_api_keys >= 0"`
	CreatedAtMS    int64  `gorm:"column:created_at_ms;not null;autoCreateTime:milli;check:chk_user_created_at,created_at_ms >= 0"`
	UpdatedAtMS    int64  `gorm:"column:updated_at_ms;not null;autoUpdateTime:milli;check:chk_user_updated_at,updated_at_ms >= 0"`
}

func (user0021) TableName() string { return "users" }

type userSession0021 struct {
	ID          uint   `gorm:"primaryKey;autoIncrement"`
	UserID      uint   `gorm:"not null;index"`
	TokenHash   string `gorm:"type:varchar(64);not null;uniqueIndex"`
	ExpiresAtMS int64  `gorm:"column:expires_at_ms;not null;check:chk_user_session_expires,expires_at_ms >= 0"`
	CreatedAtMS int64  `gorm:"column:created_at_ms;not null;autoCreateTime:milli"`
}

func (userSession0021) TableName() string { return "user_sessions" }

type userBalanceAdjustment0021 struct {
	ID             uint   `gorm:"primaryKey;autoIncrement"`
	UserID         uint   `gorm:"not null;index"`
	DeltaNanoUSD   int64  `gorm:"column:delta_nano_usd;not null"`
	Reason         string `gorm:"type:varchar(32);not null;check:chk_user_adj_reason,reason IN ('topup','correction')"`
	AdminNote      string `gorm:"type:varchar(500);not null;default:''"`
	CreatedByAdmin string `gorm:"type:varchar(255);not null;default:''"`
	CreatedAtMS    int64  `gorm:"column:created_at_ms;not null;autoCreateTime:milli;check:chk_user_adj_created,created_at_ms >= 0"`
}

func (userBalanceAdjustment0021) TableName() string { return "user_balance_adjustments" }

type accessKeyUserID0021 struct {
	UserID *uint `gorm:"column:user_id;index"`
}

func (accessKeyUserID0021) TableName() string { return "access_keys" }

func Up0021(db *gorm.DB) error {
	switch strings.ToLower(db.Dialector.Name()) {
	case "mysql", "postgres", "postgresql", "sqlite":
	default:
		return fmt.Errorf("user dashboard: unsupported database driver %q", db.Dialector.Name())
	}
	for _, model := range []any{&user0021{}, &userSession0021{}, &userBalanceAdjustment0021{}} {
		if db.Migrator().HasTable(model) {
			continue
		}
		if err := db.Migrator().CreateTable(model); err != nil {
			return fmt.Errorf("create user dashboard tables: %w", err)
		}
	}
	if !db.Migrator().HasColumn(&accessKeyUserID0021{}, "user_id") {
		if err := db.Migrator().AddColumn(&accessKeyUserID0021{}, "user_id"); err != nil {
			return fmt.Errorf("add access_keys.user_id: %w", err)
		}
	}
	return Validate0021(db)
}

func ValidateRecoverable0021(db *gorm.DB) error {
	if !db.Migrator().HasTable(&user0021{}) {
		return nil
	}
	return Validate0021(db)
}

func Validate0021(db *gorm.DB) error {
	for table, model := range map[string]any{
		"users":                    &user0021{},
		"user_sessions":            &userSession0021{},
		"user_balance_adjustments": &userBalanceAdjustment0021{},
	} {
		if !db.Migrator().HasTable(model) {
			return fmt.Errorf("user dashboard table %s is missing", table)
		}
	}
	if !db.Migrator().HasColumn(&accessKeyUserID0021{}, "user_id") {
		return fmt.Errorf("access_keys.user_id column is missing")
	}
	users, err := db.Migrator().ColumnTypes(&user0021{})
	if err != nil {
		return err
	}
	found := make(map[string]bool, len(users))
	for _, column := range users {
		name := column.Name()
		found[name] = true
		if primary, known := column.PrimaryKey(); known && primary {
			continue
		}
		if nullable, known := column.Nullable(); known && nullable {
			return fmt.Errorf("users.%s must be non-null", name)
		}
	}
	for _, name := range []string{
		"id", "email", "password_hash", "status", "balance_nano_usd",
		"max_api_keys", "created_at_ms", "updated_at_ms",
	} {
		if !found[name] {
			return fmt.Errorf("users column %s is missing", name)
		}
	}
	sessions, err := db.Migrator().ColumnTypes(&userSession0021{})
	if err != nil {
		return err
	}
	sessionColumns := make(map[string]bool, len(sessions))
	for _, column := range sessions {
		sessionColumns[column.Name()] = true
	}
	for _, name := range []string{"id", "user_id", "token_hash", "expires_at_ms", "created_at_ms"} {
		if !sessionColumns[name] {
			return fmt.Errorf("user_sessions column %s is missing", name)
		}
	}
	adjustments, err := db.Migrator().ColumnTypes(&userBalanceAdjustment0021{})
	if err != nil {
		return err
	}
	adjustmentColumns := make(map[string]bool, len(adjustments))
	for _, column := range adjustments {
		adjustmentColumns[column.Name()] = true
	}
	for _, name := range []string{
		"id", "user_id", "delta_nano_usd", "reason", "admin_note",
		"created_by_admin", "created_at_ms",
	} {
		if !adjustmentColumns[name] {
			return fmt.Errorf("user_balance_adjustments column %s is missing", name)
		}
	}
	return nil
}
