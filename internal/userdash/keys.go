package userdash

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	ID              uint   `json:"id"`
	Name            string `json:"name"`
	KeyPrefix       string `json:"key_prefix"`
	KeySuffix       string `json:"key_suffix"`
	Status          string `json:"status"`
	CreatedAtMS     int64  `json:"created_at_ms"`
	LastRequestAtMS *int64 `json:"last_request_at_ms"`
	Usage30dNanoUSD int64  `json:"usage_30d_nano_usd"`
	RequestCount30d int64  `json:"request_count_30d"`
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

// ownedKeyUsageWindow is the rolling window used for the per-key usage column.
const ownedKeyUsageWindow = 30 * 24 * time.Hour

// ListOwnedKeys returns every key owned by the user, disabled included, each
// decorated with its 30-day usage and last request time. Both aggregates are
// fetched in one batched query each (no N+1) and keys with no usage keep their
// zero/nil defaults.
func (s *Service) ListOwnedKeys(ctx context.Context, userID uint) ([]OwnedKeyView, error) {
	var rows []models.AccessKey
	if err := s.db.WithContext(ctx).
		Select("id", "name", "key_prefix", "key_suffix", "status", "created_at_ms").
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
	views := make([]OwnedKeyView, 0, len(rows))
	for _, row := range rows {
		prefix := ownedKeyPrefix
		if row.KeyPrefix != nil {
			prefix = *row.KeyPrefix
		}
		view := OwnedKeyView{
			ID: row.ID, Name: row.Name, KeyPrefix: prefix, KeySuffix: row.KeySuffix,
			Status: row.Status, CreatedAtMS: row.CreatedAtMS,
		}
		if aggregate, ok := usage[row.ID]; ok {
			view.Usage30dNanoUSD = aggregate.Usage30dNanoUSD
			view.RequestCount30d = aggregate.RequestCount30d
		}
		if last, ok := lastRequests[row.ID]; ok {
			lastRequest := last
			view.LastRequestAtMS = &lastRequest
		}
		views = append(views, view)
	}
	return views, nil
}

// ownedKeyUsageAggregate is one row of the batched 30-day usage query.
type ownedKeyUsageAggregate struct {
	AccessKeyID     uint  `gorm:"column:access_key_id"`
	Usage30dNanoUSD int64 `gorm:"column:usage_30d_nano_usd"`
	RequestCount30d int64 `gorm:"column:request_count_30d"`
}

// ownedKeyUsage sums the last 30 days of usage_stats for the given keys.
func (s *Service) ownedKeyUsage(ctx context.Context, keyIDs []uint) (map[uint]ownedKeyUsageAggregate, error) {
	aggregates := make(map[uint]ownedKeyUsageAggregate, len(keyIDs))
	if len(keyIDs) == 0 {
		return aggregates, nil
	}
	rows := make([]ownedKeyUsageAggregate, 0, len(keyIDs))
	if err := s.db.WithContext(ctx).Model(&models.UsageStat{}).
		Select(
			"access_key_id, COALESCE(SUM(estimated_cost_nano_usd), 0) AS usage_30d_nano_usd, "+
				"COALESCE(SUM(request_count), 0) AS request_count_30d",
		).
		Where("access_key_id IN ?", keyIDs).
		Where("bucket_start_ms >= ?", s.now().Add(-ownedKeyUsageWindow).UnixMilli()).
		Group("access_key_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		aggregates[row.AccessKeyID] = row
	}
	return aggregates, nil
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

// DisableOwnedKey flips the key to disabled without deleting the row; logs and
// usage aggregates keep their referential target. An unowned or unknown key id
// is indistinguishable (ErrRecordNotFound -> 404).
func (s *Service) DisableOwnedKey(ctx context.Context, userID, keyID uint) error {
	result := s.db.WithContext(ctx).Model(&models.AccessKey{}).
		Where("id = ? AND user_id = ?", keyID, userID).
		Update("status", "disabled")
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
	if err := m.service.DisableOwnedKey(c.Request.Context(), profile.ID, uint(id)); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(c, app_errors.ErrResourceNotFound)
			return
		}
		logrus.WithError(err).Error("userdash: delete key failed")
		response.Error(c, app_errors.ErrInternalServer)
		return
	}
	c.Status(http.StatusOK)
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
	response.SuccessI18n(c, "common.success", rows)
}
