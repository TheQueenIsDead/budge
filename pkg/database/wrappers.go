package database

import (
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"time"
)

/* Accounts */

func (s *Store) CountAccount() (int, error) {
	return Count[models.Account](s.db)
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
func (s *Store) DeleteInventory(id []byte) error {
	return Delete[models.Inventory](s.db, id)
}

/* Merchants */

func (s *Store) CountMerchant() (int, error) {
	return Count[models.Merchant](s.db)
}
func (s *Store) CreateMerchant(merchant models.Merchant) error {
	return Create[models.Merchant](s.db, merchant)
}
func (s *Store) GetMerchant(id []byte) (models.Merchant, error) {
	return Get[models.Merchant](s.db, id)
}
func (s *Store) ReadMerchants() ([]models.Merchant, error) {
	return Read[models.Merchant](s.db)
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

/* Settings */

func (s *Store) GetAkahuSettings() (models.IntegrationAkahuSettings, error) {
	return Get[models.IntegrationAkahuSettings](s.db, models.IntegrationAkahuSettings{}.Key())
}
func (s *Store) UpdateAkahuSettings(settings models.IntegrationAkahuSettings) error {
	return Update[models.IntegrationAkahuSettings](s.db, settings)
}
