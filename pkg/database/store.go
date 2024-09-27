package database

import (
	"fmt"
	"github.com/TheQueenIsDead/budge/pkg/database/buckets"
	bolt "go.etcd.io/bbolt"
	"time"
)

type Storable interface {
	Key() []byte
	Bucket() []byte
}

type Store struct {
	db *bolt.DB
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

	return &Store{
		db:           db,
	}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}
