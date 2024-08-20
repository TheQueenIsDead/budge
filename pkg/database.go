package pkg

import (
	"encoding/json"
	bolt "go.etcd.io/bbolt"
)

var (
	AccountBucket  = []byte("accounts")
	MerchantBucket = []byte("merchants")
)

func GetAccounts(db *bolt.DB) []Account {

	var accounts []Account

	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(AccountBucket)
		err := b.ForEach(func(k, v []byte) error {
			var a Account
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
		return nil
	}

	return accounts
}
