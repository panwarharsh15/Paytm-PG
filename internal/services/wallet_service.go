package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/paytm-pg/backend/internal/models"
	"github.com/paytm-pg/backend/internal/repository"
	"gorm.io/gorm"
)

// ─── DTOs ────────────────────────────────────────────────────────────────────

type AddMoneyRequest struct {
	Amount      float64 `json:"amount" binding:"required,gt=0,lte=100000"`
	Description string  `json:"description"`
	ReferenceID string  `json:"reference_id" binding:"required"`
}

type WithdrawRequest struct {
	Amount      float64 `json:"amount" binding:"required,gt=0"`
	Description string  `json:"description"`
	ReferenceID string  `json:"reference_id" binding:"required"`
}

type WalletBalanceResponse struct {
	Balance  float64 `json:"balance"`
	Currency string  `json:"currency"`
	IsLocked bool    `json:"is_locked"`
}

// ─── Wallet Service ──────────────────────────────────────────────────────────

type WalletService interface {
	GetBalance(ctx context.Context, userID uuid.UUID) (*WalletBalanceResponse, error)
	AddMoney(ctx context.Context, userID uuid.UUID, req AddMoneyRequest) (*models.Transaction, error)
	Withdraw(ctx context.Context, userID uuid.UUID, req WithdrawRequest) (*models.Transaction, error)
}

type walletService struct {
	walletRepo repository.WalletRepository
	txnRepo    repository.TransactionRepository
	db         *gorm.DB
}

func NewWalletService(
	walletRepo repository.WalletRepository,
	txnRepo repository.TransactionRepository,
	db *gorm.DB,
) WalletService {
	return &walletService{
		walletRepo: walletRepo,
		txnRepo:    txnRepo,
		db:         db,
	}
}

func (s *walletService) GetBalance(ctx context.Context, userID uuid.UUID) (*WalletBalanceResponse, error) {
	wallet, err := s.walletRepo.FindByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &WalletBalanceResponse{
		Balance:  wallet.Balance,
		Currency: wallet.Currency,
		IsLocked: wallet.IsLocked,
	}, nil
}

// AddMoney credits the wallet inside a DB transaction.
// Idempotency: if ReferenceID already exists, returns existing transaction (no double credit).
func (s *walletService) AddMoney(ctx context.Context, userID uuid.UUID, req AddMoneyRequest) (*models.Transaction, error) {
	// Idempotency check
	if existing, err := s.txnRepo.FindByReferenceID(ctx, req.ReferenceID); err == nil {
		return existing, nil
	}

	var result *models.Transaction

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		wallet, err := s.walletRepo.FindByUserID(ctx, userID)
		if err != nil {
			return err
		}

		if wallet.IsLocked {
			return ErrWalletLocked
		}

		balanceBefore := wallet.Balance
		wallet.Balance += req.Amount

		if err := s.walletRepo.UpdateBalance(ctx, tx, wallet); err != nil {
			return fmt.Errorf("failed to update wallet: %w", err)
		}

		txn := &models.Transaction{
			UserID:        userID,
			WalletID:      wallet.ID,
			Amount:        req.Amount,
			BalanceBefore: balanceBefore,
			BalanceAfter:  wallet.Balance,
			Type:          models.TxnTypeCredit,
			Status:        models.TxnStatusSuccess,
			ReferenceID:   req.ReferenceID,
			Description:   req.Description,
		}

		if err := s.txnRepo.Create(ctx, tx, txn); err != nil {
			return fmt.Errorf("failed to create transaction: %w", err)
		}

		result = txn
		return nil
	})

	return result, err
}

// Withdraw debits the wallet — checks balance first to prevent overdraft
func (s *walletService) Withdraw(ctx context.Context, userID uuid.UUID, req WithdrawRequest) (*models.Transaction, error) {
	// Idempotency check
	if existing, err := s.txnRepo.FindByReferenceID(ctx, req.ReferenceID); err == nil {
		return existing, nil
	}

	var result *models.Transaction

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		wallet, err := s.walletRepo.FindByUserID(ctx, userID)
		if err != nil {
			return err
		}

		if wallet.IsLocked {
			return ErrWalletLocked
		}

		if wallet.Balance < req.Amount {
			return ErrInsufficientBalance
		}

		balanceBefore := wallet.Balance
		wallet.Balance -= req.Amount

		if err := s.walletRepo.UpdateBalance(ctx, tx, wallet); err != nil {
			return err
		}

		txn := &models.Transaction{
			UserID:        userID,
			WalletID:      wallet.ID,
			Amount:        req.Amount,
			BalanceBefore: balanceBefore,
			BalanceAfter:  wallet.Balance,
			Type:          models.TxnTypeDebit,
			Status:        models.TxnStatusSuccess,
			ReferenceID:   req.ReferenceID,
			Description:   req.Description,
		}

		if err := s.txnRepo.Create(ctx, tx, txn); err != nil {
			return err
		}

		result = txn
		return nil
	})

	return result, err
}

// ─── Wallet-specific errors ──────────────────────────────────────────────────

var (
	ErrInsufficientBalance = errors.New("insufficient wallet balance")
	ErrWalletLocked        = errors.New("wallet is locked due to suspicious activity")
	ErrWalletNotFound      = errors.New("wallet not found")
)
