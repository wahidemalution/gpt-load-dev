package userdash

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"gpt-load/internal/storage/models"
)

func TestOwnedKeyLimitsAndStatus(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	owner, err := env.service.Signup(ctx, "limits@example.com", "password1")
	if err != nil {
		t.Fatalf("Signup() error = %v", err)
	}
	token, _, _, err := env.service.Login(ctx, "limits@example.com", "password1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if _, err := env.service.Signup(ctx, "other@example.com", "password1"); err != nil {
		t.Fatalf("Signup() other error = %v", err)
	}

	created, _, err := env.service.CreateOwnedKey(ctx, owner.ID, "limited")
	if err != nil {
		t.Fatalf("CreateOwnedKey() error = %v", err)
	}

	// 1. Set limits; reload the row and cost rule.
	recorder := env.do(http.MethodPatch, "/api/clientarea/keys/"+fmt.Sprint(created.ID)+"/limits", token, map[string]any{
		"rpm_limit":             5,
		"max_concurrent":        2,
		"credit_limit_nano_usd": int64(1_000_000_000),
		"models":                []string{"gpt-4o"},
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("PATCH limits = %d %s, want 200", recorder.Code, recorder.Body.String())
	}
	var row models.AccessKey
	if err := env.db.First(&row, created.ID).Error; err != nil {
		t.Fatalf("reload key: %v", err)
	}
	if row.RPMLimit != 5 {
		t.Fatalf("rpm_limit = %d, want 5", row.RPMLimit)
	}
	if row.MaxConcurrent != 2 {
		t.Fatalf("max_concurrent = %d, want 2", row.MaxConcurrent)
	}
	var rule models.AccessKeyCostLimitRule
	if err := env.db.Where("access_key_id = ? AND kind = ?", created.ID, models.AccessKeyCostLimitKindTotal).
		Take(&rule).Error; err != nil {
		t.Fatalf("load total rule: %v", err)
	}
	if rule.LimitNanoUSD != 1_000_000_000 {
		t.Fatalf("total rule limit = %d, want 1000000000", rule.LimitNanoUSD)
	}
	if !storedFiltersContainModel(row.Filters, "gpt-4o") {
		t.Fatalf("stored filters %s missing gpt-4o", string(row.Filters))
	}

	// 2. GET /keys returns the new fields and usage_total_nano_usd.
	if err := env.db.Create(&models.UsageStat{
		BucketStartMS: env.now.Add(-time.Hour).UnixMilli(), AccessKeyID: created.ID,
		Model: "gpt-4o", RequestCount: 2, SuccessCount: 2, EstimatedCostNanoUSD: 777,
	}).Error; err != nil {
		t.Fatalf("seed usage stat: %v", err)
	}
	list := env.do(http.MethodGet, "/api/clientarea/keys", token, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("GET /keys = %d %s, want 200", list.Code, list.Body.String())
	}
	var views []OwnedKeyView
	if err := json.Unmarshal(decodeEnvelope(t, list).Data, &views); err != nil {
		t.Fatalf("decode key list: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("key list = %d rows, want 1", len(views))
	}
	view := views[0]
	if view.RPMLimit != 5 || view.MaxConcurrent != 2 || view.CreditLimitNanoUSD != 1_000_000_000 {
		t.Fatalf("view limits = %#v", view)
	}
	if view.UsageTotalNanoUSD != 777 {
		t.Fatalf("usage_total_nano_usd = %d, want 777", view.UsageTotalNanoUSD)
	}
	if len(view.Models) != 1 || view.Models[0] != "gpt-4o" {
		t.Fatalf("view models = %#v, want [gpt-4o]", view.Models)
	}

	// 3. PATCH another user's key id -> 404.
	otherToken, _, _, err := env.service.Login(ctx, "other@example.com", "password1")
	if err != nil {
		t.Fatalf("Login() other error = %v", err)
	}
	cross := env.do(http.MethodPatch, "/api/clientarea/keys/"+fmt.Sprint(created.ID)+"/limits", otherToken, map[string]any{
		"rpm_limit": 9,
	})
	if cross.Code != http.StatusNotFound {
		t.Fatalf("PATCH other's key = %d %s, want 404", cross.Code, cross.Body.String())
	}
	crossStatus := env.do(http.MethodPatch, "/api/clientarea/keys/"+fmt.Sprint(created.ID)+"/status", otherToken, map[string]string{
		"status": "disabled",
	})
	if crossStatus.Code != http.StatusNotFound {
		t.Fatalf("PATCH other's key status = %d %s, want 404", crossStatus.Code, crossStatus.Body.String())
	}

	// 4. status disabled then active flips and re-enables.
	disable := env.do(http.MethodPatch, "/api/clientarea/keys/"+fmt.Sprint(created.ID)+"/status", token, map[string]string{
		"status": "disabled",
	})
	if disable.Code != http.StatusOK {
		t.Fatalf("PATCH status disabled = %d %s, want 200", disable.Code, disable.Body.String())
	}
	if err := env.db.First(&row, created.ID).Error; err != nil {
		t.Fatalf("reload disabled key: %v", err)
	}
	if row.Status != "disabled" {
		t.Fatalf("status = %q, want disabled", row.Status)
	}
	enable := env.do(http.MethodPatch, "/api/clientarea/keys/"+fmt.Sprint(created.ID)+"/status", token, map[string]string{
		"status": "active",
	})
	if enable.Code != http.StatusOK {
		t.Fatalf("PATCH status active = %d %s, want 200", enable.Code, enable.Body.String())
	}
	if err := env.db.First(&row, created.ID).Error; err != nil {
		t.Fatalf("reload enabled key: %v", err)
	}
	if row.Status != "active" {
		t.Fatalf("status = %q, want active", row.Status)
	}
	badStatus := env.do(http.MethodPatch, "/api/clientarea/keys/"+fmt.Sprint(created.ID)+"/status", token, map[string]string{
		"status": "deleted",
	})
	if badStatus.Code != http.StatusBadRequest {
		t.Fatalf("PATCH bad status = %d %s, want 400", badStatus.Code, badStatus.Body.String())
	}

	// Updating the credit twice keeps the rule and its checkpoint revision in
	// lockstep (the runtime restore path rejects a mismatch).
	first := env.do(http.MethodPatch, "/api/clientarea/keys/"+fmt.Sprint(created.ID)+"/limits", token, map[string]any{
		"credit_limit_nano_usd": int64(500),
	})
	if first.Code != http.StatusOK {
		t.Fatalf("PATCH first credit = %d %s, want 200", first.Code, first.Body.String())
	}
	second := env.do(http.MethodPatch, "/api/clientarea/keys/"+fmt.Sprint(created.ID)+"/limits", token, map[string]any{
		"credit_limit_nano_usd": int64(600),
	})
	if second.Code != http.StatusOK {
		t.Fatalf("PATCH second credit = %d %s, want 200", second.Code, second.Body.String())
	}
	if err := env.db.Where("access_key_id = ? AND kind = ?", created.ID, models.AccessKeyCostLimitKindTotal).
		Take(&rule).Error; err != nil {
		t.Fatalf("reload total rule: %v", err)
	}
	if rule.LimitNanoUSD != 600 || rule.RuleRevision != 3 {
		t.Fatalf("total rule = %d limit / rev %d, want 600 / 3", rule.LimitNanoUSD, rule.RuleRevision)
	}
	var state models.AccessKeyCostLimitState
	if err := env.db.First(&state, rule.ID).Error; err != nil {
		t.Fatalf("load rule state: %v", err)
	}
	if state.RuleRevision != rule.RuleRevision {
		t.Fatalf("state revision = %d, rule revision = %d, want equal", state.RuleRevision, rule.RuleRevision)
	}

	// 5. credit 0 clears the total rule; models [] clears the allowlist.
	clear := env.do(http.MethodPatch, "/api/clientarea/keys/"+fmt.Sprint(created.ID)+"/limits", token, map[string]any{
		"credit_limit_nano_usd": 0,
		"models":                []string{},
	})
	if clear.Code != http.StatusOK {
		t.Fatalf("PATCH clear = %d %s, want 200", clear.Code, clear.Body.String())
	}
	var count int64
	if err := env.db.Model(&models.AccessKeyCostLimitRule{}).
		Where("access_key_id = ? AND kind = ?", created.ID, models.AccessKeyCostLimitKindTotal).
		Count(&count).Error; err != nil {
		t.Fatalf("count total rules: %v", err)
	}
	if count != 0 {
		t.Fatalf("total rules = %d, want 0 after credit clear", count)
	}
	if err := env.db.First(&row, created.ID).Error; err != nil {
		t.Fatalf("reload cleared key: %v", err)
	}
	if storedFiltersContainModel(row.Filters, "gpt-4o") {
		t.Fatalf("stored filters %s still contains gpt-4o after clear", string(row.Filters))
	}

	// Negative values are rejected without persisting.
	negative := env.do(http.MethodPatch, "/api/clientarea/keys/"+fmt.Sprint(created.ID)+"/limits", token, map[string]any{
		"rpm_limit": -1,
	})
	if negative.Code != http.StatusBadRequest {
		t.Fatalf("PATCH negative rpm = %d %s, want 400", negative.Code, negative.Body.String())
	}
}

func storedFiltersContainModel(raw models.JSON, model string) bool {
	if len(raw) == 0 {
		return false
	}
	var decoded struct {
		Models []string `json:"models"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return false
	}
	for _, value := range decoded.Models {
		if value == model {
			return true
		}
	}
	return false
}
