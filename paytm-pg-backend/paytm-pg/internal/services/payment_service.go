package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/paytm-pg/backend/internal/config"
	"github.com/paytm-pg/backend/internal/models"
	"github.com/paytm-pg/backend/internal/repository"
	"gorm.io/gorm"
)

// ─── DTOs ────────────────────────────────────────────────────────────────────

type InitiatePaymentRequest struct {
	Amount      float64              `json:"amount" binding:"required,gt=0"`
	Currency    string               `json:"currency" binding:"omitempty,len=3"`
	Method      models.PaymentMethod `json:"method" binding:"required"`
	Description string               `json:"description"`
	CallbackURL string               `json:"callback_url"`
	Metadata    string               `json:"metadata"`
}

type VerifyPaymentRequest struct {
	OrderID          string `json:"order_id" binding:"required"`
	GatewayOrderID   string `json:"gateway_order_id" binding:"required"`
	GatewayPaymentID string `json:"gateway_payment_id" binding:"required"`
	GatewaySignature string `json:"gateway_signature" binding:"required"`
}

type RefundRequest struct {
	PaymentID   uuid.UUID `json:"payment_id" binding:"required"`
	Amount      float64   `json:"amount" binding:"required,gt=0"`
	Description string    `json:"description"`
}

type PaginationRequest struct {
	Page  int `form:"page,default=1" binding:"min=1"`
	Limit int `form:"limit,default=20" binding:"min=1,max=100"`
}

type PaginatedTransactions struct {
	Data       []models.Transaction `json:"data"`
	Total      int64                `json:"total"`
	Page       int                  `json:"page"`
	Limit      int                  `json:"limit"`
	TotalPages int64                `json:"total_pages"`
}

// ─── Payment Service ─────────────────────────────────────────────────────────

type PaymentService interface {
	InitiatePayment(ctx context.Context, userID uuid.UUID, req InitiatePaymentRequest) (*models.Payment, error)
	VerifyPayment(ctx context.Context, userID uuid.UUID, req VerifyPaymentRequest) (*models.Payment, error)
	InitiateRefund(ctx context.Context, userID uuid.UUID, req RefundRequest) (*models.Payment, error)
	GetPaymentStatus(ctx context.Context, userID uuid.UUID, paymentID uuid.UUID) (*models.Payment, error)
	GetTransactions(ctx context.Context, userID uuid.UUID, pagination PaginationRequest) (*PaginatedTransactions, error)
	GetTransactionByID(ctx context.Context, userID uuid.UUID, txnID uuid.UUID) (*models.Transaction, error)
}

type paymentService struct {
	paymentRepo repository.PaymentRepository
	walletRepo  repository.WalletRepository
	txnRepo     repository.TransactionRepository
	db          *gorm.DB
	cfg         *config.Config
}

func NewPaymentService(
	paymentRepo repository.PaymentRepository,
	walletRepo repository.WalletRepository,
	txnRepo repository.TransactionRepository,
	db *gorm.DB,
	cfg *config.Config,
) PaymentService {
	return &paymentService{
		paymentRepo: paymentRepo,
		walletRepo:  walletRepo,
		txnRepo:     txnRepo,
		db:          db,
		cfg:         cfg,
	}
}

// InitiatePayment creates a payment order. The client then sends user to payment page.
func (s *paymentService) InitiatePayment(ctx context.Context, userID uuid.UUID, req InitiatePaymentRequest) (*models.Payment, error) {
	if req.Currency == "" {
		req.Currency = "INR"
	}

	orderID := generateOrderID()

	// For WALLET payments — check balance upfront
	if req.Method == models.PaymentMethodWallet {
		wallet, err := s.walletRepo.FindByUserID(ctx, userID)
		if err != nil {
			return nil, err
		}
		if wallet.Balance < req.Amount {
			return nil, ErrInsufficientBalance
		}
	}

	payment := &models.Payment{
		OrderID:     orderID,
		UserID:      userID,
		Amount:      req.Amount,
		Currency:    req.Currency,
		Status:      models.PaymentStatusCreated,
		Method:      req.Method,
		Description: req.Description,
		CallbackURL: req.CallbackURL,
		Metadata:    req.Metadata,
		// In production: call Razorpay/Stripe SDK here to get GatewayOrderID
		GatewayOrderID: "gw_" + orderID,
	}

	if err := s.paymentRepo.Create(ctx, payment); err != nil {
		return nil, fmt.Errorf("failed to create payment: %w", err)
	}

	return payment, nil
}

// VerifyPayment verifies the payment signature and settles the transaction.
// This is the most critical function — wrong implementation = financial loss.
func (s *paymentService) VerifyPayment(ctx context.Context, userID uuid.UUID, req VerifyPaymentRequest) (*models.Payment, error) {
	payment, err := s.paymentRepo.FindByOrderID(ctx, req.OrderID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrPaymentNotFound
	}
	if err != nil {
		return nil, err
	}

	// Ownership check — user can only verify their own payments
	if payment.UserID != userID {
		return nil, ErrUnauthorized
	}

	// Prevent re-processing
	if payment.Status == models.PaymentStatusCaptured {
		return payment, nil // Idempotent
	}
	if payment.Status == models.PaymentStatusFailed {
		return nil, ErrPaymentAlreadyFailed
	}

	// ── SIGNATURE VERIFICATION ─────────────────────────────────────────────
	// This is how Razorpay works — DO NOT skip this in production
	// razorpay_signature = HMAC-SHA256(order_id + "|" + payment_id, key_secret)
	if !s.verifySignature(req.GatewayOrderID, req.GatewayPaymentID, req.GatewaySignature) {
		// Mark payment as failed
		payment.Status = models.PaymentStatusFailed
		payment.FailureReason = "signature verification failed"
		s.paymentRepo.Update(ctx, payment)
		return nil, ErrSignatureVerificationFailed
	}

	// ── SETTLE INSIDE DB TRANSACTION ──────────────────────────────────────
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		payment.Status = models.PaymentStatusCaptured
		payment.GatewayPaymentID = req.GatewayPaymentID
		payment.GatewaySignature = req.GatewaySignature
		payment.CapturedAt = &now

		if payment.Method == models.PaymentMethodWallet {
			wallet, err := s.walletRepo.FindByUserID(ctx, userID)
			if err != nil {
				return err
			}
			if wallet.Balance < payment.Amount {
				return ErrInsufficientBalance
			}

			balanceBefore := wallet.Balance
			wallet.Balance -= payment.Amount

			if err := s.walletRepo.UpdateBalance(ctx, tx, wallet); err != nil {
				return err
			}

			paymentID := payment.ID
			txn := &models.Transaction{
				UserID:        userID,
				WalletID:      wallet.ID,
				PaymentID:     &paymentID,
				Amount:        payment.Amount,
				BalanceBefore: balanceBefore,
				BalanceAfter:  wallet.Balance,
				Type:          models.TxnTypeDebit,
				Status:        models.TxnStatusSuccess,
				ReferenceID:   "pay_" + payment.OrderID,
				Description:   "Payment: " + payment.Description,
			}
			if err := s.txnRepo.Create(ctx, tx, txn); err != nil {
				return err
			}
		}

		return tx.Save(payment).Error
	})

	if err != nil {
		return nil, err
	}

	return payment, nil
}

// InitiateRefund reverses a captured payment
func (s *paymentService) InitiateRefund(ctx context.Context, userID uuid.UUID, req RefundRequest) (*models.Payment, error) {
	payment, err := s.paymentRepo.FindByID(ctx, req.PaymentID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrPaymentNotFound
	}
	if err != nil {
		return nil, err
	}

	if payment.UserID != userID {
		return nil, ErrUnauthorized
	}
	if payment.Status != models.PaymentStatusCaptured {
		return nil, ErrRefundNotAllowed
	}
	if req.Amount > payment.Amount {
		return nil, ErrRefundExceedsPayment
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		wallet, err := s.walletRepo.FindByUserID(ctx, userID)
		if err != nil {
			return err
		}

		balanceBefore := wallet.Balance
		wallet.Balance += req.Amount

		if err := s.walletRepo.UpdateBalance(ctx, tx, wallet); err != nil {
			return err
		}

		paymentID := payment.ID
		txn := &models.Transaction{
			UserID:        userID,
			WalletID:      wallet.ID,
			PaymentID:     &paymentID,
			Amount:        req.Amount,
			BalanceBefore: balanceBefore,
			BalanceAfter:  wallet.Balance,
			Type:          models.TxnTypeRefund,
			Status:        models.TxnStatusSuccess,
			ReferenceID:   "refund_" + payment.OrderID,
			Description:   req.Description,
		}
		if err := s.txnRepo.Create(ctx, tx, txn); err != nil {
			return err
		}

		now := time.Now()
		payment.Status = models.PaymentStatusRefunded
		payment.RefundedAt = &now
		payment.RefundAmount = req.Amount

		return tx.Save(payment).Error
	})

	return payment, err
}

func (s *paymentService) GetPaymentStatus(ctx context.Context, userID uuid.UUID, paymentID uuid.UUID) (*models.Payment, error) {
	payment, err := s.paymentRepo.FindByID(ctx, paymentID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrPaymentNotFound
	}
	if err != nil {
		return nil, err
	}
	if payment.UserID != userID {
		return nil, ErrUnauthorized
	}
	return payment, nil
}

func (s *paymentService) GetTransactions(ctx context.Context, userID uuid.UUID, pagination PaginationRequest) (*PaginatedTransactions, error) {
	offset := (pagination.Page - 1) * pagination.Limit
	txns, total, err := s.txnRepo.FindByUserID(ctx, userID, pagination.Limit, offset)
	if err != nil {
		return nil, err
	}

	totalPages := total / int64(pagination.Limit)
	if total%int64(pagination.Limit) != 0 {
		totalPages++
	}

	return &PaginatedTransactions{
		Data:       txns,
		Total:      total,
		Page:       pagination.Page,
		Limit:      pagination.Limit,
		TotalPages: totalPages,
	}, nil
}

func (s *paymentService) GetTransactionByID(ctx context.Context, userID uuid.UUID, txnID uuid.UUID) (*models.Transaction, error) {
	txn, err := s.txnRepo.FindByID(ctx, txnID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrTransactionNotFound
	}
	if err != nil {
		return nil, err
	}
	if txn.UserID != userID {
		return nil, ErrUnauthorized
	}
	return txn, nil
}

// ─── Internal helpers ────────────────────────────────────────────────────────

func (s *paymentService) verifySignature(gatewayOrderID, gatewayPaymentID, signature string) bool {
	// This is Razorpay's signature verification algorithm
	payload := gatewayOrderID + "|" + gatewayPaymentID
	mac := hmac.New(sha256.New, []byte(s.cfg.PGKeySecret))
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func generateOrderID() string {
	return fmt.Sprintf("ORD_%d_%s", time.Now().UnixMilli(), uuid.New().String()[:8])
}

// ─── Payment errors ──────────────────────────────────────────────────────────

var (
	ErrPaymentNotFound          = errors.New("payment not found")
	ErrTransactionNotFound      = errors.New("transaction not found")
	ErrUnauthorized             = errors.New("unauthorized access to this resource")
	ErrPaymentAlreadyFailed     = errors.New("payment has already failed")
	ErrSignatureVerificationFailed = errors.New("payment signature verification failed")
	ErrRefundNotAllowed         = errors.New("refund not allowed for this payment status")
	ErrRefundExceedsPayment     = errors.New("refund amount exceeds original payment")
)
