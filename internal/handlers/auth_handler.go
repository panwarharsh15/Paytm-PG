package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/paytm-pg/backend/internal/services"
	"github.com/paytm-pg/backend/internal/utils"
)

type AuthHandler struct {
	authService services.AuthService
}

func NewAuthHandler(authService services.AuthService) *AuthHandler {
	return &AuthHandler{authService: authService}
}

// Register godoc
// @Summary Register a new user
// @Tags auth
// @Accept json
// @Produce json
// @Param request body services.RegisterRequest true "Registration data"
// @Success 201 {object} utils.APIResponse
// @Router /auth/register [post]
func (h *AuthHandler) Register(c *gin.Context) {
	var req services.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	user, tokens, err := h.authService.Register(c.Request.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrEmailAlreadyExists):
			utils.ConflictError(c, err.Error())
		case errors.Is(err, services.ErrPhoneAlreadyExists):
			utils.ConflictError(c, err.Error())
		default:
			utils.InternalError(c, err)
		}
		return
	}

	utils.Created(c, gin.H{
		"user":   user,
		"tokens": tokens,
	})
}

// Login godoc
// @Summary Login with email and password
// @Tags auth
// @Accept json
// @Produce json
// @Param request body services.LoginRequest true "Login credentials"
// @Success 200 {object} utils.APIResponse
// @Router /auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req services.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	userAgent := c.GetHeader("User-Agent")
	ip := c.ClientIP()

	user, tokens, err := h.authService.Login(c.Request.Context(), req, userAgent, ip)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidCredentials):
			utils.UnauthorizedError(c, "Invalid email or password")
		case errors.Is(err, services.ErrAccountDisabled):
			utils.ForbiddenError(c, err.Error())
		default:
			utils.InternalError(c, err)
		}
		return
	}

	utils.OK(c, gin.H{
		"user":   user,
		"tokens": tokens,
	})
}

func (h *AuthHandler) RefreshToken(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	tokens, err := h.authService.RefreshTokens(
		c.Request.Context(),
		req.RefreshToken,
		c.GetHeader("User-Agent"),
		c.ClientIP(),
	)
	if err != nil {
		utils.UnauthorizedError(c, "Invalid or expired refresh token")
		return
	}

	utils.OK(c, tokens)
}

func (h *AuthHandler) Logout(c *gin.Context) {
	// Refresh token revocation would go here
	utils.OK(c, gin.H{"message": "logged out successfully"})
}

func (h *AuthHandler) GetProfile(c *gin.Context) {
	userID := utils.MustGetUserID(c)

	user, err := h.authService.GetProfile(c.Request.Context(), userID)
	if err != nil {
		utils.InternalError(c, err)
		return
	}

	utils.OK(c, user)
}

func (h *AuthHandler) UpdateProfile(c *gin.Context) {
	userID := utils.MustGetUserID(c)

	var req services.UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	user, err := h.authService.UpdateProfile(c.Request.Context(), userID, req)
	if err != nil {
		utils.InternalError(c, err)
		return
	}

	utils.OK(c, user)
}

// ─── Health Checks (used by Kubernetes liveness/readiness probes) ────────────

func HealthCheck(db interface{ DB() (interface{ Ping() error }, error) }) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"service": "paytm-pg",
		})
	}
}

func ReadinessCheck(db interface{ DB() (interface{ Ping() error }, error) }) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check DB connectivity
		sqlDB, err := db.DB()
		if err != nil || sqlDB.Ping() != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "not ready",
				"reason": "database unavailable",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	}
}

func MetricsHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// In production: expose Prometheus metrics here using promhttp.Handler()
		c.JSON(http.StatusOK, gin.H{"status": "metrics endpoint - integrate prometheus"})
	}
}
