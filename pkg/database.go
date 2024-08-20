package pkg

import (
	"encoding/json"
	bolt "go.etcd.io/bbolt"
)

var (
	AccountBucket  = []byte("accounts")
	MerchantBucket = []byte("merchants")
)

func PutAccount(db *bolt.DB, account *Account) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(AccountBucket)
		buf, err := json.Marshal(account)
		if err != nil {
			return err
		}
		err = b.Put([]byte(account.Number), buf)
		if err != nil {
			return err
		}
		return nil
	})
}

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
