package database

import (
	"fmt"
	"github.com/TheQueenIsDead/budge/pkg/database/buckets"
	bolt "go.etcd.io/bbolt"
	"time"
)

type Store struct {
	db *bolt.DB
	//logger       *slog.Logger // TODO: Use me

	Accounts     AccountStore
	Inventory    InventoryStore
	Merchants    MerchantStore
	Transactions TransactionStore
}

func NewStore() (*Store, error) {

	opts := bolt.DefaultOptions
	opts.Timeout = 5 * time.Second
	db, err := bolt.Open("budge.bolt.db", 0600, opts)
	if err != nil {
		return nil, err
	}

	for _, bucket := range buckets.All() {
		err := db.Update(func(tx *bolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists(bucket)
			if err != nil {
				return fmt.Errorf("create bucket: %s", err)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	accountStore := NewAccountStorer(db)
	inventoryStore := NewInventoryStorer(db)
	merchantStore := NewMerchantStorer(db)
	transactionStore := NewTransactionStorer(db)
	return &Store{
		db:           db,
		Accounts:     accountStore,
		Inventory:    inventoryStore,
		Merchants:    merchantStore,
		Transactions: transactionStore,
	}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}
