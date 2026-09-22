package userdash

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"

	"gorm.io/gorm"

	"gpt-load/internal/storage/models"
)

// seedBalanceUser creates a user with a zero balance and one owned access key.
func seedBalanceUser(t *testing.T, env *testEnv) (uint, uint) {
	t.Helper()
	profile, err := env.service.Signup(context.Background(), "ledger@example.com", "password1")
	if err != nil {
		t.Fatalf("Signup() error = %v", err)
	}
	key := models.AccessKey{
		Name:      "test key",
		KeyValue:  "sk-gl-test",
		KeyHash:   "test-hash",
		KeySuffix: "test",
		UserID:    &profile.ID,
	}
	if err := env.db.Create(&key).Error; err != nil {
		t.Fatalf("create access key: %v", err)
	}
	return profile.ID, key.ID
}

func TestBalanceAdjustAndDeductAndReconcile(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	userID, keyID := seedBalanceUser(t, env)

	// (a) adjustments update balance and append ledger rows.
	if _, err := env.service.AdjustBalance(ctx, userID, 1000, "topup", "seed", "admin@example.com"); err != nil {
		t.Fatalf("AdjustBalance(+1000) error = %v", err)
	}
	if got, err := env.service.GetBalance(ctx, userID); err != nil || got != 1000 {
		t.Fatalf("GetBalance() = (%d, %v), want (1000, nil)", got, err)
	}
	if _, err := env.service.AdjustBalance(ctx, userID, -300, "correction", "fix", "admin@example.com"); err != nil {
		t.Fatalf("AdjustBalance(-300) error = %v", err)
	}
	if got, _ := env.service.GetBalance(ctx, userID); got != 700 {
		t.Fatalf("balance = %d, want 700", got)
	}
	adj, err := env.service.ListAdjustments(ctx, userID, 10, 0)
	if err != nil {
		t.Fatalf("ListAdjustments() error = %v", err)
	}
	if len(adj) != 2 || adj[0].DeltaNanoUSD+adj[1].DeltaNanoUSD != 700 {
		t.Fatalf("ledger rows = %+v, want two rows summing 700", adj)
	}

	// Seed a priced request log so ReconcileBalance sees the deduction.
	log := models.RequestLog{
		ID:                   "req-ledger-1",
		AccessKeyID:          keyID,
		Protocol:             "openai",
		ClientModel:          "gpt-test",
		Status:               "success",
		StatusCode:           200,
		CompletedAtMS:        env.now.UnixMilli(),
		DurationMs:           10,
		UsageState:           "complete",
		CostState:            "priced",
		PricingCompleteness:  "complete",
		EstimatedCostNanoUSD: 250,
	}
	if err := env.db.Create(&log).Error; err != nil {
		t.Fatalf("create request log: %v", err)
	}

	// (b) DeductCosts sums per-user costs and subtracts once.
	costs := []AccessKeyCost{{AccessKeyID: keyID, CostNanoUSD: 150}, {AccessKeyID: keyID, CostNanoUSD: 100}}
	if err := DeductCosts(ctx, env.db, costs); err != nil {
		t.Fatalf("DeductCosts() error = %v", err)
	}
	if got, _ := env.service.GetBalance(ctx, userID); got != 450 {
		t.Fatalf("balance after deduct = %d, want 450", got)
	}

	// Same rows inside a rolled-back tx leave balance untouched; a single
	// committed DeductCosts applies the arithmetic exactly once.
	tx := env.db.Begin()
	if err := DeductCosts(ctx, tx, costs); err != nil {
		t.Fatalf("DeductCosts() in tx error = %v", err)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got, _ := env.service.GetBalance(ctx, userID); got != 450 {
		t.Fatalf("balance after rolled-back deduct = %d, want 450", got)
	}

	// Overdraw is allowed: balance may go negative.
	if err := DeductCosts(ctx, env.db, []AccessKeyCost{{AccessKeyID: keyID, CostNanoUSD: 1000}}); err != nil {
		t.Fatalf("DeductCosts() overdraw error = %v", err)
	}
	if got, _ := env.service.GetBalance(ctx, userID); got != -550 {
		t.Fatalf("balance after overdraw = %d, want -550", got)
	}

	// (c) ReconcileBalance passes with the ledger intact (overdraw deductions
	// above are not journaled, so only the 250 log cost counts).
	if err := env.db.Model(&models.User{}).Where("id = ?", userID).
		Update("balance_nano_usd", 700-250).Error; err != nil {
		t.Fatalf("reset balance: %v", err)
	}
	if err := ReconcileBalance(ctx, env.db, userID); err != nil {
		t.Fatalf("ReconcileBalance() error = %v", err)
	}
	// ...and reports drift after corruption.
	if err := env.db.Model(&models.User{}).Where("id = ?", userID).
		Update("balance_nano_usd", 1).Error; err != nil {
		t.Fatalf("corrupt balance: %v", err)
	}
	if err := ReconcileBalance(ctx, env.db, userID); err == nil {
		t.Fatal("ReconcileBalance() after corruption error = nil, want drift error")
	}
}

func TestBalanceAdjustUnknownUser(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.service.AdjustBalance(context.Background(), 9999, 1, "topup", "", "admin"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("AdjustBalance(unknown) error = %v, want gorm.ErrRecordNotFound", err)
	}
}

func TestBalanceAdjustOverflowRejected(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	userID, _ := seedBalanceUser(t, env)

	if _, err := env.service.AdjustBalance(ctx, userID, 1, "topup", "", "admin"); err != nil {
		t.Fatalf("AdjustBalance(+1) error = %v", err)
	}
	if _, err := env.service.AdjustBalance(ctx, userID, math.MaxInt64, "topup", "", "admin"); !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("AdjustBalance(overflow) error = %v, want ErrBalanceOverflow", err)
	}
	if got, _ := env.service.GetBalance(ctx, userID); got != 1 {
		t.Fatalf("balance = %d, want 1 after rejected overflow", got)
	}
}

func TestBalanceConcurrentAdjust(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	userID, _ := seedBalanceUser(t, env)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := env.service.AdjustBalance(ctx, userID, 1, "topup", "", "admin"); err != nil {
				t.Errorf("AdjustBalance() error = %v", err)
			}
		}()
	}
	wg.Wait()

	if got, err := env.service.GetBalance(ctx, userID); err != nil || got != 10 {
		t.Fatalf("balance = (%d, %v), want (10, nil)", got, err)
	}
}
