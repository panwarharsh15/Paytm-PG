package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/paytm-pg/backend/internal/config"
	"github.com/paytm-pg/backend/internal/models"
	"github.com/paytm-pg/backend/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

// ─── DTOs (Data Transfer Objects) ───────────────────────────────────────────

type RegisterRequest struct {
	FullName string `json:"full_name" binding:"required,min=2,max=100"`
	Email    string `json:"email" binding:"required,email"`
	Phone    string `json:"phone" binding:"required,min=10,max=13"`
	Password string `json:"password" binding:"required,min=8"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type UpdateProfileRequest struct {
	FullName string `json:"full_name" binding:"omitempty,min=2,max=100"`
	Phone    string `json:"phone" binding:"omitempty,min=10,max=13"`
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // seconds
}

type Claims struct {
	UserID uuid.UUID `json:"user_id"`
	Email  string    `json:"email"`
	Role   string    `json:"role"`
	jwt.RegisteredClaims
}

// ─── Auth Service ────────────────────────────────────────────────────────────

type AuthService interface {
	Register(ctx context.Context, req RegisterRequest) (*models.User, *TokenPair, error)
	Login(ctx context.Context, req LoginRequest, userAgent, ip string) (*models.User, *TokenPair, error)
	RefreshTokens(ctx context.Context, refreshToken, userAgent, ip string) (*TokenPair, error)
	Logout(ctx context.Context, refreshToken string) error
	GetProfile(ctx context.Context, userID uuid.UUID) (*models.User, error)
	UpdateProfile(ctx context.Context, userID uuid.UUID, req UpdateProfileRequest) (*models.User, error)
	ValidateAccessToken(tokenStr string) (*Claims, error)
}

type authService struct {
	userRepo   repository.UserRepository
	walletRepo repository.WalletRepository
	cfg        *config.Config
}

func NewAuthService(userRepo repository.UserRepository, walletRepo repository.WalletRepository, cfg *config.Config) AuthService {
	return &authService{
		userRepo:   userRepo,
		walletRepo: walletRepo,
		cfg:        cfg,
	}
}

func (s *authService) Register(ctx context.Context, req RegisterRequest) (*models.User, *TokenPair, error) {
	// Check duplicates
	if exists, _ := s.userRepo.ExistsByEmail(ctx, req.Email); exists {
		return nil, nil, ErrEmailAlreadyExists
	}
	if exists, _ := s.userRepo.ExistsByPhone(ctx, req.Phone); exists {
		return nil, nil, ErrPhoneAlreadyExists
	}

	// Hash password — bcrypt cost 12 is a good balance for production
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to hash password: %w", err)
	}

	user := &models.User{
		FullName: req.FullName,
		Email:    req.Email,
		Phone:    req.Phone,
		Password: string(hashed),
		Role:     "user",
		IsActive: true,
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, nil, fmt.Errorf("failed to create user: %w", err)
	}

	// Auto-create wallet on registration
	wallet := &models.Wallet{
		UserID:   user.ID,
		Balance:  0,
		Currency: "INR",
	}
	if err := s.walletRepo.Create(ctx, wallet); err != nil {
		return nil, nil, fmt.Errorf("failed to create wallet: %w", err)
	}

	tokens, err := s.generateTokenPair(user, "", "")
	if err != nil {
		return nil, nil, err
	}

	return user, tokens, nil
}

func (s *authService) Login(ctx context.Context, req LoginRequest, userAgent, ip string) (*models.User, *TokenPair, error) {
	user, err := s.userRepo.FindByEmail(ctx, req.Email)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, nil, err
	}

	if !user.IsActive {
		return nil, nil, ErrAccountDisabled
	}

	// Constant-time comparison prevents timing attacks
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		return nil, nil, ErrInvalidCredentials
	}

	tokens, err := s.generateTokenPair(user, userAgent, ip)
	if err != nil {
		return nil, nil, err
	}

	return user, tokens, nil
}

func (s *authService) RefreshTokens(ctx context.Context, refreshToken, userAgent, ip string) (*TokenPair, error) {
	// In a real system you'd look this up in DB/Redis and check if revoked
	// For now we validate the token structure
	claims, err := s.ValidateAccessToken(refreshToken)
	if err != nil {
		return nil, ErrInvalidRefreshToken
	}

	user, err := s.userRepo.FindByID(ctx, claims.UserID)
	if err != nil {
		return nil, ErrInvalidRefreshToken
	}

	return s.generateTokenPair(user, userAgent, ip)
}

func (s *authService) Logout(ctx context.Context, refreshToken string) error {
	// In production: add token to blocklist in Redis with TTL = token expiry
	// For now: no-op (stateless JWT)
	return nil
}

func (s *authService) GetProfile(ctx context.Context, userID uuid.UUID) (*models.User, error) {
	return s.userRepo.FindByID(ctx, userID)
}

func (s *authService) UpdateProfile(ctx context.Context, userID uuid.UUID, req UpdateProfileRequest) (*models.User, error) {
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	if req.FullName != "" {
		user.FullName = req.FullName
	}
	if req.Phone != "" {
		if exists, _ := s.userRepo.ExistsByPhone(ctx, req.Phone); exists {
			return nil, ErrPhoneAlreadyExists
		}
		user.Phone = req.Phone
	}

	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}

func (s *authService) ValidateAccessToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(s.cfg.JWTSecret), nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// ─── Internal helpers ────────────────────────────────────────────────────────

func (s *authService) generateTokenPair(user *models.User, userAgent, ip string) (*TokenPair, error) {
	now := time.Now()

	accessClaims := &Claims{
		UserID: user.ID,
		Email:  user.Email,
		Role:   user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(s.cfg.JWTAccessExpiry)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "paytm-pg",
			Subject:   user.ID.String(),
		},
	}

	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).
		SignedString([]byte(s.cfg.JWTSecret))
	if err != nil {
		return nil, fmt.Errorf("failed to sign access token: %w", err)
	}

	// Refresh token — opaque random token (more secure than JWT for refresh)
	refreshBytes := make([]byte, 32)
	if _, err := rand.Read(refreshBytes); err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}
	refreshToken := hex.EncodeToString(refreshBytes)

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(s.cfg.JWTAccessExpiry.Seconds()),
	}, nil
}

// ─── Service Errors ──────────────────────────────────────────────────────────

var (
	ErrEmailAlreadyExists  = errors.New("email already registered")
	ErrPhoneAlreadyExists  = errors.New("phone number already registered")
	ErrInvalidCredentials  = errors.New("invalid email or password")
	ErrAccountDisabled     = errors.New("account is disabled")
	ErrInvalidToken        = errors.New("invalid token")
	ErrInvalidRefreshToken = errors.New("invalid or expired refresh token")
)
