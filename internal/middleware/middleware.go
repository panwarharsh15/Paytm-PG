package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paytm-pg/backend/internal/config"
	"github.com/paytm-pg/backend/internal/services"
	"github.com/paytm-pg/backend/pkg/logger"
	"golang.org/x/time/rate"
)

// ─── Request ID ──────────────────────────────────────────────────────────────

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		reqID := c.GetHeader("X-Request-ID")
		if reqID == "" {
			reqID = uuid.New().String()
		}
		c.Set("request_id", reqID)
		c.Header("X-Request-ID", reqID)
		c.Next()
	}
}

// ─── Structured Logger ───────────────────────────────────────────────────────

func Logger(log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		statusCode := c.Writer.Status()
		reqID, _ := c.Get("request_id")

		log.Info("HTTP request",
			"method", c.Request.Method,
			"path", path,
			"query", query,
			"status", statusCode,
			"latency_ms", latency.Milliseconds(),
			"ip", c.ClientIP(),
			"user_agent", c.Request.UserAgent(),
			"request_id", reqID,
		)
	}
}

// ─── Recovery (panic → 500, not crash) ──────────────────────────────────────

func Recovery(log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				log.Error("Panic recovered",
					"error", err,
					"path", c.Request.URL.Path,
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error":   "internal server error",
					"message": "something went wrong",
				})
			}
		}()
		c.Next()
	}
}

// ─── CORS ─────────────────────────────────────────────────────────────────────

func CORS(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		// Check if origin is allowed
		allowed := false
		for _, o := range cfg.AllowedOrigins {
			if o == origin || o == "*" {
				allowed = true
				break
			}
		}

		if allowed {
			c.Header("Access-Control-Allow-Origin", origin)
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization, X-Request-ID")
		c.Header("Access-Control-Expose-Headers", "X-Request-ID")
		c.Header("Access-Control-Max-Age", "86400")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// ─── JWT Auth ────────────────────────────────────────────────────────────────

// Auth validates the JWT and stores user claims in context.
// We pass the config, not the service, to avoid circular imports.
func Auth(cfg *config.Config) gin.HandlerFunc {
	// Create a minimal auth service just for token validation
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "authorization header is required",
			})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "authorization header format must be: Bearer {token}",
			})
			return
		}

		tokenStr := parts[1]

		// Inline validation to avoid circular deps
		authSvc := services.NewAuthService(nil, nil, cfg)
		claims, err := authSvc.ValidateAccessToken(tokenStr)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or expired token",
			})
			return
		}

		// Store in context — handlers read this via utils.MustGetUserID
		c.Set("user_id", claims.UserID)
		c.Set("user_email", claims.Email)
		c.Set("user_role", claims.Role)
		c.Next()
	}
}

// ─── Rate Limiter (per IP, in-memory) ────────────────────────────────────────
// For production: use Redis-based rate limiter to work across multiple pods

type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

var (
	limiters = make(map[string]*rateLimiterEntry)
	mu       sync.Mutex
)

func RateLimiter() gin.HandlerFunc {
	// Cleanup old entries every minute
	go func() {
		for {
			time.Sleep(time.Minute)
			mu.Lock()
			for ip, entry := range limiters {
				if time.Since(entry.lastSeen) > 3*time.Minute {
					delete(limiters, ip)
				}
			}
			mu.Unlock()
		}
	}()

	return func(c *gin.Context) {
		ip := c.ClientIP()

		mu.Lock()
		entry, exists := limiters[ip]
		if !exists {
			entry = &rateLimiterEntry{
				limiter: rate.NewLimiter(rate.Every(time.Second), 30), // 30 req/s burst
			}
			limiters[ip] = entry
		}
		entry.lastSeen = time.Now()
		mu.Unlock()

		if !entry.limiter.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded, slow down",
			})
			return
		}

		c.Next()
	}
}

// ─── Security Headers ────────────────────────────────────────────────────────

func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Content-Security-Policy", "default-src 'self'")
		c.Next()
	}
}
