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
		{BucketStartMS: bucketMS, AccessKeyID: owned.ID, Model: "gpt-5", RequestCount: 3, SuccessCount: 3, EstimatedCostNanoUSD: 1500},
		{BucketStartMS: bucketMS, AccessKeyID: owned.ID, Model: "gpt-4", RequestCount: 1, SuccessCount: 1, EstimatedCostNanoUSD: 500},
		{BucketStartMS: bucketMS, AccessKeyID: unowned.ID, Model: "gpt-5", RequestCount: 100, SuccessCount: 100, EstimatedCostNanoUSD: 999999},
	}
	for i := range seed {
		if err := env.db.Create(&seed[i]).Error; err != nil {
			t.Fatalf("seed usage stat #%d: %v", i, err)
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

	if err := env.service.DisableOwnedKey(ctx, created.ID, view.ID); err != nil {
		t.Fatalf("DisableOwnedKey() error = %v", err)
	}
	if refreshes != 2 {
		t.Fatalf("refreshes after disable = %d, want 2 (gateway must observe the disabled key)", refreshes)
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
