package models

const (
	UserStatusActive   = "active"
	UserStatusDisabled = "disabled"
)

// User is a client-area account with a transactionally-maintained nano-USD balance.
type User struct {
	ID             uint   `gorm:"primaryKey;autoIncrement"`
	Email          string `gorm:"type:varchar(255);not null;uniqueIndex"`
	PasswordHash   string `gorm:"type:varchar(255);not null"`
	Status         string `gorm:"type:varchar(32);not null;default:'active';check:chk_user_status,status IN ('active','disabled')"`
	BalanceNanoUSD int64  `gorm:"column:balance_nano_usd;not null;default:0"`
	MaxAPIKeys     int    `gorm:"column:max_api_keys;not null;default:3;check:chk_user_max_api_keys,max_api_keys >= 0"`
	CreatedAtMS    int64  `gorm:"column:created_at_ms;not null;autoCreateTime:milli;check:chk_user_created_at,created_at_ms >= 0"`
	UpdatedAtMS    int64  `gorm:"column:updated_at_ms;not null;autoUpdateTime:milli;check:chk_user_updated_at,updated_at_ms >= 0"`
}

func (User) TableName() string { return "users" }

// UserSession stores only the sha256 of the opaque bearer token.
type UserSession struct {
	ID          uint   `gorm:"primaryKey;autoIncrement"`
	UserID      uint   `gorm:"not null;index"`
	TokenHash   string `gorm:"type:varchar(64);not null;uniqueIndex"`
	ExpiresAtMS int64  `gorm:"column:expires_at_ms;not null;check:chk_user_session_expires,expires_at_ms >= 0"`
	CreatedAtMS int64  `gorm:"column:created_at_ms;not null;autoCreateTime:milli"`
}

func (UserSession) TableName() string { return "user_sessions" }

// UserBalanceAdjustment is the ledger of manual balance changes (topup/correction).
type UserBalanceAdjustment struct {
	ID             uint   `gorm:"primaryKey;autoIncrement"`
	UserID         uint   `gorm:"not null;index"`
	DeltaNanoUSD   int64  `gorm:"column:delta_nano_usd;not null"`
	Reason         string `gorm:"type:varchar(32);not null;check:chk_user_adj_reason,reason IN ('topup','correction')"`
	AdminNote      string `gorm:"type:varchar(500);not null;default:''"`
	CreatedByAdmin string `gorm:"type:varchar(255);not null;default:''"`
	CreatedAtMS    int64  `gorm:"column:created_at_ms;not null;autoCreateTime:milli;check:chk_user_adj_created,created_at_ms >= 0"`
}

func (UserBalanceAdjustment) TableName() string { return "user_balance_adjustments" }
