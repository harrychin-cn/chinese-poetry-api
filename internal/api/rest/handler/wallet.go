package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

const defaultWalletInitialCredits = 20

// WalletHandler exposes the stage-6 wallet, ledger, and tipping MVP.
type WalletHandler struct {
	repo           *database.Repository
	initialCredits int
}

func NewWalletHandler(repo *database.Repository) *WalletHandler {
	return &WalletHandler{repo: repo, initialCredits: defaultWalletInitialCredits}
}

type topUpWalletRequest struct {
	Amount         int    `json:"amount"`
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotency_key"`
}

type tipWorkRequest struct {
	Amount         int    `json:"amount"`
	Message        string `json:"message"`
	IdempotencyKey string `json:"idempotency_key"`
}

// Get returns the current wallet and recent ledger rows.
func (h *WalletHandler) Get(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	wallet, err := h.repo.GetOrCreateCreditWallet(apiKeyID, h.initialCredits)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to read wallet")
		return
	}
	transactions, err := h.repo.ListCreditTransactions(apiKeyID, queryInt(c, "limit", 20))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to read wallet transactions")
		return
	}
	items := make([]map[string]any, len(transactions))
	for i, item := range transactions {
		items[i] = formatCreditTransaction(item)
	}
	respondOK(c, gin.H{"wallet": formatCreditWallet(wallet), "transactions": items})
}

// Transactions returns recent point ledger rows for the current API key.
func (h *WalletHandler) Transactions(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	if _, err := h.repo.GetOrCreateCreditWallet(apiKeyID, h.initialCredits); err != nil {
		respondError(c, http.StatusInternalServerError, "failed to read wallet")
		return
	}
	transactions, err := h.repo.ListCreditTransactions(apiKeyID, queryInt(c, "limit", 50))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to read wallet transactions")
		return
	}
	items := make([]map[string]any, len(transactions))
	for i, item := range transactions {
		items[i] = formatCreditTransaction(item)
	}
	respondOK(c, gin.H{"items": items})
}

// TopUp grants local stage-6 MVP credits to the current wallet.
func (h *WalletHandler) TopUp(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	var req topUpWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid wallet top-up request")
		return
	}
	if req.Amount <= 0 || req.Amount > 100000 {
		respondError(c, http.StatusBadRequest, "amount must be between 1 and 100000")
		return
	}
	key := req.IdempotencyKey
	if key == "" {
		key = "wallet_top_up:" + formatID(apiKeyID) + ":" + formatID(time.Now().UTC().UnixNano())
	}
	reason := req.Reason
	if reason == "" {
		reason = "wallet_top_up"
	}
	wallet, txItem, err := h.repo.GrantCredits(database.GrantCreditsParams{
		APIKeyID:       apiKeyID,
		Amount:         req.Amount,
		Reason:         reason,
		IdempotencyKey: key,
		InitialBalance: h.initialCredits,
	})
	if errors.Is(err, database.ErrInvalidQueryParam) {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to top up wallet")
		return
	}
	respondOK(c, gin.H{"wallet": formatCreditWallet(wallet), "transaction": formatCreditTransaction(*txItem)})
}

// TipWork transfers points from the current wallet to a public work author.
func (h *WalletHandler) TipWork(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	workID, ok := parseWorkID(c)
	if !ok {
		return
	}
	var req tipWorkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid work tip request")
		return
	}
	if req.Amount <= 0 || req.Amount > 10000 {
		respondError(c, http.StatusBadRequest, "amount must be between 1 and 10000")
		return
	}
	key := req.IdempotencyKey
	if key == "" {
		key = "work_tip:" + formatID(apiKeyID) + ":" + formatID(workID) + ":" + formatID(time.Now().UTC().UnixNano())
	}
	tip, senderWallet, recipientWallet, err := h.repo.TipOriginalWork(database.TipOriginalWorkParams{
		FromAPIKeyID:   apiKeyID,
		WorkID:         workID,
		Amount:         req.Amount,
		Message:        req.Message,
		IdempotencyKey: key,
		InitialBalance: h.initialCredits,
	})
	if errors.Is(err, database.ErrInvalidQueryParam) {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, database.ErrInsufficientCredits) {
		c.JSON(http.StatusPaymentRequired, gin.H{
			"error":             "insufficient wallet credits",
			"recharge_endpoint": "/api/v1/wallet/top-up",
		})
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusNotFound, "public work not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to tip work")
		return
	}
	respondOK(c, gin.H{
		"tip":              formatWorkTip(*tip),
		"sender_wallet":    formatCreditWallet(senderWallet),
		"recipient_wallet": formatCreditWallet(recipientWallet),
	})
}

// ListWorkTips returns tip summary and recent tip rows for a visible work.
func (h *WalletHandler) ListWorkTips(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	workID, ok := parseWorkID(c)
	if !ok {
		return
	}
	work, err := h.repo.GetOriginalWork(apiKeyID, workID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		work, err = h.repo.GetPublicOriginalWorkByID(workID)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, database.ErrInvalidQueryParam) {
		respondError(c, http.StatusNotFound, "work not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to read work")
		return
	}
	tips, err := h.repo.ListWorkTips(work.ID, queryInt(c, "limit", 20))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to read work tips")
		return
	}
	summary, err := h.repo.GetWorkTipSummary(work.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to read work tip summary")
		return
	}
	items := make([]map[string]any, len(tips))
	for i, item := range tips {
		items[i] = formatWorkTip(item)
	}
	respondOK(c, gin.H{"summary": summary, "items": items})
}

func formatCreditTransaction(tx database.CreditTransaction) map[string]any {
	return map[string]any{
		"id":               tx.ID,
		"wallet_id":        tx.WalletID,
		"api_key_id":       tx.APIKeyID,
		"work_id":          tx.WorkID,
		"media_asset_id":   tx.MediaAssetID,
		"job_id":           tx.JobID,
		"transaction_type": tx.TransactionType,
		"amount":           tx.Amount,
		"balance_after":    tx.BalanceAfter,
		"reason":           tx.Reason,
		"idempotency_key":  tx.IdempotencyKey,
		"created_at":       tx.CreatedAt,
	}
}

func formatWorkTip(tip database.WorkTip) map[string]any {
	return map[string]any{
		"id":              tip.ID,
		"work_id":         tip.WorkID,
		"from_api_key_id": tip.FromAPIKeyID,
		"to_api_key_id":   tip.ToAPIKeyID,
		"amount":          tip.Amount,
		"message":         tip.Message,
		"idempotency_key": tip.IdempotencyKey,
		"status":          tip.Status,
		"created_at":      tip.CreatedAt,
	}
}
