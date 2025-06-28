package database

import (
	"errors"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/buckets"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	bolt "go.etcd.io/bbolt"
)

/* Accounts */

func (s *Store) CountAccount() (int, error) {
	return Count[models.Account](s.db)
}
func (s *Store) GetAccount(id []byte) (models.Account, error) {
	return Get[models.Account](s.db, id)
}
func (s *Store) GetAccountsTotal() (float64, error) {
	accounts, err := Read[models.Account](s.db)
	if err != nil {
		return 0, err
	}
	total := 0.0
	for _, account := range accounts {
		total += account.Balance.Current
	}
	return total, nil
}
func (s *Store) CreateAccount(account models.Account) error {
	return Create[models.Account](s.db, account)
}
func (s *Store) ReadAccounts() ([]models.Account, error) {
	return Read[models.Account](s.db)
}

/* Inventory */
func (s *Store) CreateInventory(inventory models.Inventory) error {
	return Create[models.Inventory](s.db, inventory)
}

func (s *Store) ReadInventory() ([]models.Inventory, error) {
	return Read[models.Inventory](s.db)
}

/* Merchants */

func (s *Store) CreateMerchant(merchant models.Merchant) error {
	return Create[models.Merchant](s.db, merchant)
}

/* Transactions */

func (s *Store) CountTransactions() (int, error) {
	return Count[models.Transaction](s.db)
}
func (s *Store) CreateTransaction(transaction models.Transaction) error {
	return Create[models.Transaction](s.db, transaction)
}
func (s *Store) ReadTransactions() ([]models.Transaction, error) {
	return Read[models.Transaction](s.db)
}
func (s *Store) ReadTransactionsByAccount(account string) ([]models.Transaction, error) {
	return ReadFilter[models.Transaction](s.db, func(transaction models.Transaction) bool {
		return transaction.Account == account
	})
}
func (s *Store) ReadTransactionsByDate(start time.Time, end time.Time) ([]models.Transaction, error) {
	return ReadFilter[models.Transaction](s.db, func(transaction models.Transaction) bool {
		return transaction.Date.After(start) && transaction.Date.Before(end)
	})
}
func (s *Store) SearchTransactions(search string, account string, start time.Time, end time.Time) ([]models.Transaction, error) {
	return ReadFilter[models.Transaction](s.db, func(transaction models.Transaction) bool {
		accountMatch := account == "" || transaction.Account == account
		dateMatch := transaction.Date.After(start) && transaction.Date.Before(end)

		descriptionMatch := strings.Contains(strings.ToLower(transaction.Description), strings.ToLower(search))
		merchantMatch := strings.Contains(strings.ToLower(transaction.Merchant.Name), strings.ToLower(search))
		categoryMatch := strings.Contains(strings.ToLower(transaction.Category.Groups.PersonalFinance.Name), strings.ToLower(search))
		searchMatch := descriptionMatch || merchantMatch || categoryMatch

		return accountMatch && dateMatch && searchMatch
	})
}

/* Settings */

func (s *Store) GetAkahuSettings() (models.IntegrationAkahuSettings, error) {
	return Get[models.IntegrationAkahuSettings](s.db, models.IntegrationAkahuSettings{}.Key())
}
func (s *Store) UpdateAkahuSettings(settings models.IntegrationAkahuSettings) error {
	return Update[models.IntegrationAkahuSettings](s.db, settings)
}
func (s *Store) UpdateAkahuLastSync() error {
	settings, err := Get[models.IntegrationAkahuSettings](s.db, models.IntegrationAkahuSettings{}.Key())
	if err != nil {
		return err
	}
	settings.LastSync = time.Now()
	return Update[models.IntegrationAkahuSettings](s.db, settings)
}
func (s *Store) ResetAkahuLastSync() error {
	settings, err := Get[models.IntegrationAkahuSettings](s.db, models.IntegrationAkahuSettings{}.Key())
	if err != nil {
		return err
	}
	settings.LastSync = time.Time{}
	return Update[models.IntegrationAkahuSettings](s.db, settings)
}
func (s *Store) DeleteSynced() error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		accountErr := tx.DeleteBucket(buckets.AccountBucket)
		merchantErr := tx.DeleteBucket(buckets.MerchantBucket)
		transactionErr := tx.DeleteBucket(buckets.TransactionBucket)
		return errors.Join(accountErr, merchantErr, transactionErr)
	})
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		_, accountErr := tx.CreateBucket(buckets.AccountBucket)
		_, merchantErr := tx.CreateBucket(buckets.MerchantBucket)
		_, transactionErr := tx.CreateBucket(buckets.TransactionBucket)
		return errors.Join(accountErr, merchantErr, transactionErr)
	})
}
