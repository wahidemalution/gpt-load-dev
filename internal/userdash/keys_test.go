package userdash

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"gpt-load/internal/storage/models"
)

func TestKeysListCreateDeleteAndQuota(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	created, err := env.service.Signup(ctx, "keys@example.com", "password1")
	if err != nil {
		t.Fatalf("Signup() error = %v", err)
	}
	token, _, _, err := env.service.Login(ctx, "keys@example.com", "password1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if err := env.db.Model(&models.User{}).Where("id = ?", created.ID).
		Update("max_api_keys", 2).Error; err != nil {
		t.Fatalf("set max_api_keys: %v", err)
	}

	unowned := models.AccessKey{
		Name: "admin-key", KeyValue: "cipher-unowned", KeyHash: "unowned-hash",
		KeySuffix: "abcd", Status: "active",
	}
	if err := env.db.Create(&unowned).Error; err != nil {
		t.Fatalf("seed unowned key: %v", err)
	}

	empty := env.do(http.MethodGet, "/api/clientarea/keys", token, nil)
	if empty.Code != http.StatusOK {
		t.Fatalf("GET /keys = %d %s, want 200", empty.Code, empty.Body.String())
	}

	first := env.do(http.MethodPost, "/api/clientarea/keys", token, map[string]string{"name": "first"})
	if first.Code != http.StatusOK {
		t.Fatalf("POST /keys = %d %s, want 200", first.Code, first.Body.String())
	}
	firstPayload := decodeEnvelope(t, first)
	var firstData struct {
		Key     string       `json:"key"`
		Profile OwnedKeyView `json:"profile"`
	}
	if err := json.Unmarshal(firstPayload.Data, &firstData); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if !strings.HasPrefix(firstData.Key, "sk-gl-") {
		t.Fatalf("plaintext key = %q, want sk-gl- prefix", firstData.Key)
	}
	if firstData.Profile.ID == 0 || firstData.Profile.Status != "active" {
		t.Fatalf("create profile = %#v", firstData.Profile)
	}
	var row models.AccessKey
	if err := env.db.First(&row, firstData.Profile.ID).Error; err != nil {
		t.Fatalf("load created key: %v", err)
	}
	if row.UserID == nil || *row.UserID != created.ID {
		t.Fatalf("created key user_id = %v, want %d", row.UserID, created.ID)
	}
	if row.KeyValue == firstData.Key || row.KeyHash == firstData.Key {
		t.Fatal("plaintext key stored in the row")
	}
	if row.KeyHash == "" || row.KeySuffix != firstData.Key[len(firstData.Key)-4:] {
		t.Fatalf("key row hash/suffix = %q/%q", row.KeyHash, row.KeySuffix)
	}

	second := env.do(http.MethodPost, "/api/clientarea/keys", token, map[string]string{"name": "second"})
	if second.Code != http.StatusOK {
		t.Fatalf("second POST /keys = %d %s, want 200", second.Code, second.Body.String())
	}

	third := env.do(http.MethodPost, "/api/clientarea/keys", token, map[string]string{"name": "third"})
	if third.Code != http.StatusConflict {
		t.Fatalf("third POST /keys = %d %s, want 409", third.Code, third.Body.String())
	}

	forbidden := env.do(http.MethodDelete, "/api/clientarea/keys/"+fmt.Sprint(unowned.ID), token, nil)
	if forbidden.Code != http.StatusNotFound {
		t.Fatalf("DELETE unowned key = %d %s, want 404", forbidden.Code, forbidden.Body.String())
	}

	del := env.do(http.MethodDelete, "/api/clientarea/keys/"+fmt.Sprint(firstData.Profile.ID), token, nil)
	if del.Code != http.StatusOK {
		t.Fatalf("DELETE own key = %d %s, want 200", del.Code, del.Body.String())
	}
	var disabled models.AccessKey
	if err := env.db.First(&disabled, firstData.Profile.ID).Error; err != nil {
		t.Fatalf("reload deleted key: %v", err)
	}
	if disabled.Status != "disabled" {
		t.Fatalf("deleted key status = %q, want disabled", disabled.Status)
	}

	list := env.do(http.MethodGet, "/api/clientarea/keys", token, nil)
	listPayload := decodeEnvelope(t, list)
	var views []OwnedKeyView
	if err := json.Unmarshal(listPayload.Data, &views); err != nil {
		t.Fatalf("decode key list: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("key list = %d rows, want 2 owned keys", len(views))
	}
	for _, view := range views {
		if view.ID == unowned.ID {
			t.Fatalf("key list leaked unowned key %d", unowned.ID)
		}
	}
}

func TestKeysUsageScopedToOwnedKeys(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	created, err := env.service.Signup(ctx, "usage@example.com", "password1")
	if err != nil {
		t.Fatalf("Signup() error = %v", err)
	}
	token, _, _, err := env.service.Login(ctx, "usage@example.com", "password1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	owned := models.AccessKey{
		Name: "owned", KeyValue: "cipher-owned", KeyHash: "owned-hash",
		KeySuffix: "wxyz", Status: "active",
	}
	if err := env.db.Create(&owned).Error; err != nil {
		t.Fatalf("seed owned key: %v", err)
	}
	if err := env.db.Model(&models.AccessKey{}).Where("id = ?", owned.ID).
		Update("user_id", created.ID).Error; err != nil {
		t.Fatalf("attach owned key: %v", err)
	}
	unowned := models.AccessKey{
		Name: "admin", KeyValue: "cipher-unowned", KeyHash: "unowned-usage-hash",
		KeySuffix: "abcd", Status: "active",
	}
	if err := env.db.Create(&unowned).Error; err != nil {
		t.Fatalf("seed unowned key: %v", err)
	}

	bucketMS := env.now.Add(-time.Hour).UnixMilli()
	seed := []models.UsageStat{
		{BucketStartMS: bucketMS, AccessKeyID: owned.ID, Model: "gpt-5", RequestCount: 3, SuccessCount: 3, EstimatedCostNanoUSD: 1500,
			UncachedInputTokens: 100, OutputTokens: 40, CacheReadTokens: 7, CacheWrite5MTokens: 5, CacheWrite1HTokens: 3},
		{BucketStartMS: bucketMS, AccessKeyID: owned.ID, Model: "gpt-4", RequestCount: 1, SuccessCount: 1, EstimatedCostNanoUSD: 500},
		{BucketStartMS: bucketMS, AccessKeyID: unowned.ID, Model: "gpt-5", RequestCount: 100, SuccessCount: 100, EstimatedCostNanoUSD: 999999,
			UncachedInputTokens: 99999},
	}
	for i := range seed {
		if err := env.db.Create(&seed[i]).Error; err != nil {
			t.Fatalf("seed usage stat #%d: %v", i, err)
		}
	}

	// Two streaming request logs for gpt-5 (avg latency 150ms, avg TTFT 110ms)
	// plus one non-streaming log (no TTFT). One gpt-4 log carries no TTFT.
	logs := []models.RequestLog{
		{ID: "u-1", AccessKeyID: owned.ID, Protocol: "openai", ClientModel: "gpt-5", Status: "success",
			StatusCode: 200, CompletedAtMS: bucketMS, DurationMs: 100, FirstResponseMs: ptrInt64(80),
			UsageState: "complete", CostState: "priced", PricingCompleteness: "complete"},
		{ID: "u-2", AccessKeyID: owned.ID, Protocol: "openai", ClientModel: "gpt-5", Status: "success",
			StatusCode: 200, CompletedAtMS: bucketMS, DurationMs: 200, FirstResponseMs: ptrInt64(140),
			UsageState: "complete", CostState: "priced", PricingCompleteness: "complete"},
		{ID: "u-3", AccessKeyID: owned.ID, Protocol: "openai", ClientModel: "gpt-5", Status: "success",
			StatusCode: 200, CompletedAtMS: bucketMS, DurationMs: 300,
			UsageState: "complete", CostState: "priced", PricingCompleteness: "complete"},
		{ID: "u-4", AccessKeyID: owned.ID, Protocol: "openai", ClientModel: "gpt-4", Status: "success",
			StatusCode: 200, CompletedAtMS: bucketMS, DurationMs: 50,
			UsageState: "complete", CostState: "priced", PricingCompleteness: "complete"},
		{ID: "u-5", AccessKeyID: unowned.ID, Protocol: "openai", ClientModel: "gpt-5", Status: "success",
			StatusCode: 200, CompletedAtMS: bucketMS, DurationMs: 9000, FirstResponseMs: ptrInt64(9000),
			UsageState: "complete", CostState: "priced", PricingCompleteness: "complete"},
	}
	for i := range logs {
		if err := env.db.Create(&logs[i]).Error; err != nil {
			t.Fatalf("seed request log #%d: %v", i, err)
		}
	}

	fromMS := env.now.Add(-24 * time.Hour).UnixMilli()
	toMS := env.now.Add(time.Hour).UnixMilli()
	recorder := env.do(
		http.MethodGet,
		"/api/clientarea/usage?from_ms="+fmt.Sprint(fromMS)+"&to_ms="+fmt.Sprint(toMS),
		token, nil,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /usage = %d %s, want 200", recorder.Code, recorder.Body.String())
	}
	payload := decodeEnvelope(t, recorder)
	var rows []struct {
		Model                string `json:"model"`
		RequestCount         int64  `json:"request_count"`
		EstimatedCostNanoUSD int64  `json:"estimated_cost_nano_usd"`
		TokensIn             int64  `json:"tokens_in"`
		TokensOut            int64  `json:"tokens_out"`
		CacheReadTokens      int64  `json:"cache_read_tokens"`
		CacheWriteTokens     int64  `json:"cache_write_tokens"`
		AvgTTFTMs            *int64 `json:"avg_ttft_ms"`
		AvgLatencyMs         int64  `json:"avg_latency_ms"`
	}
	if err := json.Unmarshal(payload.Data, &rows); err != nil {
		t.Fatalf("decode usage rows: %v", err)
	}
	want := map[string][2]int64{
		"gpt-5": {3, 1500},
		"gpt-4": {1, 500},
	}
	if len(rows) != len(want) {
		t.Fatalf("usage rows = %#v, want %d model groups scoped to owned keys", rows, len(want))
	}
	for _, row := range rows {
		sums, ok := want[row.Model]
		if !ok || sums[0] != row.RequestCount || sums[1] != row.EstimatedCostNanoUSD {
			t.Fatalf("usage row %#v, want model=%v requests=%d cost=%d", row, row.Model, sums[0], sums[1])
		}
		switch row.Model {
		case "gpt-5":
			if row.TokensIn != 100 || row.TokensOut != 40 || row.CacheReadTokens != 7 || row.CacheWriteTokens != 8 {
				t.Fatalf("gpt-5 tokens = in:%d out:%d read:%d write:%d, want 100/40/7/8",
					row.TokensIn, row.TokensOut, row.CacheReadTokens, row.CacheWriteTokens)
			}
			if row.AvgTTFTMs == nil || *row.AvgTTFTMs != 110 {
				t.Fatalf("gpt-5 avg_ttft_ms = %v, want 110 (streaming logs only)", row.AvgTTFTMs)
			}
			if row.AvgLatencyMs != 200 {
				t.Fatalf("gpt-5 avg_latency_ms = %d, want 200", row.AvgLatencyMs)
			}
		case "gpt-4":
			if row.TokensIn != 0 || row.CacheWriteTokens != 0 {
				t.Fatalf("gpt-4 tokens = %#v, want zeroed", row)
			}
			if row.AvgTTFTMs != nil {
				t.Fatalf("gpt-4 avg_ttft_ms = %v, want nil (no streaming samples)", *row.AvgTTFTMs)
			}
			if row.AvgLatencyMs != 50 {
				t.Fatalf("gpt-4 avg_latency_ms = %d, want 50", row.AvgLatencyMs)
			}
		}
	}
}

func ptrInt64(value int64) *int64 { return &value }

func TestUsageDailyGranularityScopedAndAligned(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	created, err := env.service.Signup(ctx, "daily@example.com", "password1")
	if err != nil {
		t.Fatalf("Signup() error = %v", err)
	}
	token, _, _, err := env.service.Login(ctx, "daily@example.com", "password1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	owned := models.AccessKey{
		Name: "owned", KeyValue: "cipher-owned", KeyHash: "owned-hash",
		KeySuffix: "wxyz", Status: "active",
	}
	if err := env.db.Create(&owned).Error; err != nil {
		t.Fatalf("seed owned key: %v", err)
	}
	if err := env.db.Model(&models.AccessKey{}).Where("id = ?", owned.ID).
		Update("user_id", created.ID).Error; err != nil {
		t.Fatalf("attach owned key: %v", err)
	}
	unowned := models.AccessKey{
		Name: "admin", KeyValue: "cipher-unowned", KeyHash: "unowned-daily-hash",
		KeySuffix: "abcd", Status: "active",
	}
	if err := env.db.Create(&unowned).Error; err != nil {
		t.Fatalf("seed unowned key: %v", err)
	}

	// Two hourly buckets on the same UTC day for the owned key sum into one
	// day row; an unowned-key row on the same day must not leak in.
	day := time.Date(2026, time.September, 20, 0, 0, 0, 0, time.UTC)
	dayMS := day.UnixMilli()
	seed := []models.UsageStat{
		{BucketStartMS: dayMS + 2*time.Hour.Milliseconds(), AccessKeyID: owned.ID, Model: "gpt-5", RequestCount: 1, SuccessCount: 1, UncachedInputTokens: 100, OutputTokens: 50, CacheReadTokens: 10},
		{BucketStartMS: dayMS + 5*time.Hour.Milliseconds(), AccessKeyID: owned.ID, Model: "gpt-5", RequestCount: 1, SuccessCount: 1, UncachedInputTokens: 200, CacheWrite5MTokens: 25},
		{BucketStartMS: dayMS + 2*time.Hour.Milliseconds(), AccessKeyID: unowned.ID, Model: "gpt-5", RequestCount: 9, SuccessCount: 9, UncachedInputTokens: 99999},
	}
	for i := range seed {
		if err := env.db.Create(&seed[i]).Error; err != nil {
			t.Fatalf("seed usage stat #%d: %v", i, err)
		}
	}

	fromMS := day.Add(-time.Hour).UnixMilli()
	toMS := day.Add(24 * time.Hour).UnixMilli()
	recorder := env.do(
		http.MethodGet,
		"/api/clientarea/usage?granularity=day&from_ms="+fmt.Sprint(fromMS)+"&to_ms="+fmt.Sprint(toMS),
		token, nil,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /usage granularity=day = %d %s, want 200", recorder.Code, recorder.Body.String())
	}
	payload := decodeEnvelope(t, recorder)
	var body struct {
		Models []struct {
			Model        string `json:"model"`
			RequestCount int64  `json:"request_count"`
		} `json:"models"`
		Daily []DailyTokenRow `json:"daily"`
	}
	if err := json.Unmarshal(payload.Data, &body); err != nil {
		t.Fatalf("decode granularity=day response: %v", err)
	}
	if len(body.Models) != 1 || body.Models[0].Model != "gpt-5" || body.Models[0].RequestCount != 2 {
		t.Fatalf("models = %#v, want one owned gpt-5 row with 2 requests", body.Models)
	}
	// 100+50+10 + 200+25 = 385 tokens; unowned 99999 excluded.
	if len(body.Daily) != 1 || body.Daily[0].DayMS != dayMS || body.Daily[0].TotalTokens != 385 {
		t.Fatalf("daily = %#v, want one row day_ms=%d tokens=385", body.Daily, dayMS)
	}

	// The default shape stays a bare per-model array with no daily field.
	legacy := env.do(
		http.MethodGet,
		"/api/clientarea/usage?from_ms="+fmt.Sprint(fromMS)+"&to_ms="+fmt.Sprint(toMS),
		token, nil,
	)
	var legacyRows []UsageRow
	if err := json.Unmarshal(decodeEnvelope(t, legacy).Data, &legacyRows); err != nil {
		t.Fatalf("decode default usage response: %v", err)
	}
	if len(legacyRows) != 1 || legacyRows[0].RequestCount != 2 || legacyRows[0].Model != "gpt-5" {
		t.Fatalf("legacy rows = %#v, want one owned gpt-5 row", legacyRows)
	}
}

func TestKeysListPopulatesLifetimeUsage(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	created, err := env.service.Signup(ctx, "keyusage@example.com", "password1")
	if err != nil {
		t.Fatalf("Signup() error = %v", err)
	}
	token, _, _, err := env.service.Login(ctx, "keyusage@example.com", "password1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	withUsage := models.AccessKey{
		Name: "used", KeyValue: "cipher-used", KeyHash: "used-hash",
		KeySuffix: "used", Status: "active", UserID: &created.ID,
	}
	withoutUsage := models.AccessKey{
		Name: "idle", KeyValue: "cipher-idle", KeyHash: "idle-hash",
		KeySuffix: "idle", Status: "active", UserID: &created.ID,
	}
	for _, key := range []*models.AccessKey{&withUsage, &withoutUsage} {
		if err := env.db.Create(key).Error; err != nil {
			t.Fatalf("seed owned key: %v", err)
		}
	}

	// Lifetime usage for the first key only, split across two buckets/models.
	recentBucket := env.now.Add(-time.Hour).UnixMilli()
	oldBucket := env.now.Add(-40 * 24 * time.Hour).UnixMilli()
	stats := []models.UsageStat{
		{BucketStartMS: recentBucket, AccessKeyID: withUsage.ID, Model: "gpt-5", RequestCount: 3, SuccessCount: 3, EstimatedCostNanoUSD: 1500},
		{BucketStartMS: recentBucket, AccessKeyID: withUsage.ID, Model: "gpt-4", RequestCount: 2, SuccessCount: 2, EstimatedCostNanoUSD: 500},
		// Old bucket still counts: the aggregate is lifetime, not windowed.
		{BucketStartMS: oldBucket, AccessKeyID: withUsage.ID, Model: "gpt-3", RequestCount: 9, SuccessCount: 9, EstimatedCostNanoUSD: 9000},
	}
	for i := range stats {
		if err := env.db.Create(&stats[i]).Error; err != nil {
			t.Fatalf("seed usage stat #%d: %v", i, err)
		}
	}
	lastRequestMS := env.now.Add(-2 * time.Hour).UnixMilli()
	log := models.RequestLog{
		ID: "req-key-usage", AccessKeyID: withUsage.ID, Protocol: "openai",
		ClientModel: "gpt-5", Status: "success", StatusCode: 200,
		CompletedAtMS: lastRequestMS, DurationMs: 10,
		UsageState: "complete", CostState: "priced", PricingCompleteness: "complete",
		EstimatedCostNanoUSD: 1500,
	}
	if err := env.db.Create(&log).Error; err != nil {
		t.Fatalf("seed request log: %v", err)
	}

	recorder := env.do(http.MethodGet, "/api/clientarea/keys", token, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /keys = %d %s, want 200", recorder.Code, recorder.Body.String())
	}
	payload := decodeEnvelope(t, recorder)
	var views []OwnedKeyView
	if err := json.Unmarshal(payload.Data, &views); err != nil {
		t.Fatalf("decode key list: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("key list = %d rows, want 2", len(views))
	}
	byID := make(map[uint]OwnedKeyView, len(views))
	for _, view := range views {
		byID[view.ID] = view
	}

	used, ok := byID[withUsage.ID]
	if !ok {
		t.Fatalf("key %d missing from list", withUsage.ID)
	}
	if used.UsageTotalNanoUSD != 11000 {
		t.Fatalf("used key usage = %d nano, want 11000 lifetime", used.UsageTotalNanoUSD)
	}
	if used.LastRequestAtMS == nil || *used.LastRequestAtMS != lastRequestMS {
		t.Fatalf("used key last_request_at_ms = %v, want %d", used.LastRequestAtMS, lastRequestMS)
	}

	idle, ok := byID[withoutUsage.ID]
	if !ok {
		t.Fatalf("key %d missing from list", withoutUsage.ID)
	}
	if idle.UsageTotalNanoUSD != 0 {
		t.Fatalf("idle key usage = %d nano, want 0", idle.UsageTotalNanoUSD)
	}
	if idle.LastRequestAtMS != nil {
		t.Fatalf("idle key last_request_at_ms = %v, want nil", *idle.LastRequestAtMS)
	}
}

func TestOwnedKeyMutationsRepublishRuntimeState(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	created, err := env.service.Signup(ctx, "refresh@example.com", "password1")
	if err != nil {
		t.Fatalf("Signup() error = %v", err)
	}

	refreshes := 0
	env.service.WithStateRefresher(func(context.Context) error {
		refreshes++
		return nil
	})

	view, _, err := env.service.CreateOwnedKey(ctx, created.ID, "refresh-key")
	if err != nil {
		t.Fatalf("CreateOwnedKey() error = %v", err)
	}
	if refreshes != 1 {
		t.Fatalf("refreshes after create = %d, want 1 (gateway must observe the new key)", refreshes)
	}

	if err := env.service.SetOwnedKeyStatus(ctx, created.ID, view.ID, "disabled"); err != nil {
		t.Fatalf("SetOwnedKeyStatus() error = %v", err)
	}
	if refreshes != 2 {
		t.Fatalf("refreshes after disable = %d, want 2 (gateway must observe the disabled key)", refreshes)
	}
}

func TestAdjustBalanceInvalidatesBalanceGate(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	created, err := env.service.Signup(ctx, "invalidate@example.com", "password1")
	if err != nil {
		t.Fatalf("Signup() error = %v", err)
	}

	invalidations := 0
	env.service.WithBalanceInvalidator(func() { invalidations++ })

	if _, err := env.service.AdjustBalance(ctx, created.ID, 500, "topup", "seed", "admin"); err != nil {
		t.Fatalf("AdjustBalance() error = %v", err)
	}
	if invalidations != 1 {
		t.Fatalf("invalidations after AdjustBalance = %d, want 1 (a topped-up user must not stay 402)", invalidations)
	}
}

func TestCreateAdminUserWithBalanceInvalidatesBalanceGate(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	invalidations := 0
	env.service.WithBalanceInvalidator(func() { invalidations++ })

	if _, _, err := env.service.CreateAdminUser(ctx, AdminCreateInput{
		Email: "invalidate-create@example.com", Password: "password1", BalanceNanoUSD: 1000,
	}); err != nil {
		t.Fatalf("CreateAdminUser() error = %v", err)
	}
	if invalidations != 1 {
		t.Fatalf("invalidations after CreateAdminUser initial balance = %d, want 1", invalidations)
	}
}

func TestPatchAdminUserDisableRepublishesRuntimeState(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	created, err := env.service.Signup(ctx, "cascade@example.com", "password1")
	if err != nil {
		t.Fatalf("Signup() error = %v", err)
	}
	if _, _, err := env.service.CreateOwnedKey(ctx, created.ID, "cascade-key"); err != nil {
		t.Fatalf("CreateOwnedKey() error = %v", err)
	}

	refreshes := 0
	env.service.WithStateRefresher(func(context.Context) error {
		refreshes++
		return nil
	})

	disabled := models.UserStatusDisabled
	if _, err := env.service.PatchAdminUser(ctx, created.ID, AdminPatchInput{Status: &disabled}); err != nil {
		t.Fatalf("PatchAdminUser() error = %v", err)
	}
	if refreshes != 1 {
		t.Fatalf("refreshes after disable cascade = %d, want 1", refreshes)
	}
}
