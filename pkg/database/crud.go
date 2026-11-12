package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

// ErrNotFound is what Get answers for a key that is not there, so a caller
// can tell a missing document, often a bad link, from one that could not be
// read.
var ErrNotFound = errors.New("not found")

// Create inserts a new Storable object and returns an error if the Key already exists.
func Create[T Storable](ctx context.Context, db *bolt.DB, item T) error {
	// TODO: Error if something already exists
	return update(ctx, db, bucketOf[T](), func(tx *bolt.Tx) error {
		t := *(new(T))
		b := tx.Bucket(t.Bucket())
		value, err := json.Marshal(&item)
		if err != nil {
			return err
		}
		return b.Put(item.Key(), value)
	})
}

// Count returns the number of items in the bucket dictated by the Storable.Bucket() method.
func Count[T Storable](ctx context.Context, db *bolt.DB) (int, error) {
	var count int
	var result T
	err := view(ctx, db, bucketOf[T](), func(tx *bolt.Tx) error {
		b := tx.Bucket(result.Bucket())
		if b == nil {
			return fmt.Errorf("no %s bucket", result.Bucket())
		}
		count = b.Stats().KeyN
		return nil
	})
	return count, err
}

// Get retrieves a Storable of type T given an identifier. The identifier should correspond to the returned value
// of the Storable.Key() method. An identifier with nothing saved under it is ErrNotFound.
func Get[T Storable](ctx context.Context, db *bolt.DB, id []byte) (T, error) {
	var result T
	err := view(ctx, db, bucketOf[T](), func(tx *bolt.Tx) error {
		b := tx.Bucket(result.Bucket())
		buf := b.Get(id)
		if buf == nil {
			return ErrNotFound
		}
		return json.Unmarshal(buf, &result)
	})
	return result, err
}

// GetOptional is Get for a document that may never have been saved. It
// reports a missing document as not found rather than as an error, so only a
// document that is there and cannot be read is an error.
func GetOptional[T Storable](ctx context.Context, db *bolt.DB, id []byte) (result T, found bool, err error) {
	err = view(ctx, db, bucketOf[T](), func(tx *bolt.Tx) error {
		buf := tx.Bucket(result.Bucket()).Get(id)
		if buf == nil {
			return nil
		}
		found = true
		return json.Unmarshal(buf, &result)
	})
	return result, found, err
}

// Read iterates the bucket for the given Storable and returns all models of type T.
func Read[T Storable](ctx context.Context, db *bolt.DB) ([]T, error) {
	var result []T
	t := *(new(T))
	err := view(ctx, db, bucketOf[T](), func(tx *bolt.Tx) error {
		b := tx.Bucket(t.Bucket())
		return b.ForEach(func(k, v []byte) error {
			item := new(T)
			err := json.Unmarshal(v, &item)
			if err != nil {
				return err
			}
			result = append(result, *item)
			return nil
		})
	})
	return result, err
}

// ReadFilter iterates the bucket for the given Storable and returns all models of type T
// if the given filter function returns true.
func ReadFilter[T Storable](ctx context.Context, db *bolt.DB, filter func(T) bool) ([]T, error) {
	var result []T
	t := *(new(T))
	err := view(ctx, db, bucketOf[T](), func(tx *bolt.Tx) error {
		b := tx.Bucket(t.Bucket())
		return b.ForEach(func(k, v []byte) error {
			item := new(T)
			err := json.Unmarshal(v, &item)
			if err != nil {
				return err
			}
			if filter(*item) {
				result = append(result, *item)
			}
			return nil
		})
	})
	return result, err
}

// Update overwrites a pre-existing Storable object and returns an error if the Key does not exist.
func Update[T Storable](ctx context.Context, db *bolt.DB, item T) error {
	// TODO: Error if the item does not exist.
	return update(ctx, db, bucketOf[T](), func(tx *bolt.Tx) error {
		t := *(new(T))
		b := tx.Bucket(t.Bucket())
		value, err := json.Marshal(&item)
		if err != nil {
			return err
		}
		return b.Put(item.Key(), value)
	})
}

// Delete removes an object of type T given an identifier. The identifier should correspond to the returned value
// of the Storable.Key() method.
func Delete[T Storable](ctx context.Context, db *bolt.DB, id []byte) error {
	return update(ctx, db, bucketOf[T](), func(tx *bolt.Tx) error {
		t := *(new(T))
		b := tx.Bucket(t.Bucket())
		return b.Delete(id)
	})
}

// putAll writes every item into its bucket within an open transaction, so a
// caller can store many objects behind a single commit.
func putAll[T Storable](tx *bolt.Tx, items []T) error {
	t := *(new(T))
	b := tx.Bucket(t.Bucket())
	for _, item := range items {
		value, err := json.Marshal(&item)
		if err != nil {
			return err
		}
		if err := b.Put(item.Key(), value); err != nil {
			return err
		}
	}
	return nil
}

// bucketOf names the bucket a Storable is kept in.
func bucketOf[T Storable]() string {
	var t T
	return string(t.Bucket())
}
