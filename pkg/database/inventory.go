package database

import (
	"encoding/json"
	"errors"
	"github.com/TheQueenIsDead/budge/pkg/database/buckets"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	bolt "go.etcd.io/bbolt"
	"log/slog"
)

type InventoryStore interface {
	Count() (int, error)
	Delete(id string) error
	Get(id string) (models.Inventory, error)
	List() ([]models.Inventory, error)
	Put(a models.Inventory) (string, error)
}

type InventoryStorer struct {
	bucket []byte
	db     *bolt.DB
	logger *slog.Logger
}

var _ InventoryStore = (*InventoryStorer)(nil)

func NewInventoryStorer(db *bolt.DB) *InventoryStorer {
	return &InventoryStorer{
		db:     db,
		bucket: buckets.InventoryBucket,
		logger: &slog.Logger{},
	}
}

func (s *InventoryStorer) Count() (int, error) {
	var count int
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(s.bucket)
		if b == nil {
			return errors.New("no account bucket")
		}
		count = b.Stats().KeyN
		return nil
	})
	return count, err
}

func (s *InventoryStorer) Delete(id string) error {
	panic("implement me")
}

func (s *InventoryStorer) Get(id string) (models.Inventory, error) {
	panic("unimplemented")
}

func (s *InventoryStorer) List() ([]models.Inventory, error) {

	var inventory []models.Inventory

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(s.bucket)
		err := b.ForEach(func(k, v []byte) error {
			var i models.Inventory
			err := json.Unmarshal(v, &i)
			if err != nil {
				return err
			}
			inventory = append(inventory, i)
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

	return inventory, nil
}

func (s *InventoryStorer) Put(i models.Inventory) (string, error) {
	var key, value []byte
	err := s.db.Update(func(tx *bolt.Tx) (txErr error) {
		b := tx.Bucket(s.bucket)
		key = i.Key()
		value, txErr = i.Value()
		if txErr != nil {
			return
		}
		return b.Put(key, value)
	})
	return string(key), err
}
