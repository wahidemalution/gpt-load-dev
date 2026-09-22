package userdash

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"gpt-load/internal/storage/models"
)

const (
	sessionTTL     = 7 * 24 * time.Hour
	minPasswordLen = 8
	bcryptCost     = bcrypt.DefaultCost
)

var (
	ErrEmailTaken         = errors.New("userdash: email already taken")
	ErrInvalidCredentials = errors.New("userdash: invalid credentials")
	ErrUnauthenticated    = errors.New("userdash: unauthenticated")
	ErrUserDisabled       = errors.New("userdash: user disabled")
	ErrValidation         = errors.New("userdash: validation failed")
)

// dummyHash keeps login timing uniform when the email does not exist: the
// lookup miss still pays one bcrypt compare against this fixed hash.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("gpt-load-dummy-password"), bcryptCost)

type Service struct {
	db     *gorm.DB
	random io.Reader
	now    func() time.Time
}

func NewService(db *gorm.DB, random io.Reader, now func() time.Time) *Service {
	if random == nil {
		random = rand.Reader
	}
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, random: random, now: now}
}

type Profile struct {
	ID             uint   `json:"id"`
	Email          string `json:"email"`
	Status         string `json:"status"`
	BalanceNanoUSD int64  `json:"balance_nano_usd"`
	MaxAPIKeys     int    `json:"max_api_keys"`
	CreatedAtMS    int64  `json:"created_at_ms"`
}

func normalizeEmail(email string) (string, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if !strings.Contains(email, "@") || strings.ContainsAny(email, " \t\r\n") {
		return "", ErrValidation
	}
	return email, nil
}

func validatePassword(password string) error {
	if len(password) < minPasswordLen {
		return ErrValidation
	}
	var hasLetter, hasDigit bool
	for _, r := range password {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return ErrValidation
	}
	return nil
}

func profileFromRow(u *models.User) Profile {
	return Profile{
		ID:             u.ID,
		Email:          u.Email,
		Status:         u.Status,
		BalanceNanoUSD: u.BalanceNanoUSD,
		MaxAPIKeys:     u.MaxAPIKeys,
		CreatedAtMS:    u.CreatedAtMS,
	}
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Service) Signup(ctx context.Context, email, password string) (Profile, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return Profile{}, err
	}
	if err := validatePassword(password); err != nil {
		return Profile{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return Profile{}, err
	}
	user := models.User{
		Email:        email,
		PasswordHash: string(hash),
		Status:       models.UserStatusActive,
	}
	if err := s.db.WithContext(ctx).Create(&user).Error; err != nil {
		if isDuplicateKeyError(err) {
			return Profile{}, ErrEmailTaken
		}
		return Profile{}, err
	}
	return profileFromRow(&user), nil
}

func (s *Service) Login(ctx context.Context, email, password string) (string, Profile, time.Time, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return "", Profile{}, time.Time{}, err
	}
	var user models.User
	err = s.db.WithContext(ctx).Where("email = ?", email).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Constant-time discipline: pay the same bcrypt cost even when
			// the account does not exist.
			_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
			return "", Profile{}, time.Time{}, ErrInvalidCredentials
		}
		return "", Profile{}, time.Time{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return "", Profile{}, time.Time{}, ErrInvalidCredentials
	}
	if user.Status != models.UserStatusActive {
		return "", Profile{}, time.Time{}, ErrUserDisabled
	}
	token, expiresAt, err := s.mintSession(ctx, user.ID)
	if err != nil {
		return "", Profile{}, time.Time{}, err
	}
	return token, profileFromRow(&user), expiresAt, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (Profile, error) {
	tokenHash := hashToken(token)
	var session models.UserSession
	if err := s.db.WithContext(ctx).Where("token_hash = ?", tokenHash).First(&session).Error; err != nil {
		return Profile{}, ErrUnauthenticated
	}
	if subtle.ConstantTimeCompare([]byte(session.TokenHash), []byte(tokenHash)) != 1 {
		return Profile{}, ErrUnauthenticated
	}
	if session.ExpiresAtMS <= s.now().UnixMilli() {
		return Profile{}, ErrUnauthenticated
	}
	var user models.User
	if err := s.db.WithContext(ctx).First(&user, session.UserID).Error; err != nil {
		return Profile{}, ErrUnauthenticated
	}
	if user.Status != models.UserStatusActive {
		return Profile{}, ErrUserDisabled
	}
	return profileFromRow(&user), nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	return s.db.WithContext(ctx).
		Where("token_hash = ?", hashToken(token)).
		Delete(&models.UserSession{}).Error
}

func (s *Service) mintSession(ctx context.Context, userID uint) (string, time.Time, error) {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(s.random, buf); err != nil {
		return "", time.Time{}, err
	}
	token := hex.EncodeToString(buf)
	expiresAt := s.now().Add(sessionTTL)
	session := models.UserSession{
		UserID:      userID,
		TokenHash:   hashToken(token),
		ExpiresAtMS: expiresAt.UnixMilli(),
	}
	if err := s.db.WithContext(ctx).Create(&session).Error; err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

func isDuplicateKeyError(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	// ponytail: string fallback for drivers without GORM error translation,
	// mirrors app_errors.ParseDBError; drop once TranslateError is everywhere.
	normalized := strings.ToLower(err.Error())
	return strings.Contains(normalized, "unique constraint failed") ||
		strings.Contains(normalized, "duplicate key value violates unique constraint") ||
		strings.Contains(normalized, "duplicate entry")
}
