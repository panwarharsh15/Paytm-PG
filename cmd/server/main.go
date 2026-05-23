package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/paytm-pg/backend/internal/config"
	"github.com/paytm-pg/backend/internal/handlers"
	"github.com/paytm-pg/backend/internal/middleware"
	"github.com/paytm-pg/backend/internal/models"
	"github.com/paytm-pg/backend/internal/repository"
	"github.com/paytm-pg/backend/internal/services"
	"github.com/paytm-pg/backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// @title Paytm-PG API
// @version 1.0
// @description Production-grade Payment Gateway API
// @host localhost:8080
// @BasePath /api/v1
func main() {
	// ─── Load Config ────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// ─── Logger ─────────────────────────────────────────────────────────────
	appLogger := logger.New(cfg.AppEnv)
	appLogger.Info("Starting Paytm-PG backend", "env", cfg.AppEnv, "port", cfg.Port)

	// ─── Database ────────────────────────────────────────────────────────────
	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=%s TimeZone=Asia/Kolkata",
		cfg.DBHost, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBPort, cfg.DBSSLMode,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		appLogger.Fatal("Failed to connect to database", "error", err)
	}

	// Connection Pool — critical for production
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	appLogger.Info("Database connected successfully")

	// ─── Auto Migrate ────────────────────────────────────────────────────────
	if err := db.AutoMigrate(
		&models.User{},
		&models.Wallet{},
		&models.Transaction{},
		&models.Payment{},
		&models.RefreshToken{},
	); err != nil {
		appLogger.Fatal("Auto migration failed", "error", err)
	}
	appLogger.Info("Database migration completed")

	// ─── Wire Dependencies (Clean Architecture) ──────────────────────────────
	// Repositories
	userRepo := repository.NewUserRepository(db)
	walletRepo := repository.NewWalletRepository(db)
	txnRepo := repository.NewTransactionRepository(db)
	paymentRepo := repository.NewPaymentRepository(db)

	// Services
	authService := services.NewAuthService(userRepo, walletRepo, cfg)
	walletService := services.NewWalletService(walletRepo, txnRepo, db)
	paymentService := services.NewPaymentService(paymentRepo, walletRepo, txnRepo, db, cfg)

	// Handlers
	authHandler := handlers.NewAuthHandler(authService)
	walletHandler := handlers.NewWalletHandler(walletService)
	paymentHandler := handlers.NewPaymentHandler(paymentService)

	// ─── Router Setup ────────────────────────────────────────────────────────
	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	// Global middleware
	router.Use(middleware.RequestID())
	router.Use(middleware.Logger(appLogger))
	router.Use(middleware.Recovery(appLogger))
	router.Use(middleware.CORS(cfg))
	router.Use(middleware.RateLimiter())

	// ─── Routes ──────────────────────────────────────────────────────────────
	// Health & readiness probes (no auth — used by k8s)
	router.GET("/health", handlers.HealthCheck(db))
	router.GET("/ready", handlers.ReadinessCheck(db))
	router.GET("/metrics", handlers.MetricsHandler())

	v1 := router.Group("/api/v1")
	{
		// Public routes
		auth := v1.Group("/auth")
		{
			auth.POST("/register", authHandler.Register)
			auth.POST("/login", authHandler.Login)
			auth.POST("/refresh", authHandler.RefreshToken)
			auth.POST("/logout", middleware.Auth(cfg), authHandler.Logout)
		}

		// Protected routes
		protected := v1.Group("")
		protected.Use(middleware.Auth(cfg))
		{
			// User
			user := protected.Group("/user")
			{
				user.GET("/profile", authHandler.GetProfile)
				user.PUT("/profile", authHandler.UpdateProfile)
			}

			// Wallet
			wallet := protected.Group("/wallet")
			{
				wallet.GET("/balance", walletHandler.GetBalance)
				wallet.POST("/add-money", walletHandler.AddMoney)
				wallet.POST("/withdraw", walletHandler.Withdraw)
			}

			// Payments
			payment := protected.Group("/payment")
			{
				payment.POST("/initiate", paymentHandler.InitiatePayment)
				payment.POST("/verify", paymentHandler.VerifyPayment)
				payment.POST("/refund", paymentHandler.InitiateRefund)
				payment.GET("/:payment_id", paymentHandler.GetPaymentStatus)
			}

			// Transactions
			txn := protected.Group("/transactions")
			{
				txn.GET("", paymentHandler.GetTransactions)
				txn.GET("/:txn_id", paymentHandler.GetTransactionByID)
			}
		}
	}

	// 404 handler
	router.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": "route not found"})
	})

	// ─── HTTP Server with Graceful Shutdown ──────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		appLogger.Info("Server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			appLogger.Fatal("Server failed to start", "error", err)
		}
	}()

	// ─── Graceful Shutdown ───────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	appLogger.Info("Shutting down server gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		appLogger.Fatal("Server forced to shutdown", "error", err)
	}

	appLogger.Info("Server exited cleanly")
}
