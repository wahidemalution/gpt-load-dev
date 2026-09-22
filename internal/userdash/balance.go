package userdash

import (
	"context"
	"errors"
	"fmt"
	"math"

	"gorm.io/gorm"

	"gpt-load/internal/storage/models"
)

var ErrBalanceOverflow = errors.New("userdash: balance adjustment would overflow int64")

// AccessKeyCost is one deducted request cost attributed to an access key.
type AccessKeyCost struct {
	AccessKeyID uint
	CostNanoUSD int64
}

// AdjustBalance applies a manual ledger delta and the matching balance update
// in one transaction. The overflow guard runs inside the transaction as a
// SQL-level WHERE clause so concurrent adjustments cannot both pass a stale
// application-side check (TOCTOU) and wrap inside the database.
func (s *Service) AdjustBalance(ctx context.Context, userID uint, deltaNanoUSD int64, reason, adminNote, createdByAdmin string) (Profile, error) {
	if deltaNanoUSD == 0 {
		return Profile{}, fmt.Errorf("%w: zero-delta adjustment", ErrValidation)
	}

	var profile Profile
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Sign-aware atomic guarded update: positive deltas cannot push the
		// balance past MaxInt64; negative deltas cannot underflow past
		// MinInt64. RowsAffected == 0 means the user is unknown OR the guard
		// rejected the delta; distinguish by an explicit existence probe.
		result := tx.Model(&models.User{}).Where("id = ?", userID).
			Where(
				"(? >= 0 AND balance_nano_usd <= ?) OR (? < 0 AND balance_nano_usd >= ?)",
				deltaNanoUSD, math.MaxInt64-deltaNanoUSD,
				deltaNanoUSD, math.MinInt64-deltaNanoUSD,
			).
			Update("balance_nano_usd", gorm.Expr("balance_nano_usd + ?", deltaNanoUSD))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			var exists int64
			if err := tx.Model(&models.User{}).Where("id = ?", userID).Count(&exists).Error; err != nil {
				return err
			}
			if exists == 0 {
				return gorm.ErrRecordNotFound
			}
			return ErrBalanceOverflow
		}
		row := models.UserBalanceAdjustment{
			UserID: userID, DeltaNanoUSD: deltaNanoUSD, Reason: reason,
			AdminNote: adminNote, CreatedByAdmin: createdByAdmin,
			CreatedAtMS: s.now().UnixMilli(),
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		var user models.User
		if err := tx.First(&user, userID).Error; err != nil {
			return err
		}
		profile = profileFromRow(&user)
		return nil
	})
	return profile, err
}

// ListAdjustments returns the user's balance ledger rows, newest first.
func (s *Service) ListAdjustments(ctx context.Context, userID uint, limit, offset int) ([]models.UserBalanceAdjustment, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows := make([]models.UserBalanceAdjustment, 0, limit)
	err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("id DESC").
		Limit(limit).Offset(offset).
		Find(&rows).Error
	return rows, err
}

// GetBalance returns the user's current nano-USD balance.
func (s *Service) GetBalance(ctx context.Context, userID uint) (int64, error) {
	var user models.User
	if err := s.db.WithContext(ctx).Select("balance_nano_usd").First(&user, userID).Error; err != nil {
		return 0, err
	}
	return user.BalanceNanoUSD, nil
}

// DeductCosts subtracts per-key request costs from owner balances. The caller
// guarantees idempotency (only invoked on first journal commit).
func DeductCosts(ctx context.Context, tx *gorm.DB, costs []AccessKeyCost) error {
	if len(costs) == 0 || tx == nil {
		return nil
	}
	perUser := make(map[uint]int64, len(costs))
	keyOwner := make(map[uint]uint, len(costs))
	var keys []models.AccessKey
	ids := make([]uint, 0, len(costs))
	for _, cost := range costs {
		ids = append(ids, cost.AccessKeyID)
	}
	if err := tx.WithContext(ctx).Where("id IN ? AND user_id IS NOT NULL", ids).Find(&keys).Error; err != nil {
		return err
	}
	for _, key := range keys {
		keyOwner[key.ID] = *key.UserID
	}
	for _, cost := range costs {
		userID, ok := keyOwner[cost.AccessKeyID]
		if !ok {
			continue
		}
		perUser[userID] += cost.CostNanoUSD // caller pre-checked overflow on journals
	}
	for userID, nano := range perUser {
		if nano == 0 {
			continue
		}
		if err := tx.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).
			Update("balance_nano_usd", gorm.Expr("balance_nano_usd - ?", nano)).Error; err != nil {
			return err
		}
	}
	return nil
}

// ReconcileBalance asserts the money invariant:
// balance == SUM(adjustments) - SUM(deducted request-log costs attributed to
// the user's access keys). Test/ops helper; returns a descriptive error on drift.
func ReconcileBalance(ctx context.Context, db *gorm.DB, userID uint) error {
	if db == nil {
		return errors.New("userdash: reconcile requires a db handle")
	}
	var balance int64
	if err := db.WithContext(ctx).Model(&models.User{}).Select("balance_nano_usd").
		Where("id = ?", userID).Scan(&balance).Error; err != nil {
		return fmt.Errorf("userdash: load balance: %w", err)
	}
	var adjustments int64
	if err := db.WithContext(ctx).Model(&models.UserBalanceAdjustment{}).
		Select("COALESCE(SUM(delta_nano_usd), 0)").
		Where("user_id = ?", userID).Scan(&adjustments).Error; err != nil {
		return fmt.Errorf("userdash: sum adjustments: %w", err)
	}
	var deducted int64
	if err := db.WithContext(ctx).Raw(`
		SELECT COALESCE(SUM(r.estimated_cost_nano_usd), 0)
		FROM request_logs r
		JOIN access_keys k ON k.id = r.access_key_id
		WHERE k.user_id = ?`, userID).Scan(&deducted).Error; err != nil {
		return fmt.Errorf("userdash: sum deducted costs: %w", err)
	}
	expected := adjustments - deducted
	if balance != expected {
		return fmt.Errorf(
			"userdash: balance drift for user %d: balance = %d, want %d (adjustments %d - deducted %d)",
			userID, balance, expected, adjustments, deducted)
	}
	return nil
}
