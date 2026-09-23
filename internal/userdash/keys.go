package userdash

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/platform/response"
	"gpt-load/internal/storage/models"
)

const ownedKeyPrefix = "sk-gl-"

var ErrQuotaReached = errors.New("userdash: api key quota reached")

var ownedKeyPrefixValue = ownedKeyPrefix

// OwnedKeyView is the client-area projection of a user-owned access key.
type OwnedKeyView struct {
	ID                 uint     `json:"id"`
	Name               string   `json:"name"`
	KeyPrefix          string   `json:"key_prefix"`
	KeySuffix          string   `json:"key_suffix"`
	Status             string   `json:"status"`
	CreatedAtMS        int64    `json:"created_at_ms"`
	LastRequestAtMS    *int64   `json:"last_request_at_ms"`
	RPMLimit           int64    `json:"rpm_limit"`
	MaxConcurrent      int64    `json:"max_concurrent"`
	CreditLimitNanoUSD int64    `json:"credit_limit_nano_usd"`
	Models             []string `json:"models"`
	UsageTotalNanoUSD  int64    `json:"usage_total_nano_usd"`
}

type keysRequest struct {
	Name string `json:"name"`
}

// CreateOwnedKey mints an sk-gl- key for the user inside one transaction that
// also enforces the max_api_keys quota (counting every key, disabled included).
// The plaintext credential is returned exactly once and never persisted.
func (s *Service) CreateOwnedKey(ctx context.Context, userID uint, name string) (OwnedKeyView, string, error) {
	if s.encryptor == nil {
		return OwnedKeyView{}, "", errors.New("userdash: key encryptor is not configured")
	}
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || len(trimmed) > 255 {
		return OwnedKeyView{}, "", ErrValidation
	}
	randomBytes := make([]byte, 16)
	if _, err := io.ReadFull(s.random, randomBytes); err != nil {
		return OwnedKeyView{}, "", fmt.Errorf("generate access key: %w", err)
	}
	plaintext := ownedKeyPrefix + hex.EncodeToString(randomBytes)
	ciphertext, err := s.encryptor.Encrypt(plaintext)
	if err != nil {
		return OwnedKeyView{}, "", fmt.Errorf("encrypt access key: %w", err)
	}
	var view OwnedKeyView
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.Select("id", "max_api_keys").First(&user, userID).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&models.AccessKey{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
			return err
		}
		if count >= int64(user.MaxAPIKeys) {
			return ErrQuotaReached
		}
		row := models.AccessKey{
			Name:      trimmed,
			KeyValue:  ciphertext,
			KeyHash:   s.encryptor.Hash(plaintext),
			KeyPrefix: &ownedKeyPrefixValue,
			KeySuffix: plaintext[len(plaintext)-4:],
			Status:    "active",
			UserID:    &userID,
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		view = OwnedKeyView{
			ID: row.ID, Name: row.Name, KeyPrefix: ownedKeyPrefix, KeySuffix: row.KeySuffix,
			Status: row.Status, CreatedAtMS: row.CreatedAtMS,
		}
		return nil
	})
	if err != nil {
		return OwnedKeyView{}, "", err
	}
	if err := s.refreshRuntime(ctx); err != nil {
		return OwnedKeyView{}, "", err
	}
	return view, plaintext, nil
}

// ListOwnedKeys returns every key owned by the user, disabled included, each
// decorated with its lifetime usage and last request time. Both aggregates are
// fetched in one batched query each (no N+1) and keys with no usage keep their
// zero/nil defaults.
func (s *Service) ListOwnedKeys(ctx context.Context, userID uint) ([]OwnedKeyView, error) {
	var rows []models.AccessKey
	if err := s.db.WithContext(ctx).
		Select("id", "name", "key_prefix", "key_suffix", "status", "created_at_ms", "rpm_limit", "max_concurrent", "filters").
		Where("user_id = ?", userID).
		Order("id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	keyIDs := make([]uint, 0, len(rows))
	for _, row := range rows {
		keyIDs = append(keyIDs, row.ID)
	}
	usage, err := s.ownedKeyUsage(ctx, keyIDs)
	if err != nil {
		return nil, err
	}
	lastRequests, err := s.ownedKeyLastRequests(ctx, keyIDs)
	if err != nil {
		return nil, err
	}
	credits, err := s.ownedKeyCreditLimits(ctx, keyIDs)
	if err != nil {
		return nil, err
	}
	views := make([]OwnedKeyView, 0, len(rows))
	for _, row := range rows {
		prefix := ownedKeyPrefix
		if row.KeyPrefix != nil {
			prefix = *row.KeyPrefix
		}
		view := OwnedKeyView{
			ID: row.ID, Name: row.Name, KeyPrefix: prefix, KeySuffix: row.KeySuffix,
			Status: row.Status, CreatedAtMS: row.CreatedAtMS,
			RPMLimit: row.RPMLimit, MaxConcurrent: row.MaxConcurrent,
			Models: decodeOwnedKeyModels(row.Filters),
		}
		if aggregate, ok := usage[row.ID]; ok {
			view.UsageTotalNanoUSD = aggregate.UsageTotalNanoUSD
		}
		if credit, ok := credits[row.ID]; ok {
			view.CreditLimitNanoUSD = credit
		}
		if last, ok := lastRequests[row.ID]; ok {
			lastRequest := last
			view.LastRequestAtMS = &lastRequest
		}
		views = append(views, view)
	}
	return views, nil
}

// ownedKeyUsageAggregate is one row of the batched lifetime usage query.
type ownedKeyUsageAggregate struct {
	AccessKeyID       uint  `gorm:"column:access_key_id"`
	UsageTotalNanoUSD int64 `gorm:"column:usage_total_nano_usd"`
}

// ownedKeyUsage sums lifetime usage_stats cost for the given keys.
func (s *Service) ownedKeyUsage(ctx context.Context, keyIDs []uint) (map[uint]ownedKeyUsageAggregate, error) {
	aggregates := make(map[uint]ownedKeyUsageAggregate, len(keyIDs))
	if len(keyIDs) == 0 {
		return aggregates, nil
	}
	rows := make([]ownedKeyUsageAggregate, 0, len(keyIDs))
	if err := s.db.WithContext(ctx).Model(&models.UsageStat{}).
		Select("access_key_id, COALESCE(SUM(estimated_cost_nano_usd), 0) AS usage_total_nano_usd").
		Where("access_key_id IN ?", keyIDs).
		Group("access_key_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		aggregates[row.AccessKeyID] = row
	}
	return aggregates, nil
}

// ownedKeyCreditLimits returns each key's `total` cost limit in nano-USD. Keys
// without a total rule are absent (rendered as 0 = unlimited).
func (s *Service) ownedKeyCreditLimits(ctx context.Context, keyIDs []uint) (map[uint]int64, error) {
	credits := make(map[uint]int64, len(keyIDs))
	if len(keyIDs) == 0 {
		return credits, nil
	}
	var rows []struct {
		AccessKeyID  uint  `gorm:"column:access_key_id"`
		LimitNanoUSD int64 `gorm:"column:limit_nano_usd"`
	}
	if err := s.db.WithContext(ctx).Model(&models.AccessKeyCostLimitRule{}).
		Select("access_key_id, limit_nano_usd").
		Where("access_key_id IN ? AND kind = ?", keyIDs, models.AccessKeyCostLimitKindTotal).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		credits[row.AccessKeyID] = row.LimitNanoUSD
	}
	return credits, nil
}

// storedOwnedKeyFilters mirrors control's persisted filters JSON so a
// models-only update preserves groups/protocols/allowed_cidrs. Kept local to
// avoid importing the control package from userdash.
type storedOwnedKeyFilters struct {
	Groups       []uint   `json:"groups"`
	Protocols    []string `json:"protocols"`
	Models       []string `json:"models"`
	AllowedCIDRs []string `json:"allowed_cidrs,omitempty"`
}

func decodeOwnedKeyModels(raw models.JSON) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var decoded storedOwnedKeyFilters
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return []string{}
	}
	if decoded.Models == nil {
		return []string{}
	}
	return decoded.Models
}

// normalizeOwnedKeyModels trims, rejects empties, and de-duplicates models,
// mirroring control's normalizeAccessKeyFilters model handling.
func normalizeOwnedKeyModels(models []string) ([]string, error) {
	result := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, value := range models {
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			return nil, ErrValidation
		}
		if _, duplicate := seen[normalized]; duplicate {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result, nil
}

// encodeOwnedKeyFilters replaces only the models field of the stored filters,
// preserving every other field control may have written.
func encodeOwnedKeyFilters(existing models.JSON, allowedModels []string) (models.JSON, error) {
	var decoded storedOwnedKeyFilters
	if len(existing) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(existing))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&decoded); err != nil {
			return nil, fmt.Errorf("decode access key filters: %w", err)
		}
	}
	decoded.Models = allowedModels
	if decoded.Groups == nil {
		decoded.Groups = []uint{}
	}
	if decoded.Protocols == nil {
		decoded.Protocols = []string{}
	}
	if decoded.AllowedCIDRs == nil {
		decoded.AllowedCIDRs = []string{}
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return nil, fmt.Errorf("encode access key filters: %w", err)
	}
	return models.JSON(encoded), nil
}

// OwnedKeyLimitsInput is the set-aware input for SetOwnedKeyLimits: a nil
// pointer leaves the corresponding field untouched.
type OwnedKeyLimitsInput struct {
	RPMLimit           *int64
	MaxConcurrent      *int64
	CreditLimitNanoUSD *int64
	Models             *[]string
}

// SetOwnedKeyLimits updates the enforcement inputs of one user-owned key. An
// unowned or unknown key id is indistinguishable (gorm.ErrRecordNotFound).
func (s *Service) SetOwnedKeyLimits(ctx context.Context, userID, keyID uint, input OwnedKeyLimitsInput) error {
	if input.RPMLimit != nil && *input.RPMLimit < 0 {
		return ErrValidation
	}
	if input.MaxConcurrent != nil && *input.MaxConcurrent < 0 {
		return ErrValidation
	}
	if input.CreditLimitNanoUSD != nil && *input.CreditLimitNanoUSD < 0 {
		return ErrValidation
	}
	var normalizedModels []string
	if input.Models != nil {
		models, err := normalizeOwnedKeyModels(*input.Models)
		if err != nil {
			return err
		}
		normalizedModels = models
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row models.AccessKey
		if err := tx.Select("id", "filters").
			Where("id = ? AND user_id = ?", keyID, userID).
			Take(&row).Error; err != nil {
			return err
		}
		updates := make(map[string]any, 3)
		if input.RPMLimit != nil {
			updates["rpm_limit"] = *input.RPMLimit
		}
		if input.MaxConcurrent != nil {
			updates["max_concurrent"] = *input.MaxConcurrent
		}
		if input.Models != nil {
			encoded, err := encodeOwnedKeyFilters(row.Filters, normalizedModels)
			if err != nil {
				return err
			}
			updates["filters"] = encoded
		}
		if len(updates) > 0 {
			if err := tx.Model(&models.AccessKey{}).
				Where("id = ? AND user_id = ?", keyID, userID).
				Updates(updates).Error; err != nil {
				return err
			}
		}
		if input.CreditLimitNanoUSD != nil {
			if err := setOwnedKeyCreditLimit(tx, keyID, *input.CreditLimitNanoUSD); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return s.refreshRuntime(ctx)
}

// setOwnedKeyCreditLimit replaces the key's single `total` cost rule. A limit
// of 0 deletes the rule (the model CHECK forbids limit_nano_usd <= 0); a
// positive limit upserts a new revision.
func setOwnedKeyCreditLimit(tx *gorm.DB, keyID uint, limitNanoUSD int64) error {
	var existing models.AccessKeyCostLimitRule
	err := tx.Where("access_key_id = ? AND kind = ?", keyID, models.AccessKeyCostLimitKindTotal).
		Take(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	found := err == nil
	if limitNanoUSD == 0 {
		if !found {
			return nil
		}
		return tx.Delete(&models.AccessKeyCostLimitRule{}, existing.ID).Error
	}
	if found {
		if existing.RuleRevision == ^uint64(0) {
			return fmt.Errorf("advance access key cost limit rule %d revision", existing.ID)
		}
		nextRevision := existing.RuleRevision + 1
		if err := tx.Model(&models.AccessKeyCostLimitRule{}).
			Where("id = ? AND access_key_id = ?", existing.ID, keyID).
			Updates(map[string]any{
				"limit_nano_usd": limitNanoUSD,
				"rule_revision":  nextRevision,
			}).Error; err != nil {
			return err
		}
		// Keep the checkpoint revision in lockstep: the runtime restore path
		// rejects a state whose revision does not match its rule. Accumulated
		// usage is preserved (a total-limit change does not reset spend).
		return tx.Model(&models.AccessKeyCostLimitState{}).
			Where("rule_id = ? AND rule_revision = ?", existing.ID, existing.RuleRevision).
			Update("rule_revision", nextRevision).Error
	}
	rule := models.AccessKeyCostLimitRule{
		AccessKeyID: keyID, Kind: models.AccessKeyCostLimitKindTotal,
		LimitNanoUSD: limitNanoUSD, PeriodSeconds: 0, RuleRevision: 1,
	}
	if err := tx.Create(&rule).Error; err != nil {
		return err
	}
	state := models.AccessKeyCostLimitState{
		RuleID: rule.ID, RuleRevision: rule.RuleRevision, SnapshotVersion: 1,
	}
	return tx.Create(&state).Error
}

// ownedKeyLastRequests returns the newest request time per key. Keys with no
// request log are absent from the map (rendered as null).
func (s *Service) ownedKeyLastRequests(ctx context.Context, keyIDs []uint) (map[uint]int64, error) {
	lastRequests := make(map[uint]int64, len(keyIDs))
	if len(keyIDs) == 0 {
		return lastRequests, nil
	}
	var rows []struct {
		AccessKeyID uint  `gorm:"column:access_key_id"`
		LastRequest int64 `gorm:"column:last_request"`
	}
	if err := s.db.WithContext(ctx).Model(&models.RequestLog{}).
		Select("access_key_id, MAX(completed_at_ms) AS last_request").
		Where("access_key_id IN ?", keyIDs).
		Group("access_key_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		lastRequests[row.AccessKeyID] = row.LastRequest
	}
	return lastRequests, nil
}

// SetOwnedKeyStatus flips the key between active and disabled without deleting
// the row; logs and usage aggregates keep their referential target. An unowned
// or unknown key id is indistinguishable (ErrRecordNotFound -> 404).
func (s *Service) SetOwnedKeyStatus(ctx context.Context, userID, keyID uint, status string) error {
	if status != "active" && status != "disabled" {
		return ErrValidation
	}
	result := s.db.WithContext(ctx).Model(&models.AccessKey{}).
		Where("id = ? AND user_id = ?", keyID, userID).
		Update("status", status)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return s.refreshRuntime(ctx)
}

func (m *HTTPModule) handleListKeys(c *gin.Context) {
	profile := c.MustGet(ContextProfileKey).(Profile)
	views, err := m.service.ListOwnedKeys(c.Request.Context(), profile.ID)
	if err != nil {
		logrus.WithError(err).Error("userdash: list keys failed")
		response.Error(c, app_errors.ErrInternalServer)
		return
	}
	response.SuccessI18n(c, "common.success", views)
}

func (m *HTTPModule) handleCreateKey(c *gin.Context) {
	var request keysRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.Error(c, app_errors.ErrInvalidJSON)
		return
	}
	profile := c.MustGet(ContextProfileKey).(Profile)
	view, plaintext, err := m.service.CreateOwnedKey(c.Request.Context(), profile.ID, request.Name)
	if err != nil {
		if errors.Is(err, ErrQuotaReached) {
			response.Error(c, &app_errors.APIError{
				HTTPStatus: http.StatusConflict,
				Code:       "API_KEY_QUOTA_REACHED",
				Message:    "API key quota reached",
			})
			return
		}
		mapServiceError(c, err)
		return
	}
	response.SuccessI18n(c, "common.success", gin.H{"key": plaintext, "profile": view})
}

func (m *HTTPModule) handleDeleteKey(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(c, app_errors.ErrResourceNotFound)
		return
	}
	profile := c.MustGet(ContextProfileKey).(Profile)
	if err := m.service.SetOwnedKeyStatus(c.Request.Context(), profile.ID, uint(id), "disabled"); err != nil {
		mapServiceError(c, err)
		return
	}
	c.Status(http.StatusOK)
}

type keyLimitsRequest struct {
	RPMLimit           *int64    `json:"rpm_limit"`
	MaxConcurrent      *int64    `json:"max_concurrent"`
	CreditLimitNanoUSD *int64    `json:"credit_limit_nano_usd"`
	Models             *[]string `json:"models"`
}

type keyStatusRequest struct {
	Status string `json:"status"`
}

func (m *HTTPModule) handleKeyLimits(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(c, app_errors.ErrResourceNotFound)
		return
	}
	var request keyLimitsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.Error(c, app_errors.ErrInvalidJSON)
		return
	}
	profile := c.MustGet(ContextProfileKey).(Profile)
	err = m.service.SetOwnedKeyLimits(c.Request.Context(), profile.ID, uint(id), OwnedKeyLimitsInput{
		RPMLimit:           request.RPMLimit,
		MaxConcurrent:      request.MaxConcurrent,
		CreditLimitNanoUSD: request.CreditLimitNanoUSD,
		Models:             request.Models,
	})
	if err != nil {
		mapServiceError(c, err)
		return
	}
	response.SuccessI18n(c, "common.success", nil)
}

func (m *HTTPModule) handleKeyStatus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(c, app_errors.ErrResourceNotFound)
		return
	}
	var request keyStatusRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.Error(c, app_errors.ErrInvalidJSON)
		return
	}
	profile := c.MustGet(ContextProfileKey).(Profile)
	if err := m.service.SetOwnedKeyStatus(c.Request.Context(), profile.ID, uint(id), request.Status); err != nil {
		mapServiceError(c, err)
		return
	}
	response.SuccessI18n(c, "common.success", nil)
}

func (m *HTTPModule) handleUsage(c *gin.Context) {
	fromMS, err := strconv.ParseInt(c.Query("from_ms"), 10, 64)
	if err != nil || fromMS < 0 {
		response.Error(c, app_errors.ErrValidation)
		return
	}
	toMS, err := strconv.ParseInt(c.Query("to_ms"), 10, 64)
	if err != nil || toMS <= fromMS {
		response.Error(c, app_errors.ErrValidation)
		return
	}
	profile := c.MustGet(ContextProfileKey).(Profile)
	rows, err := m.service.QueryOwnedUsage(c.Request.Context(), profile.ID, fromMS, toMS)
	if err != nil {
		logrus.WithError(err).Error("userdash: usage query failed")
		response.Error(c, app_errors.ErrInternalServer)
		return
	}
	// granularity=day adds a per-day token series for the activity heatmap; the
	// default response stays a bare per-model array for compatibility.
	if c.Query("granularity") == "day" {
		daily, dailyErr := m.service.QueryOwnedDailyTokens(c.Request.Context(), profile.ID, fromMS, toMS)
		if dailyErr != nil {
			logrus.WithError(dailyErr).Error("userdash: daily usage query failed")
			response.Error(c, app_errors.ErrInternalServer)
			return
		}
		response.SuccessI18n(c, "common.success", gin.H{"models": rows, "daily": daily})
		return
	}
	response.SuccessI18n(c, "common.success", rows)
}
