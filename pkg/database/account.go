package database

import (
	"encoding/json"
	"errors"
	"github.com/TheQueenIsDead/budge/pkg/database/buckets"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	bolt "go.etcd.io/bbolt"
	"log/slog"
)

type AccountStore interface {
	Count() (int, error)
	Delete(id string) error
	Get(id string) (models.Account, error)
	List() ([]models.Account, error)
	Put(a models.Account) (string, error)
}

type AccountStorer struct {
	bucket []byte
	db     *bolt.DB
	logger *slog.Logger
}

var _ AccountStore = (*AccountStorer)(nil)

func NewAccountStorer(db *bolt.DB) *AccountStorer {
	return &AccountStorer{
		db:     db,
		bucket: buckets.AccountBucket,
		logger: &slog.Logger{},
	}
}

func (s *AccountStorer) Count() (int, error) {
	var count int
	var err error
	err = s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(s.bucket)
		if b == nil {
			return errors.New("no account bucket")
		}
		count = b.Stats().KeyN
		return nil
	})
	return count, err
}

func (s *AccountStorer) Delete(id string) error {
	panic("implement me")
}

func (s *AccountStorer) Get(id string) (models.Account, error) {
	panic("unimplemented")
}

func (s *AccountStorer) List() ([]models.Account, error) {

	var accounts []models.Account

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(s.bucket)
		err := b.ForEach(func(k, v []byte) error {
			var a models.Account
			err := json.Unmarshal(v, &a)
			if err != nil {
				return err
			}
			accounts = append(accounts, a)
			return nil
		})
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, nil
	}

	return accounts, nil
}

func (s *AccountStorer) Put(a models.Account) (string, error) {
	// TODO: Catch err
	var err error
	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(s.bucket)
		buf, err := json.Marshal(a)
		if err != nil {
			return err
		}
		err = b.Put([]byte(a.Number), buf)
		if err != nil {
			return err
		}
		return nil
	})
	return "", err
}
