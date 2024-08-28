package database

import (
	"encoding/json"
	"errors"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	bolt "go.etcd.io/bbolt"
)

type TransactionStore interface {
	Count() (int, error)
	Delete(id string) error
	Get(id string) (models.Transaction, error)
	List() ([]models.Transaction, error)
	Put(t models.Transaction) (string, error)
}

type TransactionStorer struct {
	db     *bolt.DB
	bucket []byte
}

var _ TransactionStore = (*TransactionStorer)(nil)

func NewTransactionStorer(db *bolt.DB) *TransactionStorer {
	return &TransactionStorer{
		db:     db,
		bucket: []byte("transactions"),
	}
}

func (s *TransactionStorer) Count() (int, error) {
	var count int
	var err error
	err = s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(s.bucket)
		if b == nil {
			return errors.New("no transaction bucket")
		}
		count = b.Stats().KeyN
		return nil
	})
	return count, err
}

func (s *TransactionStorer) Delete(id string) error {
	//TODO implement me
	panic("implement me")
}

func (s *TransactionStorer) Get(id string) (models.Transaction, error) {
	//TODO implement me
	panic("implement me")
}

func (s *TransactionStorer) List() ([]models.Transaction, error) {

	var transactions []models.Transaction

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(s.bucket)
		err := b.ForEach(func(k, v []byte) error {
			var t models.Transaction
			err := json.Unmarshal(v, &t)
			if err != nil {
				return err
			}
			transactions = append(transactions, t)
			return nil
		})
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return transactions, nil
	}

	return transactions, nil
}

func (s *TransactionStorer) Put(t models.Transaction) (string, error) {
	//TODO implement me
	panic("implement me")
}
