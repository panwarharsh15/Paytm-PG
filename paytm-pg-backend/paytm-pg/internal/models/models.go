package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ─── Base Model (replaces gorm.Model — gives us UUID PKs) ───────────────────

type Base struct {
	ID        uuid.UUID      `json:"id" gorm:"type:uuid;primaryKey"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"` // soft delete
}

func (b *Base) BeforeCreate(tx *gorm.DB) error {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	return nil
}

// ─── User ────────────────────────────────────────────────────────────────────

type User struct {
	Base
	FullName    string  `json:"full_name" gorm:"not null"`
	Email       string  `json:"email" gorm:"uniqueIndex;not null"`
	Phone       string  `json:"phone" gorm:"uniqueIndex;not null"`
	Password    string  `json:"-" gorm:"not null"` // never serialized to JSON
	IsVerified  bool    `json:"is_verified" gorm:"default:false"`
	IsActive    bool    `json:"is_active" gorm:"default:true"`
	Role        string  `json:"role" gorm:"default:'user'"` // user | admin | merchant

	// Relations
	Wallet       *Wallet       `json:"wallet,omitempty" gorm:"foreignKey:UserID"`
	Transactions []Transaction `json:"transactions,omitempty" gorm:"foreignKey:UserID"`
}

func (User) TableName() string { return "users" }

// ─── Wallet ──────────────────────────────────────────────────────────────────

type Wallet struct {
	Base
	UserID   uuid.UUID `json:"user_id" gorm:"type:uuid;uniqueIndex;not null"`
	Balance  float64   `json:"balance" gorm:"type:decimal(15,2);default:0"`
	Currency string    `json:"currency" gorm:"default:'INR'"`
	IsLocked bool      `json:"is_locked" gorm:"default:false"` // fraud freeze

	User User `json:"-" gorm:"foreignKey:UserID"`
}

func (Wallet) TableName() string { return "wallets" }

// ─── Transaction ─────────────────────────────────────────────────────────────

type TransactionType string
type TransactionStatus string

const (
	TxnTypeCredit   TransactionType = "CREDIT"
	TxnTypeDebit    TransactionType = "DEBIT"
	TxnTypeRefund   TransactionType = "REFUND"

	TxnStatusPending   TransactionStatus = "PENDING"
	TxnStatusSuccess   TransactionStatus = "SUCCESS"
	TxnStatusFailed    TransactionStatus = "FAILED"
	TxnStatusReversed  TransactionStatus = "REVERSED"
)

type Transaction struct {
	Base
	UserID          uuid.UUID         `json:"user_id" gorm:"type:uuid;index;not null"`
	WalletID        uuid.UUID         `json:"wallet_id" gorm:"type:uuid;index;not null"`
	PaymentID       *uuid.UUID        `json:"payment_id,omitempty" gorm:"type:uuid;index"`
	Amount          float64           `json:"amount" gorm:"type:decimal(15,2);not null"`
	BalanceBefore   float64           `json:"balance_before" gorm:"type:decimal(15,2)"`
	BalanceAfter    float64           `json:"balance_after" gorm:"type:decimal(15,2)"`
	Type            TransactionType   `json:"type" gorm:"not null"`
	Status          TransactionStatus `json:"status" gorm:"default:'PENDING'"`
	ReferenceID     string            `json:"reference_id" gorm:"uniqueIndex"` // idempotency key
	Description     string            `json:"description"`
	Metadata        string            `json:"metadata" gorm:"type:jsonb"` // flexible extra data

	User    User    `json:"-" gorm:"foreignKey:UserID"`
	Wallet  Wallet  `json:"-" gorm:"foreignKey:WalletID"`
}

func (Transaction) TableName() string { return "transactions" }

// ─── Payment ─────────────────────────────────────────────────────────────────

type PaymentStatus string
type PaymentMethod string

const (
	PaymentStatusCreated    PaymentStatus = "CREATED"
	PaymentStatusPending    PaymentStatus = "PENDING"
	PaymentStatusCaptured   PaymentStatus = "CAPTURED"
	PaymentStatusFailed     PaymentStatus = "FAILED"
	PaymentStatusRefunded   PaymentStatus = "REFUNDED"

	PaymentMethodWallet     PaymentMethod = "WALLET"
	PaymentMethodUPI        PaymentMethod = "UPI"
	PaymentMethodCard       PaymentMethod = "CARD"
	PaymentMethodNetBanking PaymentMethod = "NET_BANKING"
)

type Payment struct {
	Base
	OrderID         string        `json:"order_id" gorm:"uniqueIndex;not null"`
	UserID          uuid.UUID     `json:"user_id" gorm:"type:uuid;index;not null"`
	Amount          float64       `json:"amount" gorm:"type:decimal(15,2);not null"`
	Currency        string        `json:"currency" gorm:"default:'INR'"`
	Status          PaymentStatus `json:"status" gorm:"default:'CREATED'"`
	Method          PaymentMethod `json:"method"`
	Description     string        `json:"description"`
	GatewayOrderID  string        `json:"gateway_order_id"`  // razorpay order id
	GatewayPaymentID string       `json:"gateway_payment_id"` // razorpay payment id
	GatewaySignature string       `json:"-"`                  // never expose
	CallbackURL     string        `json:"callback_url"`
	Metadata        string        `json:"metadata" gorm:"type:jsonb"`
	FailureReason   string        `json:"failure_reason,omitempty"`
	CapturedAt      *time.Time    `json:"captured_at,omitempty"`
	RefundedAt      *time.Time    `json:"refunded_at,omitempty"`
	RefundAmount    float64       `json:"refund_amount,omitempty" gorm:"type:decimal(15,2)"`

	User         User          `json:"-" gorm:"foreignKey:UserID"`
	Transactions []Transaction `json:"transactions,omitempty" gorm:"foreignKey:PaymentID"`
}

func (Payment) TableName() string { return "payments" }

// ─── Refresh Token ───────────────────────────────────────────────────────────

type RefreshToken struct {
	Base
	UserID    uuid.UUID `json:"user_id" gorm:"type:uuid;index;not null"`
	Token     string    `json:"-" gorm:"uniqueIndex;not null"`
	ExpiresAt time.Time `json:"expires_at"`
	IsRevoked bool      `json:"is_revoked" gorm:"default:false"`
	UserAgent string    `json:"user_agent"`
	IPAddress string    `json:"ip_address"`

	User User `json:"-" gorm:"foreignKey:UserID"`
}

func (RefreshToken) TableName() string { return "refresh_tokens" }
