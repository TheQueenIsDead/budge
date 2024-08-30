package database

import (
	"bytes"
	"crypto/md5"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"github.com/TheQueenIsDead/budge/pkg/database/buckets"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	bolt "go.etcd.io/bbolt"
	"log/slog"
	"time"
)

type Store struct {
	db           *bolt.DB
	logger       *slog.Logger
	Accounts     AccountStore
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
	// TODO: Ensure this is called on server close.
	//defer db.Close()

	// TODO: Create buckets in a transaction.
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
	merchantStore := NewMerchantStorer(db)
	transactionStore := NewTransactionStorer(db)
	return &Store{
		db:           db,
		Accounts:     accountStore,
		Merchants:    merchantStore,
		Transactions: transactionStore,
	}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func HashModel(m any) [16]byte {
	var b bytes.Buffer
	gob.NewEncoder(&b).Encode(m)
	return md5.Sum(b.Bytes())
}

func (s *Store) Import(account *models.Account, merchants []models.Merchant, transactions []models.Transaction) error {
	tx, err := s.db.Begin(true)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Import Account
	b := tx.Bucket(buckets.AccountBucket)

	buf, err := json.Marshal(account)
	if err != nil {
		return err
	}
	err = b.Put([]byte(account.Number), buf)
	if err != nil {
		return err
	}

	// Import Merchants
	b = tx.Bucket(buckets.MerchantBucket)
	b.NextSequence()
	for _, m := range merchants {
		buf, err = json.Marshal(m)
		if err != nil {
			return err
		}
		err = b.Put([]byte(m.Description), buf)
		if err != nil {
			return err
		}
	}

	// Import Transactions
	b = tx.Bucket(buckets.TransactionBucket)
	for _, t := range transactions {
		buf, err = json.Marshal(t)
		if err != nil {
			return err
		}
		hash := HashModel(t)
		err = b.Put(hash[:], buf)
		if err != nil {
			return err
		}
	}

	// Commit the transaction and check for error.
	if err := tx.Commit(); err != nil {
		return err
	}

	return err
}

func ImportMerchants(db *bolt.DB, merchants []models.Merchant) error {
	tx, err := db.Begin(true)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	b := tx.Bucket(buckets.MerchantBucket)

	for _, m := range merchants {
		buf, err := json.Marshal(m)
		if err != nil {
			return err
		}
		key := HashModel(buf)
		err = b.Put(key[:], buf)
		if err != nil {
			return err
		}
	}

	// Commit the transaction and check for error.
	if err := tx.Commit(); err != nil {
		return err
	}

	return err
}
