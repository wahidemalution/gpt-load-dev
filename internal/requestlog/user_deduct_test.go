package requestlog

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"gpt-load/internal/storage/models"
	"gpt-load/internal/userdash"
)

func newDeductWriter(db *gorm.DB) *gormBatchWriter {
	return &gormBatchWriter{db: db, userDeductor: userdash.DeductCosts}
}

func seedDeductUser(t *testing.T, db *gorm.DB, balance int64) (uint, uint) {
	t.Helper()
	user := models.User{
		Email:          "deduct@example.com",
		PasswordHash:   "test-hash",
		Status:         models.UserStatusActive,
		BalanceNanoUSD: balance,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	key := models.AccessKey{
		Name:      "owned key",
		KeyValue:  "sk-gl-owned",
		KeyHash:   "owned-hash",
		KeySuffix: "wned",
		UserID:    &user.ID,
	}
	if err := db.Create(&key).Error; err != nil {
		t.Fatalf("create access key: %v", err)
	}
	unowned := models.AccessKey{
		Name:      "platform key",
		KeyValue:  "sk-gl-platform",
		KeyHash:   "platform-hash",
		KeySuffix: "orm-",
	}
	if err := db.Create(&unowned).Error; err != nil {
		t.Fatalf("create unowned access key: %v", err)
	}
	return user.ID, key.ID
}

func deductRow(id string, keyID uint, cost int64) models.RequestLog {
	row := aggregationRow(id, time.Date(2026, time.September, 22, 12, 0, 0, 0, time.UTC), 7, "deduct-model")
	row.AccessKeyID = keyID
	row.EstimatedCostNanoUSD = cost
	return row
}

func TestUserDeductDeductsCostOncePerJournalCommit(t *testing.T) {
	db := openRequestLogQueryDB(t)
	userID, keyID := seedDeductUser(t, db, 1000)
	writer := newDeductWriter(db)
	rows := []models.RequestLog{deductRow(aggregationRequestID(300), keyID, 400)}

	if err := writer.WriteBatch(context.Background(), rows); err != nil {
		t.Fatalf("WriteBatch() error = %v", err)
	}
	var balance int64
	if err := db.Model(&models.User{}).Where("id = ?", userID).
		Pluck("balance_nano_usd", &balance).Error; err != nil {
		t.Fatalf("query balance: %v", err)
	}
	if balance != 600 {
		t.Fatalf("balance after deduction = %d, want 600", balance)
	}

	// Retry with the same rows: idempotent, no double deduction.
	if err := writer.WriteBatch(context.Background(), rows); err != nil {
		t.Fatalf("replay WriteBatch() error = %v", err)
	}
	if err := db.Model(&models.User{}).Where("id = ?", userID).
		Pluck("balance_nano_usd", &balance).Error; err != nil {
		t.Fatalf("query replayed balance: %v", err)
	}
	if balance != 600 {
		t.Fatalf("balance after replay = %d, want 600", balance)
	}
}

func TestUserDeductSkipsUnownedKeys(t *testing.T) {
	db := openRequestLogQueryDB(t)
	userID, _ := seedDeductUser(t, db, 1000)
	writer := newDeductWriter(db)
	unownedKey := uint(999_999)
	rows := []models.RequestLog{deductRow(aggregationRequestID(301), unownedKey, 400)}

	if err := writer.WriteBatch(context.Background(), rows); err != nil {
		t.Fatalf("WriteBatch() error = %v", err)
	}
	var balance int64
	if err := db.Model(&models.User{}).Where("id = ?", userID).
		Pluck("balance_nano_usd", &balance).Error; err != nil {
		t.Fatalf("query balance: %v", err)
	}
	if balance != 1000 {
		t.Fatalf("balance after unowned deduction = %d, want 1000", balance)
	}
}

func TestUserDeductDisabledWithoutDeductor(t *testing.T) {
	db := openRequestLogQueryDB(t)
	userID, keyID := seedDeductUser(t, db, 1000)
	writer := &gormBatchWriter{db: db}
	rows := []models.RequestLog{deductRow(aggregationRequestID(302), keyID, 400)}

	if err := writer.WriteBatch(context.Background(), rows); err != nil {
		t.Fatalf("WriteBatch() error = %v", err)
	}
	var balance int64
	if err := db.Model(&models.User{}).Where("id = ?", userID).
		Pluck("balance_nano_usd", &balance).Error; err != nil {
		t.Fatalf("query balance: %v", err)
	}
	if balance != 1000 {
		t.Fatalf("balance without deductor = %d, want 1000", balance)
	}
}
