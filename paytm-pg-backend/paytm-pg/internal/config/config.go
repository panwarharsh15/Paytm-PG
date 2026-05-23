package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration.
// Values are loaded from environment variables — never hardcoded secrets.
type Config struct {
	// App
	AppEnv  string
	AppName string
	Port    string

	// Database
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string

	// JWT
	JWTSecret            string
	JWTAccessExpiry      time.Duration
	JWTRefreshExpiry     time.Duration

	// CORS
	AllowedOrigins []string

	// Rate Limiting
	RateLimitRPS   int
	RateLimitBurst int

	// Payment Gateway (mock razorpay-like)
	PGKeyID     string
	PGKeySecret string
	PGWebhookSecret string

	// Redis (for rate limiting & caching)
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int
}

// Load reads config from environment variables with sane defaults.
// In production these come from Kubernetes Secrets / ConfigMaps.
func Load() (*Config, error) {
	cfg := &Config{
		AppEnv:  getEnv("APP_ENV", "development"),
		AppName: getEnv("APP_NAME", "paytm-pg"),
		Port:    getEnv("PORT", "8080"),

		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "5432"),
		DBUser:     getEnv("DB_USER", "postgres"),
		DBPassword: getEnv("DB_PASSWORD", "postgres"),
		DBName:     getEnv("DB_NAME", "paytm_pg"),
		DBSSLMode:  getEnv("DB_SSL_MODE", "disable"),

		JWTSecret:        mustGetEnv("JWT_SECRET"),
		JWTAccessExpiry:  getDurationEnv("JWT_ACCESS_EXPIRY_MINUTES", 15) * time.Minute,
		JWTRefreshExpiry: getDurationEnv("JWT_REFRESH_EXPIRY_DAYS", 7) * 24 * time.Hour,

		AllowedOrigins: []string{
			getEnv("ALLOWED_ORIGIN_1", "http://localhost:3000"),
			getEnv("ALLOWED_ORIGIN_2", "https://pay.yourdomain.com"),
		},

		RateLimitRPS:   getIntEnv("RATE_LIMIT_RPS", 100),
		RateLimitBurst: getIntEnv("RATE_LIMIT_BURST", 200),

		PGKeyID:         getEnv("PG_KEY_ID", "rzp_test_key"),
		PGKeySecret:     getEnv("PG_KEY_SECRET", "rzp_test_secret"),
		PGWebhookSecret: getEnv("PG_WEBHOOK_SECRET", "webhook_secret"),

		RedisHost:     getEnv("REDIS_HOST", "localhost"),
		RedisPort:     getEnv("REDIS_PORT", "6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getIntEnv("REDIS_DB", 0),
	}

	return cfg, cfg.validate()
}

func (c *Config) validate() error {
	if c.JWTSecret == "" {
		return fmt.Errorf("JWT_SECRET is required")
	}
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("JWT_SECRET must be at least 32 characters for security")
	}
	return nil
}

func (c *Config) IsProduction() bool {
	return c.AppEnv == "production"
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// mustGetEnv panics if a required env var is not set — fail fast on startup
func mustGetEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		// In k8s, this surfaces immediately in pod logs before it ever serves traffic
		panic(fmt.Sprintf("required environment variable %q is not set", key))
	}
	return v
}

func getIntEnv(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return time.Duration(i)
		}
	}
	return fallback
}
