package database

import (
	"encoding/json"
	"errors"
	bolt "go.etcd.io/bbolt"
)

// Create inserts a new Storable object and returns an error if the Key already exists.
func Create[T Storable](db *bolt.DB, item T) error {
	// TODO: Error if something already exists
	return db.Update(func(tx *bolt.Tx) error {
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
func Count[T Storable](db *bolt.DB) (int, error) {
	var count int
	var result T
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(result.Bucket())
		if b == nil {
			return errors.New("no account bucket")
		}
		count = b.Stats().KeyN
		return nil
	})
	return count, err
}

// Get retrieves a Storable of type T given an identifier. The identifier should correspond to the returned value
// of the Storable.Key() method.
func Get[T Storable](db *bolt.DB, id []byte) (T, error) {
	var result T
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(result.Bucket())
		buf := b.Get(id)
		return json.Unmarshal(buf, &result)
	})
	return result, err
}

// Read iterates the bucket for the given Storable and returns all models of type T.
func Read[T Storable](db *bolt.DB) ([]T, error) {
	var result []T
	t := *(new(T))
	err := db.View(func(tx *bolt.Tx) error {
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
func ReadFilter[T Storable](db *bolt.DB, filter func(T) bool) ([]T, error) {
	var result []T
	t := *(new(T))
	err := db.View(func(tx *bolt.Tx) error {
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

//func ReadById(db *bolt.DB, item Storable) (Storable, error) {
//	var result Storable
//	err := db.View(func(tx *bolt.Tx) error {
//		b := tx.Bucket(item.Bucket())
//		value := b.Get(item.Key())
//		value, err := json.Marshal(&item)
//		if err != nil {
//			return err
//		}
//		return b.Put(item.Key(), value)
//	})
//	return result, err
//}

// Update overwrites a pre-existing Storable object and returns an error if the Key does not exist.
func Update[T Storable](db *bolt.DB, item T) error {
	// TODO: Error if the item does not exist.
	return db.Update(func(tx *bolt.Tx) error {
		t := *(new(T))
		b := tx.Bucket(t.Bucket())
		value, err := json.Marshal(&item)
		if err != nil {
			return err
		}
		return b.Put(item.Key(), value)
	})
}

func Delete(item Storable) error {
	panic("not implemented")
}
