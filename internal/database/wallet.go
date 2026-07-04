package database

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	WorkTipStatusSucceeded = "succeeded"
)

// WorkTip records a point tip from one API key wallet to a public work author.
type WorkTip struct {
	ID             int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	WorkID         int64     `gorm:"column:work_id;not null" json:"work_id"`
	FromAPIKeyID   int64     `gorm:"column:from_api_key_id;not null" json:"from_api_key_id"`
	ToAPIKeyID     int64     `gorm:"column:to_api_key_id;not null" json:"to_api_key_id"`
	Amount         int       `gorm:"column:amount;not null" json:"amount"`
	Message        string    `gorm:"column:message" json:"message,omitempty"`
	IdempotencyKey string    `gorm:"column:idempotency_key" json:"idempotency_key,omitempty"`
	Status         string    `gorm:"column:status;not null" json:"status"`
	CreatedAt      time.Time `gorm:"column:created_at" json:"created_at"`
}

func (WorkTip) TableName() string { return "work_tips" }

type GrantCreditsParams struct {
	APIKeyID       int64
	WorkID         *int64
	Amount         int
	Reason         string
	IdempotencyKey string
	InitialBalance int
}

type TipOriginalWorkParams struct {
	FromAPIKeyID   int64
	WorkID         int64
	Amount         int
	Message        string
	IdempotencyKey string
	InitialBalance int
}

type WorkTipSummary struct {
	WorkID      int64 `json:"work_id"`
	TipCount    int64 `json:"tip_count"`
	TotalAmount int   `json:"total_amount"`
}

// GrantCredits adds points to one wallet and records a positive transaction.
func (r *Repository) GrantCredits(params GrantCreditsParams) (*CreditWallet, *CreditTransaction, error) {
	if params.APIKeyID < 1 || params.Amount <= 0 {
		return nil, nil, ErrInvalidQueryParam
	}
	key := normalizeCreditIdempotencyKey(params.IdempotencyKey)
	if key == "" {
		key = fmt.Sprintf("grant:%d:%d", params.APIKeyID, time.Now().UTC().UnixNano())
	}

	var wallet CreditWallet
	var txItem CreditTransaction
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if key != "" {
			err := tx.Where("api_key_id = ? AND idempotency_key = ?", params.APIKeyID, key).First(&txItem).Error
			if err == nil {
				return tx.Where("id = ?", txItem.WalletID).First(&wallet).Error
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}

		if err := getOrCreateCreditWalletTx(tx, params.APIKeyID, params.InitialBalance, &wallet); err != nil {
			return err
		}

		now := time.Now().UTC()
		newBalance := wallet.Balance + params.Amount
		if err := tx.Model(&CreditWallet{}).Where("id = ?", wallet.ID).Updates(map[string]any{
			"balance":       gorm.Expr("balance + ?", params.Amount),
			"total_granted": gorm.Expr("total_granted + ?", params.Amount),
			"updated_at":    now,
		}).Error; err != nil {
			return err
		}

		txItem = CreditTransaction{
			WalletID:        wallet.ID,
			APIKeyID:        params.APIKeyID,
			WorkID:          params.WorkID,
			TransactionType: CreditTransactionGrant,
			Amount:          params.Amount,
			BalanceAfter:    newBalance,
			Reason:          limitString(strings.TrimSpace(params.Reason), 200),
			IdempotencyKey:  key,
			CreatedAt:       now,
		}
		if txItem.Reason == "" {
			txItem.Reason = "wallet_top_up"
		}
		if err := tx.Create(&txItem).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", wallet.ID).First(&wallet).Error
	})
	if err != nil {
		return nil, nil, err
	}
	return &wallet, &txItem, nil
}

// ListCreditTransactions returns recent wallet ledger rows for one API key.
func (r *Repository) ListCreditTransactions(apiKeyID int64, limit int) ([]CreditTransaction, error) {
	if apiKeyID < 1 {
		return nil, ErrInvalidQueryParam
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	var items []CreditTransaction
	if err := r.db.Where("api_key_id = ?", apiKeyID).Order("created_at DESC, id DESC").Limit(limit).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// TipOriginalWork transfers points from the current API key to a public work author.
func (r *Repository) TipOriginalWork(params TipOriginalWorkParams) (*WorkTip, *CreditWallet, *CreditWallet, error) {
	if params.FromAPIKeyID < 1 || params.WorkID < 1 || params.Amount <= 0 {
		return nil, nil, nil, ErrInvalidQueryParam
	}
	key := normalizeCreditIdempotencyKey(params.IdempotencyKey)
	if key == "" {
		key = fmt.Sprintf("tip:%d:%d:%d", params.FromAPIKeyID, params.WorkID, time.Now().UTC().UnixNano())
	}

	var tip WorkTip
	var senderWallet CreditWallet
	var recipientWallet CreditWallet
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if key != "" {
			err := tx.Where("from_api_key_id = ? AND idempotency_key = ?", params.FromAPIKeyID, key).First(&tip).Error
			if err == nil {
				_ = tx.Where("api_key_id = ?", params.FromAPIKeyID).First(&senderWallet).Error
				_ = tx.Where("api_key_id = ?", tip.ToAPIKeyID).First(&recipientWallet).Error
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}

		var work OriginalWork
		if err := tx.Where("id = ? AND status = ? AND visibility = ?", params.WorkID, WorkStatusPublished, WorkVisibilityPublic).First(&work).Error; err != nil {
			return err
		}
		if work.APIKeyID == params.FromAPIKeyID {
			return fmt.Errorf("%w: cannot tip your own work", ErrInvalidQueryParam)
		}

		if err := getOrCreateCreditWalletTx(tx, params.FromAPIKeyID, params.InitialBalance, &senderWallet); err != nil {
			return err
		}
		if senderWallet.Balance < params.Amount {
			return ErrInsufficientCredits
		}
		if err := getOrCreateCreditWalletTx(tx, work.APIKeyID, 0, &recipientWallet); err != nil {
			return err
		}

		now := time.Now().UTC()
		senderBalanceAfter := senderWallet.Balance - params.Amount
		res := tx.Model(&CreditWallet{}).Where("id = ? AND balance >= ?", senderWallet.ID, params.Amount).Updates(map[string]any{
			"balance":     gorm.Expr("balance - ?", params.Amount),
			"total_spent": gorm.Expr("total_spent + ?", params.Amount),
			"updated_at":  now,
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrInsufficientCredits
		}

		debit := CreditTransaction{
			WalletID:        senderWallet.ID,
			APIKeyID:        params.FromAPIKeyID,
			WorkID:          &work.ID,
			TransactionType: CreditTransactionSpend,
			Amount:          -params.Amount,
			BalanceAfter:    senderBalanceAfter,
			Reason:          "work_tip_sent",
			IdempotencyKey:  "tip:send:" + key,
			CreatedAt:       now,
		}
		if err := tx.Create(&debit).Error; err != nil {
			return err
		}

		recipientBalanceAfter := recipientWallet.Balance + params.Amount
		if err := tx.Model(&CreditWallet{}).Where("id = ?", recipientWallet.ID).Updates(map[string]any{
			"balance":       gorm.Expr("balance + ?", params.Amount),
			"total_granted": gorm.Expr("total_granted + ?", params.Amount),
			"updated_at":    now,
		}).Error; err != nil {
			return err
		}

		credit := CreditTransaction{
			WalletID:        recipientWallet.ID,
			APIKeyID:        work.APIKeyID,
			WorkID:          &work.ID,
			TransactionType: CreditTransactionGrant,
			Amount:          params.Amount,
			BalanceAfter:    recipientBalanceAfter,
			Reason:          "work_tip_received",
			IdempotencyKey:  "tip:recv:" + key,
			CreatedAt:       now,
		}
		if err := tx.Create(&credit).Error; err != nil {
			return err
		}

		tip = WorkTip{
			WorkID:         work.ID,
			FromAPIKeyID:   params.FromAPIKeyID,
			ToAPIKeyID:     work.APIKeyID,
			Amount:         params.Amount,
			Message:        limitString(strings.TrimSpace(params.Message), 300),
			IdempotencyKey: key,
			Status:         WorkTipStatusSucceeded,
			CreatedAt:      now,
		}
		if err := tx.Create(&tip).Error; err != nil {
			return err
		}

		if err := tx.Where("id = ?", senderWallet.ID).First(&senderWallet).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", recipientWallet.ID).First(&recipientWallet).Error
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return &tip, &senderWallet, &recipientWallet, nil
}

func (r *Repository) ListWorkTips(workID int64, limit int) ([]WorkTip, error) {
	if workID < 1 {
		return nil, ErrInvalidQueryParam
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	var items []WorkTip
	if err := r.db.Where("work_id = ? AND status = ?", workID, WorkTipStatusSucceeded).Order("created_at DESC, id DESC").Limit(limit).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) GetWorkTipSummary(workID int64) (*WorkTipSummary, error) {
	if workID < 1 {
		return nil, ErrInvalidQueryParam
	}
	var row struct {
		TipCount    int64
		TotalAmount int
	}
	if err := r.db.Table("work_tips").Select("COUNT(*) AS tip_count, COALESCE(SUM(amount), 0) AS total_amount").Where("work_id = ? AND status = ?", workID, WorkTipStatusSucceeded).Scan(&row).Error; err != nil {
		return nil, err
	}
	return &WorkTipSummary{WorkID: workID, TipCount: row.TipCount, TotalAmount: row.TotalAmount}, nil
}

func (r *Repository) GetPublicOriginalWorkByID(id int64) (*OriginalWork, error) {
	if id < 1 {
		return nil, ErrInvalidQueryParam
	}
	var work OriginalWork
	if err := r.db.Where("id = ? AND status = ? AND visibility = ?", id, WorkStatusPublished, WorkVisibilityPublic).First(&work).Error; err != nil {
		return nil, err
	}
	return &work, nil
}

func getOrCreateCreditWalletTx(tx *gorm.DB, apiKeyID int64, initialBalance int, wallet *CreditWallet) error {
	if apiKeyID < 1 {
		return ErrInvalidQueryParam
	}
	if initialBalance < 0 {
		initialBalance = 0
	}
	err := tx.Where("api_key_id = ?", apiKeyID).First(wallet).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	now := time.Now().UTC()
	*wallet = CreditWallet{
		APIKeyID:     apiKeyID,
		Balance:      initialBalance,
		TotalGranted: initialBalance,
		TotalSpent:   0,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := tx.Create(wallet).Error; err != nil {
		return err
	}
	if initialBalance > 0 {
		initial := CreditTransaction{
			WalletID:        wallet.ID,
			APIKeyID:        apiKeyID,
			TransactionType: CreditTransactionGrant,
			Amount:          initialBalance,
			BalanceAfter:    initialBalance,
			Reason:          "initial_image_credits",
			IdempotencyKey:  fmt.Sprintf("initial:%d", apiKeyID),
			CreatedAt:       now,
		}
		if err := tx.Create(&initial).Error; err != nil {
			return err
		}
	}
	return nil
}

func normalizeCreditIdempotencyKey(value string) string {
	return limitString(strings.TrimSpace(value), 160)
}
