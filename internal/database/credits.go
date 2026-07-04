package database

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	CreditTransactionGrant  = "grant"
	CreditTransactionSpend  = "spend"
	CreditTransactionRefund = "refund"
)

var ErrInsufficientCredits = errors.New("insufficient credits")

// CreditWallet stores the lightweight point balance for one local API key.
type CreditWallet struct {
	ID           int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	APIKeyID     int64     `gorm:"column:api_key_id;not null" json:"api_key_id"`
	Balance      int       `gorm:"column:balance;not null" json:"balance"`
	TotalGranted int       `gorm:"column:total_granted;not null" json:"total_granted"`
	TotalSpent   int       `gorm:"column:total_spent;not null" json:"total_spent"`
	CreatedAt    time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (CreditWallet) TableName() string { return "credit_wallets" }

// CreditTransaction records every point grant/spend/refund.
type CreditTransaction struct {
	ID              int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	WalletID        int64     `gorm:"column:wallet_id;not null" json:"wallet_id"`
	APIKeyID        int64     `gorm:"column:api_key_id;not null" json:"api_key_id"`
	WorkID          *int64    `gorm:"column:work_id" json:"work_id,omitempty"`
	MediaAssetID    *int64    `gorm:"column:media_asset_id" json:"media_asset_id,omitempty"`
	JobID           *int64    `gorm:"column:job_id" json:"job_id,omitempty"`
	TransactionType string    `gorm:"column:transaction_type;not null" json:"transaction_type"`
	Amount          int       `gorm:"column:amount;not null" json:"amount"`
	BalanceAfter    int       `gorm:"column:balance_after;not null" json:"balance_after"`
	Reason          string    `gorm:"column:reason" json:"reason,omitempty"`
	IdempotencyKey  string    `gorm:"column:idempotency_key" json:"idempotency_key,omitempty"`
	CreatedAt       time.Time `gorm:"column:created_at" json:"created_at"`
}

func (CreditTransaction) TableName() string { return "credit_transactions" }

type SpendCreditsParams struct {
	APIKeyID       int64
	WorkID         *int64
	MediaAssetID   *int64
	JobID          *int64
	Amount         int
	Reason         string
	IdempotencyKey string
	InitialBalance int
}

// GetOrCreateCreditWallet returns a wallet and grants the initial balance once.
func (r *Repository) GetOrCreateCreditWallet(apiKeyID int64, initialBalance int) (*CreditWallet, error) {
	if apiKeyID < 1 {
		return nil, ErrInvalidQueryParam
	}
	if initialBalance < 0 {
		initialBalance = 0
	}

	var wallet CreditWallet
	err := r.db.Transaction(func(tx *gorm.DB) error {
		err := tx.Where("api_key_id = ?", apiKeyID).First(&wallet).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		now := time.Now().UTC()
		wallet = CreditWallet{
			APIKeyID:     apiKeyID,
			Balance:      initialBalance,
			TotalGranted: initialBalance,
			TotalSpent:   0,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := tx.Create(&wallet).Error; err != nil {
			return err
		}
		if initialBalance > 0 {
			txItem := CreditTransaction{
				WalletID:        wallet.ID,
				APIKeyID:        apiKeyID,
				TransactionType: CreditTransactionGrant,
				Amount:          initialBalance,
				BalanceAfter:    initialBalance,
				Reason:          "initial_image_credits",
				IdempotencyKey:  fmt.Sprintf("initial:%d", apiKeyID),
				CreatedAt:       now,
			}
			if err := tx.Create(&txItem).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &wallet, nil
}

func (r *Repository) GetCreditWalletByAPIKeyID(apiKeyID int64) (*CreditWallet, error) {
	if apiKeyID < 1 {
		return nil, ErrInvalidQueryParam
	}
	var wallet CreditWallet
	err := r.db.Where("api_key_id = ?", apiKeyID).First(&wallet).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, gorm.ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	return &wallet, nil
}

func (r *Repository) EnsureCreditsAvailable(apiKeyID int64, amount int, initialBalance int) (*CreditWallet, error) {
	wallet, err := r.GetOrCreateCreditWallet(apiKeyID, initialBalance)
	if err != nil {
		return nil, err
	}
	if amount > 0 && wallet.Balance < amount {
		return wallet, ErrInsufficientCredits
	}
	return wallet, nil
}

func (r *Repository) SpendCredits(params SpendCreditsParams) (*CreditWallet, *CreditTransaction, error) {
	if params.APIKeyID < 1 {
		return nil, nil, ErrInvalidQueryParam
	}
	if params.Amount <= 0 {
		wallet, err := r.GetOrCreateCreditWallet(params.APIKeyID, params.InitialBalance)
		return wallet, nil, err
	}

	var wallet CreditWallet
	var txItem CreditTransaction
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if params.IdempotencyKey != "" {
			err := tx.Where("idempotency_key = ?", params.IdempotencyKey).First(&txItem).Error
			if err == nil {
				return tx.Where("id = ?", txItem.WalletID).First(&wallet).Error
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}

		err := tx.Where("api_key_id = ?", params.APIKeyID).First(&wallet).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			initial := params.InitialBalance
			if initial < 0 {
				initial = 0
			}
			now := time.Now().UTC()
			wallet = CreditWallet{
				APIKeyID:     params.APIKeyID,
				Balance:      initial,
				TotalGranted: initial,
				CreatedAt:    now,
				UpdatedAt:    now,
			}
			if err := tx.Create(&wallet).Error; err != nil {
				return err
			}
			if initial > 0 {
				grant := CreditTransaction{
					WalletID:        wallet.ID,
					APIKeyID:        params.APIKeyID,
					TransactionType: CreditTransactionGrant,
					Amount:          initial,
					BalanceAfter:    initial,
					Reason:          "initial_image_credits",
					IdempotencyKey:  fmt.Sprintf("initial:%d", params.APIKeyID),
					CreatedAt:       now,
				}
				if err := tx.Create(&grant).Error; err != nil {
					return err
				}
			}
		} else if err != nil {
			return err
		}

		if wallet.Balance < params.Amount {
			return ErrInsufficientCredits
		}

		now := time.Now().UTC()
		newBalance := wallet.Balance - params.Amount
		if err := tx.Model(&CreditWallet{}).Where("id = ? AND balance >= ?", wallet.ID, params.Amount).Updates(map[string]any{
			"balance":     gorm.Expr("balance - ?", params.Amount),
			"total_spent": gorm.Expr("total_spent + ?", params.Amount),
			"updated_at":  now,
		}).Error; err != nil {
			return err
		}

		txItem = CreditTransaction{
			WalletID:        wallet.ID,
			APIKeyID:        params.APIKeyID,
			WorkID:          params.WorkID,
			MediaAssetID:    params.MediaAssetID,
			JobID:           params.JobID,
			TransactionType: CreditTransactionSpend,
			Amount:          -params.Amount,
			BalanceAfter:    newBalance,
			Reason:          limitString(strings.TrimSpace(params.Reason), 200),
			IdempotencyKey:  limitString(strings.TrimSpace(params.IdempotencyKey), 160),
			CreatedAt:       now,
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
