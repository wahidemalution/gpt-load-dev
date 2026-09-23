package userdash

import (
	"context"
	"testing"
	"time"

	"gpt-load/internal/storage/models"
	"gpt-load/internal/testutil/encryptiontest"
)

func TestGetAdminUserExposesKeyLimits(t *testing.T) {
	env := newTestEnv(t)
	env.service = env.service.WithEncryptor(encryptiontest.Service(t, "userdash-test-encryption-key"))
	ctx := context.Background()

	owner, err := env.service.Signup(ctx, "admin-key-limits@example.com", "password1")
	if err != nil {
		t.Fatalf("Signup() error = %v", err)
	}
	key, _, err := env.service.CreateOwnedKey(ctx, owner.ID, "limited")
	if err != nil {
		t.Fatalf("CreateOwnedKey() error = %v", err)
	}

	rpmLimit := int64(7)
	maxConcurrent := int64(4)
	credit := int64(2_000_000_000)
	allowedModels := []string{"gpt-4o"}
	if err := env.service.SetOwnedKeyLimits(ctx, owner.ID, key.ID, OwnedKeyLimitsInput{
		RPMLimit: &rpmLimit, MaxConcurrent: &maxConcurrent,
		CreditLimitNanoUSD: &credit, Models: &allowedModels,
	}); err != nil {
		t.Fatalf("SetOwnedKeyLimits() error = %v", err)
	}
	if err := env.db.Create(&models.UsageStat{
		BucketStartMS: env.now.Add(-time.Hour).UnixMilli(), AccessKeyID: key.ID,
		Model: "gpt-4o", RequestCount: 1, SuccessCount: 1, EstimatedCostNanoUSD: 1234,
	}).Error; err != nil {
		t.Fatalf("seed usage stat: %v", err)
	}

	detail, err := env.service.GetAdminUser(ctx, owner.ID)
	if err != nil {
		t.Fatalf("GetAdminUser() error = %v", err)
	}
	if len(detail.Keys) != 1 {
		t.Fatalf("detail keys = %d, want 1", len(detail.Keys))
	}
	view := detail.Keys[0]
	if view.RPMLimit != 7 || view.MaxConcurrent != 4 {
		t.Fatalf("view limits = rpm %d / concurrent %d, want 7/4", view.RPMLimit, view.MaxConcurrent)
	}
	if view.CreditLimitNanoUSD != 2_000_000_000 {
		t.Fatalf("credit_limit_nano_usd = %d, want 2000000000", view.CreditLimitNanoUSD)
	}
	if len(view.Models) != 1 || view.Models[0] != "gpt-4o" {
		t.Fatalf("models = %#v, want [gpt-4o]", view.Models)
	}
	if view.UsageTotalNanoUSD != 1234 {
		t.Fatalf("usage_total_nano_usd = %d, want 1234", view.UsageTotalNanoUSD)
	}
}
