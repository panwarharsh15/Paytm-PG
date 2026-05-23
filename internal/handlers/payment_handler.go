package handlers

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paytm-pg/backend/internal/services"
	"github.com/paytm-pg/backend/internal/utils"
)

// ─── Wallet Handler ──────────────────────────────────────────────────────────

type WalletHandler struct {
	walletService services.WalletService
}

func NewWalletHandler(walletService services.WalletService) *WalletHandler {
	return &WalletHandler{walletService: walletService}
}

func (h *WalletHandler) GetBalance(c *gin.Context) {
	userID := utils.MustGetUserID(c)
	balance, err := h.walletService.GetBalance(c.Request.Context(), userID)
	if err != nil {
		utils.InternalError(c, err)
		return
	}
	utils.OK(c, balance)
}

func (h *WalletHandler) AddMoney(c *gin.Context) {
	userID := utils.MustGetUserID(c)

	var req services.AddMoneyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	txn, err := h.walletService.AddMoney(c.Request.Context(), userID, req)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrWalletLocked):
			utils.ForbiddenError(c, err.Error())
		default:
			utils.InternalError(c, err)
		}
		return
	}

	utils.Created(c, txn)
}

func (h *WalletHandler) Withdraw(c *gin.Context) {
	userID := utils.MustGetUserID(c)

	var req services.WithdrawRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	txn, err := h.walletService.Withdraw(c.Request.Context(), userID, req)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInsufficientBalance):
			utils.BadRequestError(c, err.Error())
		case errors.Is(err, services.ErrWalletLocked):
			utils.ForbiddenError(c, err.Error())
		default:
			utils.InternalError(c, err)
		}
		return
	}

	utils.OK(c, txn)
}

// ─── Payment Handler ─────────────────────────────────────────────────────────

type PaymentHandler struct {
	paymentService services.PaymentService
}

func NewPaymentHandler(paymentService services.PaymentService) *PaymentHandler {
	return &PaymentHandler{paymentService: paymentService}
}

func (h *PaymentHandler) InitiatePayment(c *gin.Context) {
	userID := utils.MustGetUserID(c)

	var req services.InitiatePaymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	payment, err := h.paymentService.InitiatePayment(c.Request.Context(), userID, req)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInsufficientBalance):
			utils.BadRequestError(c, err.Error())
		default:
			utils.InternalError(c, err)
		}
		return
	}

	utils.Created(c, payment)
}

func (h *PaymentHandler) VerifyPayment(c *gin.Context) {
	userID := utils.MustGetUserID(c)

	var req services.VerifyPaymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	payment, err := h.paymentService.VerifyPayment(c.Request.Context(), userID, req)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrPaymentNotFound):
			utils.NotFoundError(c, err.Error())
		case errors.Is(err, services.ErrUnauthorized):
			utils.ForbiddenError(c, err.Error())
		case errors.Is(err, services.ErrSignatureVerificationFailed):
			utils.BadRequestError(c, "Payment verification failed — invalid signature")
		case errors.Is(err, services.ErrInsufficientBalance):
			utils.BadRequestError(c, err.Error())
		default:
			utils.InternalError(c, err)
		}
		return
	}

	utils.OK(c, payment)
}

func (h *PaymentHandler) InitiateRefund(c *gin.Context) {
	userID := utils.MustGetUserID(c)

	var req services.RefundRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	payment, err := h.paymentService.InitiateRefund(c.Request.Context(), userID, req)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrPaymentNotFound):
			utils.NotFoundError(c, err.Error())
		case errors.Is(err, services.ErrUnauthorized):
			utils.ForbiddenError(c, err.Error())
		case errors.Is(err, services.ErrRefundNotAllowed):
			utils.BadRequestError(c, err.Error())
		case errors.Is(err, services.ErrRefundExceedsPayment):
			utils.BadRequestError(c, err.Error())
		default:
			utils.InternalError(c, err)
		}
		return
	}

	utils.OK(c, payment)
}

func (h *PaymentHandler) GetPaymentStatus(c *gin.Context) {
	userID := utils.MustGetUserID(c)
	paymentIDStr := c.Param("payment_id")

	paymentID, err := uuid.Parse(paymentIDStr)
	if err != nil {
		utils.BadRequestError(c, "invalid payment ID format")
		return
	}

	payment, err := h.paymentService.GetPaymentStatus(c.Request.Context(), userID, paymentID)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrPaymentNotFound):
			utils.NotFoundError(c, err.Error())
		case errors.Is(err, services.ErrUnauthorized):
			utils.ForbiddenError(c, err.Error())
		default:
			utils.InternalError(c, err)
		}
		return
	}

	utils.OK(c, payment)
}

func (h *PaymentHandler) GetTransactions(c *gin.Context) {
	userID := utils.MustGetUserID(c)

	var pagination services.PaginationRequest
	if err := c.ShouldBindQuery(&pagination); err != nil {
		utils.ValidationError(c, err)
		return
	}

	result, err := h.paymentService.GetTransactions(c.Request.Context(), userID, pagination)
	if err != nil {
		utils.InternalError(c, err)
		return
	}

	utils.OK(c, result)
}

func (h *PaymentHandler) GetTransactionByID(c *gin.Context) {
	userID := utils.MustGetUserID(c)
	txnIDStr := c.Param("txn_id")

	txnID, err := uuid.Parse(txnIDStr)
	if err != nil {
		utils.BadRequestError(c, "invalid transaction ID format")
		return
	}

	txn, err := h.paymentService.GetTransactionByID(c.Request.Context(), userID, txnID)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrTransactionNotFound):
			utils.NotFoundError(c, err.Error())
		case errors.Is(err, services.ErrUnauthorized):
			utils.ForbiddenError(c, err.Error())
		default:
			utils.InternalError(c, err)
		}
		return
	}

	utils.OK(c, txn)
}
