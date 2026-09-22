package userdash

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"

	"gorm.io/gorm"

	"gpt-load/internal/storage/models"
)

// AdminUserView is the admin-facing projection of a user account.
type AdminUserView struct {
	ID             uint   `json:"id"`
	Email          string `json:"email"`
	Status         string `json:"status"`
	BalanceNanoUSD int64  `json:"balance_nano_usd"`
	MaxAPIKeys     int    `json:"max_api_keys"`
	KeyCount       int64  `json:"key_count"`
	CreatedAtMS    int64  `json:"created_at_ms"`
	LastLoginAtMS  int64  `json:"last_login_at_ms"`
}

// AdminUserKeyView is the admin-facing projection of a user-owned access key.
type AdminUserKeyView struct {
	ID        uint   `json:"id"`
	Name      string `json:"name"`
	KeyPrefix string `json:"key_prefix"`
	KeySuffix string `json:"key_suffix"`
	Status    string `json:"status"`
}

// AdminUserDetail is the full admin view of one user, including owned keys.
type AdminUserDetail struct {
	AdminUserView
	Keys []AdminUserKeyView `json:"keys"`
}

// AdminListQuery carries the validated list-users query parameters.
type AdminListQuery struct {
	Search   string
	Page     int
	PageSize int
}

// AdminListResult is one page of the user list.
type AdminListResult struct {
	Items []AdminUserView `json:"items"`
	Total int64           `json:"total"`
}

// AdminCreateInput is the validated admin create-user payload.
type AdminCreateInput struct {
	Email          string
	Password       string // empty means generate
	MaxAPIKeys     int
	BalanceNanoUSD int64
}

// AdminPatchInput is the validated admin patch payload.
type AdminPatchInput struct {
	Status     *string
	MaxAPIKeys *int
}

// AdminUserPage enforces sane pagination bounds shared by list endpoints.
type AdminUserPage struct {
	Page     int
	PageSize int
}

func (p AdminUserPage) offset() int { return (p.Page - 1) * p.PageSize }

func (q AdminListQuery) offset() int {
	return AdminUserPage{Page: q.Page, PageSize: q.PageSize}.offset()
}

const (
	adminDefaultPageSize = 20
	adminMaxPageSize     = 100
)

// ParseAdminListQuery validates search/page/page_size query parameters.
func ParseAdminListQuery(search string, rawPage, rawPageSize string) (AdminListQuery, error) {
	query := AdminListQuery{
		Search:   search,
		Page:     1,
		PageSize: adminDefaultPageSize,
	}
	if rawPage != "" {
		if _, err := fmt.Sscanf(rawPage, "%d", &query.Page); err != nil || query.Page < 1 {
			return AdminListQuery{}, ErrValidation
		}
	}
	if rawPageSize != "" {
		if _, err := fmt.Sscanf(rawPageSize, "%d", &query.PageSize); err != nil ||
			query.PageSize < 1 || query.PageSize > adminMaxPageSize {
			return AdminListQuery{}, ErrValidation
		}
	}
	return query, nil
}

func adminViewFromRows(user models.User, keyCount int64, lastLogin int64) AdminUserView {
	view := AdminUserView{
		ID:             user.ID,
		Email:          user.Email,
		Status:         user.Status,
		BalanceNanoUSD: user.BalanceNanoUSD,
		MaxAPIKeys:     user.MaxAPIKeys,
		KeyCount:       keyCount,
		CreatedAtMS:    user.CreatedAtMS,
		LastLoginAtMS:  lastLogin,
	}
	return view
}

// ListAdminUsers returns one page of users with key counts and last login,
// newest first. Empty search matches every user.
func (s *Service) ListAdminUsers(ctx context.Context, query AdminListQuery) (AdminListResult, error) {
	base := s.db.WithContext(ctx).Model(&models.User{})
	if query.Search != "" {
		pattern := "%" + likeEscape(query.Search) + "%"
		base = base.Where("email LIKE ? ESCAPE '\\'", pattern)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return AdminListResult{}, err
	}
	var rows []models.User
	if err := base.
		Order("id DESC").
		Limit(query.PageSize).
		Offset(query.offset()).
		Find(&rows).Error; err != nil {
		return AdminListResult{}, err
	}
	items := make([]AdminUserView, 0, len(rows))
	for _, row := range rows {
		view, err := s.adminView(ctx, row)
		if err != nil {
			return AdminListResult{}, err
		}
		items = append(items, view)
	}
	return AdminListResult{Items: items, Total: total}, nil
}

// adminView decorates a user row with key count and last login time.
func (s *Service) adminView(ctx context.Context, user models.User) (AdminUserView, error) {
	var keyCount int64
	if err := s.db.WithContext(ctx).Model(&models.AccessKey{}).
		Where("user_id = ?", user.ID).Count(&keyCount).Error; err != nil {
		return AdminUserView{}, err
	}
	var lastLogin struct{ Max int64 }
	if err := s.db.WithContext(ctx).Model(&models.UserSession{}).
		Select("COALESCE(MAX(created_at_ms), 0) AS max").
		Where("user_id = ?", user.ID).Scan(&lastLogin).Error; err != nil {
		return AdminUserView{}, err
	}
	return adminViewFromRows(user, keyCount, lastLogin.Max), nil
}

// CreateAdminUser creates an account with an admin-set initial balance and
// quota. An empty password gets a generated one returned exactly once.
func (s *Service) CreateAdminUser(ctx context.Context, input AdminCreateInput) (Profile, string, error) {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return Profile{}, "", err
	}
	password := input.Password
	returnedPassword := ""
	if password == "" {
		generated, genErr := generatePassword(rand.Reader)
		if genErr != nil {
			return Profile{}, "", genErr
		}
		password = generated
		returnedPassword = password
	}
	if err := validatePassword(password); err != nil {
		return Profile{}, "", err
	}
	if input.MaxAPIKeys < 0 {
		return Profile{}, "", ErrValidation
	}
	profile, err := s.Signup(ctx, email, password)
	if err != nil {
		return Profile{}, "", err
	}
	updates := map[string]any{}
	if input.MaxAPIKeys > 0 {
		updates["max_api_keys"] = input.MaxAPIKeys
	}
	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(&models.User{}).
			Where("id = ?", profile.ID).Updates(updates).Error; err != nil {
			return Profile{}, "", err
		}
		if input.MaxAPIKeys > 0 {
			profile.MaxAPIKeys = input.MaxAPIKeys
		}
	}
	if input.BalanceNanoUSD != 0 {
		// Initial balance must flow through the ledger so the invariant
		// asserted by ReconcileBalance (balance == SUM(adjustments) -
		// SUM(deducted)) holds from creation onward.
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			row := models.UserBalanceAdjustment{
				UserID: profile.ID, DeltaNanoUSD: input.BalanceNanoUSD,
				Reason: "topup", AdminNote: "initial", CreatedByAdmin: "admin",
				CreatedAtMS: s.now().UnixMilli(),
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			return tx.Model(&models.User{}).Where("id = ?", profile.ID).
				Update("balance_nano_usd", input.BalanceNanoUSD).Error
		})
		if err != nil {
			return Profile{}, "", err
		}
		profile.BalanceNanoUSD = input.BalanceNanoUSD
	}
	return profile, returnedPassword, nil
}

// PatchAdminUser applies a partial update. Disabling runs the security cascade
// in ONE transaction: user status, session revocation, and owned-key disabling.
// Re-enabling does NOT restore sessions or keys.
func (s *Service) PatchAdminUser(ctx context.Context, userID uint, input AdminPatchInput) (AdminUserView, error) {
	if input.Status != nil && *input.Status != models.UserStatusActive && *input.Status != models.UserStatusDisabled {
		return AdminUserView{}, fmt.Errorf("%w: invalid status", ErrValidation)
	}
	if input.MaxAPIKeys != nil && *input.MaxAPIKeys < 0 {
		return AdminUserView{}, fmt.Errorf("%w: max_api_keys must be >= 0", ErrValidation)
	}

	var view AdminUserView
	cascadeDisabledKeys := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.First(&user, userID).Error; err != nil {
			return err
		}
		wasActive := user.Status != models.UserStatusDisabled
		updates := map[string]any{}
		if input.Status != nil && *input.Status != user.Status {
			updates["status"] = *input.Status
		}
		if input.MaxAPIKeys != nil && *input.MaxAPIKeys != user.MaxAPIKeys {
			updates["max_api_keys"] = *input.MaxAPIKeys
		}
		if len(updates) > 0 {
			if err := tx.Model(&models.User{}).Where("id = ?", userID).
				Updates(updates).Error; err != nil {
				return err
			}
			user.Status = updatesOrDefault(updates, "status", user.Status)
			user.MaxAPIKeys = anyIntOrDefault(updates["max_api_keys"], user.MaxAPIKeys)
		}
		if disabling(input.Status) && wasActive {
			if err := tx.Where("user_id = ?", userID).
				Delete(&models.UserSession{}).Error; err != nil {
				return err
			}
			if err := tx.Model(&models.AccessKey{}).
				Where("user_id = ? AND status = ?", userID, "active").
				Update("status", "disabled").Error; err != nil {
				return err
			}
			cascadeDisabledKeys = true
		}
		decorated, err := adminViewInTx(tx, user)
		if err != nil {
			return err
		}
		view = decorated
		return nil
	})
	if err != nil {
		return AdminUserView{}, err
	}
	if cascadeDisabledKeys {
		if err := s.refreshRuntime(ctx); err != nil {
			return AdminUserView{}, err
		}
	}
	return view, nil
}

func disabling(requested *string) bool {
	return requested != nil && *requested == models.UserStatusDisabled
}

func updatesOrDefault(m map[string]any, key string, fallback string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return fallback
}

func anyIntOrDefault(v any, fallback int) int {
	if i, ok := v.(int); ok {
		return i
	}
	return fallback
}

func adminViewInTx(tx *gorm.DB, user models.User) (AdminUserView, error) {
	var keyCount int64
	if err := tx.Model(&models.AccessKey{}).
		Where("user_id = ?", user.ID).Count(&keyCount).Error; err != nil {
		return AdminUserView{}, err
	}
	var lastLogin struct{ Max int64 }
	if err := tx.Model(&models.UserSession{}).
		Select("COALESCE(MAX(created_at_ms), 0) AS max").
		Where("user_id = ?", user.ID).Scan(&lastLogin).Error; err != nil {
		return AdminUserView{}, err
	}
	return adminViewFromRows(user, keyCount, lastLogin.Max), nil
}

// GetAdminUser returns the full admin detail view, including owned keys.
func (s *Service) GetAdminUser(ctx context.Context, userID uint) (AdminUserDetail, error) {
	var user models.User
	if err := s.db.WithContext(ctx).First(&user, userID).Error; err != nil {
		return AdminUserDetail{}, err
	}
	decorated, err := s.adminView(ctx, user)
	if err != nil {
		return AdminUserDetail{}, err
	}
	var rows []models.AccessKey
	if err := s.db.WithContext(ctx).
		Select("id", "name", "key_prefix", "key_suffix", "status").
		Where("user_id = ?", userID).
		Order("id ASC").
		Find(&rows).Error; err != nil {
		return AdminUserDetail{}, err
	}
	keys := make([]AdminUserKeyView, 0, len(rows))
	for _, row := range rows {
		prefix := ownedKeyPrefix
		if row.KeyPrefix != nil {
			prefix = *row.KeyPrefix
		}
		keys = append(keys, AdminUserKeyView{
			ID: row.ID, Name: row.Name, KeyPrefix: prefix, KeySuffix: row.KeySuffix,
			Status: row.Status,
		})
	}
	return AdminUserDetail{AdminUserView: decorated, Keys: keys}, nil
}

// AdminAdjustmentInput is the validated adjustment payload.
type AdminAdjustmentInput struct {
	DeltaNanoUSD int64
	Reason       string
	Note         string
}

// adminAdjustmentView is the ledger projection returned to admins.
type adminAdjustmentView struct {
	ID             uint   `json:"id"`
	DeltaNanoUSD   int64  `json:"delta_nano_usd"`
	Reason         string `json:"reason"`
	AdminNote      string `json:"admin_note"`
	CreatedByAdmin string `json:"created_by_admin"`
	CreatedAtMS    int64  `json:"created_at_ms"`
}

var validAdjustmentReasons = map[string]struct{}{
	"topup":      {},
	"correction": {},
}

// CreateAdminAdjustment records a manual balance change; reason must be
// topup|correction.
func (s *Service) CreateAdminAdjustment(ctx context.Context, userID uint, input AdminAdjustmentInput) (Profile, error) {
	if _, ok := validAdjustmentReasons[input.Reason]; !ok {
		return Profile{}, fmt.Errorf("%w: reason must be topup or correction", ErrValidation)
	}
	return s.AdjustBalance(ctx, userID, input.DeltaNanoUSD, input.Reason, input.Note, "admin")
}

// ListAdminAdjustments returns the user's ledger, newest first.
func (s *Service) ListAdminAdjustments(ctx context.Context, userID uint, page AdminUserPage) ([]adminAdjustmentView, error) {
	if page.PageSize <= 0 {
		page.PageSize = adminDefaultPageSize
	}
	if page.PageSize > adminMaxPageSize {
		page.PageSize = adminMaxPageSize
	}
	if page.Page < 1 {
		page.Page = 1
	}
	var userExists int64
	if err := s.db.WithContext(ctx).Model(&models.User{}).
		Where("id = ?", userID).Count(&userExists).Error; err != nil {
		return nil, err
	}
	if userExists == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	rows, err := s.ListAdjustments(ctx, userID, page.PageSize, page.offset())
	if err != nil {
		return nil, err
	}
	views := make([]adminAdjustmentView, 0, len(rows))
	for _, row := range rows {
		views = append(views, adminAdjustmentView{
			ID: row.ID, DeltaNanoUSD: row.DeltaNanoUSD, Reason: row.Reason,
			AdminNote: row.AdminNote, CreatedByAdmin: row.CreatedByAdmin,
			CreatedAtMS: row.CreatedAtMS,
		})
	}
	return views, nil
}

func likeEscape(value string) string {
	escaped := make([]byte, 0, len(value))
	for i := 0; i < len(value); i++ {
		switch c := value[i]; c {
		case '\\', '%', '_':
			escaped = append(escaped, '\\', c)
		default:
			escaped = append(escaped, c)
		}
	}
	return string(escaped)
}

// generatePassword produces a URL-safe password that passes validatePassword
// by construction: the first two characters are pinned to a digit and a
// letter, the rest is 20 base64 chars from 16 random bytes (~120 bits).
func generatePassword(random io.Reader) (string, error) {
	buf := make([]byte, 16)
	if _, err := io.ReadFull(random, buf); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	encoded := []byte(base64.RawURLEncoding.EncodeToString(buf))
	encoded[0] = '9'
	encoded[1] = 'x'
	return string(encoded), nil
}
