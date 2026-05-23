package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/paytm-pg/backend/internal/models"
	"gorm.io/gorm"
)

// UserRepository defines the contract — services depend on this interface, NOT the concrete struct.
// This makes it dead easy to mock in tests.
type UserRepository interface {
	Create(ctx context.Context, user *models.User) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.User, error)
	FindByEmail(ctx context.Context, email string) (*models.User, error)
	FindByPhone(ctx context.Context, phone string) (*models.User, error)
	Update(ctx context.Context, user *models.User) error
	ExistsByEmail(ctx context.Context, email string) (bool, error)
	ExistsByPhone(ctx context.Context, phone string) (bool, error)
}

type userRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepository{db: db}
}

func (r *userRepository) Create(ctx context.Context, user *models.User) error {
	return r.db.WithContext(ctx).Create(user).Error
}

func (r *userRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	var user models.User
	err := r.db.WithContext(ctx).
		Preload("Wallet").
		First(&user, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &user, err
}

func (r *userRepository) FindByEmail(ctx context.Context, email string) (*models.User, error) {
	var user models.User
	err := r.db.WithContext(ctx).First(&user, "email = ?", email).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &user, err
}

func (r *userRepository) FindByPhone(ctx context.Context, phone string) (*models.User, error) {
	var user models.User
	err := r.db.WithContext(ctx).First(&user, "phone = ?", phone).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &user, err
}

func (r *userRepository) Update(ctx context.Context, user *models.User) error {
	return r.db.WithContext(ctx).Save(user).Error
}

func (r *userRepository) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.User{}).Where("email = ?", email).Count(&count).Error
	return count > 0, err
}

func (r *userRepository) ExistsByPhone(ctx context.Context, phone string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.User{}).Where("phone = ?", phone).Count(&count).Error
	return count > 0, err
}

// ─── Wallet Repository ───────────────────────────────────────────────────────

type WalletRepository interface {
	Create(ctx context.Context, wallet *models.Wallet) error
	FindByUserID(ctx context.Context, userID uuid.UUID) (*models.Wallet, error)
	FindByIDForUpdate(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*models.Wallet, error)
	UpdateBalance(ctx context.Context, tx *gorm.DB, wallet *models.Wallet) error
}

type walletRepository struct {
	db *gorm.DB
}

func NewWalletRepository(db *gorm.DB) WalletRepository {
	return &walletRepository{db: db}
}

func (r *walletRepository) Create(ctx context.Context, wallet *models.Wallet) error {
	return r.db.WithContext(ctx).Create(wallet).Error
}

func (r *walletRepository) FindByUserID(ctx context.Context, userID uuid.UUID) (*models.Wallet, error) {
	var wallet models.Wallet
	err := r.db.WithContext(ctx).First(&wallet, "user_id = ?", userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &wallet, err
}

// FindByIDForUpdate uses SELECT FOR UPDATE — prevents double-spend race conditions
func (r *walletRepository) FindByIDForUpdate(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*models.Wallet, error) {
	var wallet models.Wallet
	err := tx.WithContext(ctx).
		Set("gorm:query_option", "FOR UPDATE").
		First(&wallet, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &wallet, err
}

func (r *walletRepository) UpdateBalance(ctx context.Context, tx *gorm.DB, wallet *models.Wallet) error {
	return tx.WithContext(ctx).
		Model(wallet).
		Update("balance", wallet.Balance).Error
}

// ─── Transaction Repository ──────────────────────────────────────────────────

type TransactionRepository interface {
	Create(ctx context.Context, tx *gorm.DB, txn *models.Transaction) error
	FindByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]models.Transaction, int64, error)
	FindByID(ctx context.Context, id uuid.UUID) (*models.Transaction, error)
	FindByReferenceID(ctx context.Context, refID string) (*models.Transaction, error)
}

type transactionRepository struct {
	db *gorm.DB
}

func NewTransactionRepository(db *gorm.DB) TransactionRepository {
	return &transactionRepository{db: db}
}

func (r *transactionRepository) Create(ctx context.Context, tx *gorm.DB, txn *models.Transaction) error {
	return tx.WithContext(ctx).Create(txn).Error
}

func (r *transactionRepository) FindByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]models.Transaction, int64, error) {
	var txns []models.Transaction
	var total int64

	query := r.db.WithContext(ctx).Model(&models.Transaction{}).Where("user_id = ?", userID)
	query.Count(&total)

	err := query.
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&txns).Error

	return txns, total, err
}

func (r *transactionRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.Transaction, error) {
	var txn models.Transaction
	err := r.db.WithContext(ctx).First(&txn, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &txn, err
}

func (r *transactionRepository) FindByReferenceID(ctx context.Context, refID string) (*models.Transaction, error) {
	var txn models.Transaction
	err := r.db.WithContext(ctx).First(&txn, "reference_id = ?", refID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &txn, err
}

// ─── Payment Repository ──────────────────────────────────────────────────────

type PaymentRepository interface {
	Create(ctx context.Context, payment *models.Payment) error
	FindByOrderID(ctx context.Context, orderID string) (*models.Payment, error)
	FindByID(ctx context.Context, id uuid.UUID) (*models.Payment, error)
	Update(ctx context.Context, payment *models.Payment) error
}

type paymentRepository struct {
	db *gorm.DB
}

func NewPaymentRepository(db *gorm.DB) PaymentRepository {
	return &paymentRepository{db: db}
}

func (r *paymentRepository) Create(ctx context.Context, payment *models.Payment) error {
	return r.db.WithContext(ctx).Create(payment).Error
}

func (r *paymentRepository) FindByOrderID(ctx context.Context, orderID string) (*models.Payment, error) {
	var payment models.Payment
	err := r.db.WithContext(ctx).
		Preload("Transactions").
		First(&payment, "order_id = ?", orderID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &payment, err
}

func (r *paymentRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.Payment, error) {
	var payment models.Payment
	err := r.db.WithContext(ctx).
		Preload("Transactions").
		First(&payment, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &payment, err
}

func (r *paymentRepository) Update(ctx context.Context, payment *models.Payment) error {
	return r.db.WithContext(ctx).Save(payment).Error
}

// ─── Shared Errors ───────────────────────────────────────────────────────────

var (
	ErrNotFound      = errors.New("record not found")
	ErrAlreadyExists = errors.New("record already exists")
)
