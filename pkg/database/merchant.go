package database

import (
	"encoding/json"
	"errors"
	"github.com/TheQueenIsDead/budge/pkg/database/buckets"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	bolt "go.etcd.io/bbolt"
)

type MerchantStore interface {
	Count() (int, error)
	Delete(string) error
	Get(string) (models.Merchant, error)
	List() ([]models.Merchant, error)
	Put(models.Merchant) (string, error)
}

type MerchantStorer struct {
	db     *bolt.DB
	bucket []byte
}

var _ MerchantStore = (*MerchantStorer)(nil)

func NewMerchantStorer(db *bolt.DB) *MerchantStorer {
	return &MerchantStorer{
		db:     db,
		bucket: []byte("merchants"),
	}
}

func (s *MerchantStorer) Count() (int, error) {
	var count int
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(s.bucket)
		if b == nil {
			return errors.New("no merchant bucket")
		}
		count = b.Stats().KeyN
		return nil
	})
	return count, err
}

func (s *MerchantStorer) Delete(id string) error {
	//TODO implement me
	panic("implement me")
}

func (s *MerchantStorer) Get(id string) (models.Merchant, error) {
	var m models.Merchant

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(s.bucket)
		buf := b.Get([]byte(id))
		if buf == nil {
			return errors.New("not found")
		}
		err := json.Unmarshal(buf, &m)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return m, err
	}

	return m, nil
}

func (s *MerchantStorer) List() ([]models.Merchant, error) {

	var merchants []models.Merchant

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(buckets.MerchantBucket)

		err := b.ForEach(func(k, v []byte) error {
			var m models.Merchant
			err := json.Unmarshal(v, &m)
			if err != nil {
				return err
			}
			merchants = append(merchants, m)
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

	return merchants, nil
}

func (s *MerchantStorer) Put(m models.Merchant) (string, error) {
	var key, value []byte
	err := s.db.Update(func(tx *bolt.Tx) (txErr error) {
		b := tx.Bucket(s.bucket)
		key = m.Key()
		value, txErr = m.Value()
		if txErr != nil {
			return
		}
		return b.Put(key, value)
	})
	return string(key), err
}
