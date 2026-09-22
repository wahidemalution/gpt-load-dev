package userdash

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"gpt-load/internal/platform/httproute"
	"gpt-load/internal/platform/i18n"
	"gpt-load/internal/storage/models"
	"gpt-load/internal/testutil/encryptiontest"
)

type adminTestEnv struct {
	*testEnv
	engine *gin.Engine
	t      *testing.T
}

// newAdminTestEnv binds the admin module with a stubbed authenticator, mirroring
// the control route tests that stub Authenticate. The real admin auth is
// control's authenticate() middleware, exercised by control's own tests.
func newAdminTestEnv(t *testing.T) *adminTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := i18n.Init(); err != nil {
		t.Fatalf("i18n.Init() error = %v", err)
	}
	env := &adminTestEnv{testEnv: newTestEnv(t), t: t}
	env.service = env.service.WithEncryptor(encryptiontest.Service(t, "userdash-test-encryption-key"))
	module := NewAdminHTTPModule(env.service, func(c *gin.Context) { c.Next() })
	engine := gin.New()
	registry, err := httproute.NewRegistry(module.Module())
	if err != nil {
		t.Fatalf("NewRegistry(adminusers) error = %v", err)
	}
	if err := registry.Bind(engine); err != nil {
		t.Fatalf("Bind(adminusers) error = %v", err)
	}
	env.engine = engine
	return env
}

func (e *adminTestEnv) do(method, path, body string) *httptest.ResponseRecorder {
	var raw []byte
	if body != "" {
		raw = []byte(body)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	e.engine.ServeHTTP(recorder, request)
	return recorder
}

func (e *adminTestEnv) signup(email string) Profile {
	t := e.t
	profile, err := e.service.Signup(context.Background(), email, "password1")
	if err != nil {
		t.Fatalf("Signup(%q) error = %v", email, err)
	}
	return profile
}

func TestAdminHTTPListUsers(t *testing.T) {
	env := newAdminTestEnv(t)
	ctx := context.Background()

	alice := env.signup("admin-list-alice@example.com")
	env.signup("admin-list-bob@example.com")
	env.signup("admin-list-carol@example.com")
	if _, _, _, err := env.service.Login(ctx, "admin-list-alice@example.com", "password1"); err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	recorder := env.do(http.MethodGet, "/api/admin/users", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /users = %d %s, want 200", recorder.Code, recorder.Body.String())
	}
	payload := decodeEnvelope(t, recorder)
	var list struct {
		Items []AdminUserView `json:"items"`
		Total int64           `json:"total"`
	}
	if err := json.Unmarshal(payload.Data, &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Total != 3 || len(list.Items) != 3 {
		t.Fatalf("list = total %d, %d items, want 3/3", list.Total, len(list.Items))
	}
	var aliceView *AdminUserView
	for i := range list.Items {
		if list.Items[i].ID == alice.ID {
			aliceView = &list.Items[i]
		}
	}
	if aliceView == nil {
		t.Fatalf("alice missing from list: %#v", list.Items)
	}
	if aliceView.Email != "admin-list-alice@example.com" ||
		aliceView.Status != models.UserStatusActive ||
		aliceView.BalanceNanoUSD != 0 ||
		aliceView.MaxAPIKeys != 3 ||
		aliceView.KeyCount != 0 ||
		aliceView.CreatedAtMS <= 0 ||
		aliceView.LastLoginAtMS <= aliceView.CreatedAtMS {
		t.Fatalf("alice view = %#v", aliceView)
	}

	search := env.do(http.MethodGet, "/api/admin/users?search=bob", "")
	if search.Code != http.StatusOK {
		t.Fatalf("GET /users?search = %d %s, want 200", search.Code, search.Body.String())
	}
	searchPayload := decodeEnvelope(t, search)
	var searchList struct {
		Items []AdminUserView `json:"items"`
		Total int64           `json:"total"`
	}
	if err := json.Unmarshal(searchPayload.Data, &searchList); err != nil {
		t.Fatalf("decode search list: %v", err)
	}
	if searchList.Total != 1 || len(searchList.Items) != 1 || searchList.Items[0].Email != "admin-list-bob@example.com" {
		t.Fatalf("search list = %#v", searchList)
	}

	paged := env.do(http.MethodGet, "/api/admin/users?page=2&page_size=2", "")
	pagedPayload := decodeEnvelope(t, paged)
	var pagedList struct {
		Items []AdminUserView `json:"items"`
		Total int64           `json:"total"`
	}
	if err := json.Unmarshal(pagedPayload.Data, &pagedList); err != nil {
		t.Fatalf("decode paged list: %v", err)
	}
	if pagedList.Total != 3 || len(pagedList.Items) != 1 {
		t.Fatalf("paged list = total %d, %d items, want total 3, 1 item", pagedList.Total, len(pagedList.Items))
	}

	badPage := env.do(http.MethodGet, "/api/admin/users?page=0", "")
	if badPage.Code != http.StatusBadRequest {
		t.Fatalf("GET /users?page=0 = %d, want 400", badPage.Code)
	}
}

func TestAdminHTTPCreateUser(t *testing.T) {
	env := newAdminTestEnv(t)

	recorder := env.do(http.MethodPost, "/api/admin/users",
		`{"email":"Admin-Created@Example.com","password":"password1","max_api_keys":5,"balance_nano_usd":12345}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /users = %d %s, want 200", recorder.Code, recorder.Body.String())
	}
	payload := decodeEnvelope(t, recorder)
	var created struct {
		Profile         Profile `json:"profile"`
		InitialPassword string  `json:"initial_password"`
	}
	if err := json.Unmarshal(payload.Data, &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Profile.ID == 0 ||
		created.Profile.Email != "admin-created@example.com" ||
		created.Profile.BalanceNanoUSD != 12345 ||
		created.Profile.MaxAPIKeys != 5 {
		t.Fatalf("created profile = %#v", created.Profile)
	}
	if created.InitialPassword != "" {
		t.Fatalf("initial_password leaked when admin supplied a password: %q", created.InitialPassword)
	}

	duplicate := env.do(http.MethodPost, "/api/admin/users",
		`{"email":"admin-created@example.com","password":"password2"}`)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate create = %d %s, want 409", duplicate.Code, duplicate.Body.String())
	}

	badBody := env.do(http.MethodPost, "/api/admin/users", `{not json`)
	if badBody.Code != http.StatusBadRequest {
		t.Fatalf("malformed create = %d, want 400", badBody.Code)
	}

	badPassword := env.do(http.MethodPost, "/api/admin/users", `{"email":"x@example.com","password":"short"}`)
	if badPassword.Code != http.StatusBadRequest {
		t.Fatalf("weak password create = %d, want 400", badPassword.Code)
	}
}

func TestAdminHTTPCreateUserGeneratedPassword(t *testing.T) {
	env := newAdminTestEnv(t)

	recorder := env.do(http.MethodPost, "/api/admin/users", `{"email":"generated@example.com"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /users = %d %s, want 200", recorder.Code, recorder.Body.String())
	}
	payload := decodeEnvelope(t, recorder)
	var created struct {
		Profile         Profile `json:"profile"`
		InitialPassword string  `json:"initial_password"`
	}
	if err := json.Unmarshal(payload.Data, &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if len(created.InitialPassword) < 8 {
		t.Fatalf("generated initial_password = %q, want >= 8 chars", created.InitialPassword)
	}
	if _, _, _, err := env.service.Login(context.Background(), "generated@example.com", created.InitialPassword); err != nil {
		t.Fatalf("Login with generated password error = %v", err)
	}
}

func TestAdminHTTPPatchDisable(t *testing.T) {
	env := newAdminTestEnv(t)
	ctx := context.Background()

	profile := env.signup("admin-patch@example.com")
	if _, _, _, err := env.service.Login(ctx, "admin-patch@example.com", "password1"); err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if _, _, err := env.service.CreateOwnedKey(ctx, profile.ID, "patch-key"); err != nil {
		t.Fatalf("CreateOwnedKey() error = %v", err)
	}

	path := "/api/admin/users/" + strconv.FormatUint(uint64(profile.ID), 10)
	recorder := env.do(http.MethodPatch, path, `{"status":"disabled"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("PATCH disable = %d %s, want 200", recorder.Code, recorder.Body.String())
	}
	payload := decodeEnvelope(t, recorder)
	var view AdminUserView
	if err := json.Unmarshal(payload.Data, &view); err != nil {
		t.Fatalf("decode patch response: %v", err)
	}
	if view.Status != models.UserStatusDisabled {
		t.Fatalf("patched status = %q, want disabled", view.Status)
	}

	var sessions int64
	if err := env.db.Model(&models.UserSession{}).Where("user_id = ?", profile.ID).Count(&sessions).Error; err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 0 {
		t.Fatalf("sessions after disable = %d, want 0", sessions)
	}
	var keys []models.AccessKey
	if err := env.db.Where("user_id = ?", profile.ID).Find(&keys).Error; err != nil {
		t.Fatalf("load keys: %v", err)
	}
	if len(keys) != 1 || keys[0].Status != "disabled" {
		t.Fatalf("keys after disable = %#v, want one disabled key", keys)
	}

	reactivate := env.do(http.MethodPatch, path, `{"status":"active"}`)
	if reactivate.Code != http.StatusOK {
		t.Fatalf("PATCH reactivate = %d %s, want 200", reactivate.Code, reactivate.Body.String())
	}
	var sessionsAfter int64
	if err := env.db.Model(&models.UserSession{}).Where("user_id = ?", profile.ID).Count(&sessionsAfter).Error; err != nil {
		t.Fatalf("count sessions after re-enable: %v", err)
	}
	if sessionsAfter != 0 {
		t.Fatalf("sessions after re-enable = %d, want 0 (no restore)", sessionsAfter)
	}

	invalid := env.do(http.MethodPatch, path, `{"status":"bogus"}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("PATCH bogus status = %d, want 400", invalid.Code)
	}
	negativeQuota := env.do(http.MethodPatch, path, `{"max_api_keys":-1}`)
	if negativeQuota.Code != http.StatusBadRequest {
		t.Fatalf("PATCH negative max_api_keys = %d, want 400", negativeQuota.Code)
	}
	quota := env.do(http.MethodPatch, path, `{"max_api_keys":7}`)
	quotaPayload := decodeEnvelope(t, quota)
	var quotaView AdminUserView
	if err := json.Unmarshal(quotaPayload.Data, &quotaView); err != nil {
		t.Fatalf("decode quota patch: %v", err)
	}
	if quotaView.MaxAPIKeys != 7 {
		t.Fatalf("max_api_keys = %d, want 7", quotaView.MaxAPIKeys)
	}

	missing := env.do(http.MethodPatch, "/api/admin/users/99999", `{"status":"disabled"}`)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("PATCH unknown user = %d, want 404", missing.Code)
	}
}

func TestAdminHTTPAdjustments(t *testing.T) {
	env := newAdminTestEnv(t)

	profile := env.signup("admin-adjust@example.com")
	base := "/api/admin/users/" + strconv.FormatUint(uint64(profile.ID), 10)

	topup := env.do(http.MethodPost, base+"/adjustments",
		`{"delta_nano_usd":5000,"reason":"topup","note":"seed"}`)
	if topup.Code != http.StatusOK {
		t.Fatalf("POST adjustments = %d %s, want 200", topup.Code, topup.Body.String())
	}
	topupPayload := decodeEnvelope(t, topup)
	var afterTopup Profile
	if err := json.Unmarshal(topupPayload.Data, &afterTopup); err != nil {
		t.Fatalf("decode adjustment response: %v", err)
	}
	if afterTopup.BalanceNanoUSD != 5000 {
		t.Fatalf("balance after topup = %d, want 5000", afterTopup.BalanceNanoUSD)
	}

	correction := env.do(http.MethodPost, base+"/adjustments",
		`{"delta_nano_usd":-2000,"reason":"correction","note":"fix"}`)
	if correction.Code != http.StatusOK {
		t.Fatalf("POST correction = %d %s, want 200", correction.Code, correction.Body.String())
	}
	correctionPayload := decodeEnvelope(t, correction)
	var afterCorrection Profile
	if err := json.Unmarshal(correctionPayload.Data, &afterCorrection); err != nil {
		t.Fatalf("decode correction response: %v", err)
	}
	if afterCorrection.BalanceNanoUSD != 3000 {
		t.Fatalf("balance after correction = %d, want 3000", afterCorrection.BalanceNanoUSD)
	}

	invalidReason := env.do(http.MethodPost, base+"/adjustments",
		`{"delta_nano_usd":1,"reason":"gift"}`)
	if invalidReason.Code != http.StatusBadRequest {
		t.Fatalf("POST invalid reason = %d, want 400", invalidReason.Code)
	}
	zeroDelta := env.do(http.MethodPost, base+"/adjustments",
		`{"delta_nano_usd":0,"reason":"topup"}`)
	if zeroDelta.Code != http.StatusBadRequest {
		t.Fatalf("POST zero delta = %d, want 400", zeroDelta.Code)
	}
	unknownUser := env.do(http.MethodPost, "/api/admin/users/99999/adjustments",
		`{"delta_nano_usd":1,"reason":"topup"}`)
	if unknownUser.Code != http.StatusNotFound {
		t.Fatalf("POST adjustments unknown user = %d, want 404", unknownUser.Code)
	}

	ledger := env.do(http.MethodGet, base+"/adjustments", "")
	if ledger.Code != http.StatusOK {
		t.Fatalf("GET adjustments = %d %s, want 200", ledger.Code, ledger.Body.String())
	}
	ledgerPayload := decodeEnvelope(t, ledger)
	var rows []adminAdjustmentView
	if err := json.Unmarshal(ledgerPayload.Data, &rows); err != nil {
		t.Fatalf("decode ledger: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("ledger rows = %d, want 2", len(rows))
	}
	if rows[0].DeltaNanoUSD != -2000 || rows[0].Reason != "correction" ||
		rows[0].AdminNote != "fix" || rows[0].CreatedByAdmin != "admin" {
		t.Fatalf("newest ledger row = %#v", rows[0])
	}
	if rows[1].DeltaNanoUSD != 5000 || rows[1].Reason != "topup" {
		t.Fatalf("oldest ledger row = %#v", rows[1])
	}
}

func TestAdminHTTPGetUserDetail(t *testing.T) {
	env := newAdminTestEnv(t)
	ctx := context.Background()

	profile := env.signup("admin-detail@example.com")
	if _, _, err := env.service.CreateOwnedKey(ctx, profile.ID, "detail-key"); err != nil {
		t.Fatalf("CreateOwnedKey() error = %v", err)
	}

	recorder := env.do(http.MethodGet, "/api/admin/users/"+strconv.FormatUint(uint64(profile.ID), 10), "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /users/:id = %d %s, want 200", recorder.Code, recorder.Body.String())
	}
	payload := decodeEnvelope(t, recorder)
	var detail struct {
		AdminUserView
		Keys []AdminUserKeyView `json:"keys"`
	}
	if err := json.Unmarshal(payload.Data, &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.ID != profile.ID || detail.KeyCount != 1 {
		t.Fatalf("detail view = %#v", detail.AdminUserView)
	}
	if len(detail.Keys) != 1 ||
		detail.Keys[0].Name != "detail-key" ||
		detail.Keys[0].KeyPrefix != "sk-gl-" ||
		len(detail.Keys[0].KeySuffix) != 4 ||
		detail.Keys[0].Status != "active" {
		t.Fatalf("detail keys = %#v", detail.Keys)
	}

	missing := env.do(http.MethodGet, "/api/admin/users/99999", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("GET unknown user = %d, want 404", missing.Code)
	}
}
