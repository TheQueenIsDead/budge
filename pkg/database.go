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

func ListMerchants(db *bolt.DB) []Merchant {

	var merchants []Merchant

	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(AccountBucket)
		err := b.ForEach(func(k, v []byte) error {
			var m Merchant
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
		return nil
	}

	return merchants
}

func ListAccounts(db *bolt.DB) []Account {

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

func ListTransactions(db *bolt.DB) []Transaction {

	var transactions []Transaction

	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(MerchantBucket)
		err := b.ForEach(func(k, v []byte) error {
			var m Merchant
			err := json.Unmarshal(v, &m)
			if err != nil {
				return err
			}
			transactions = append(transactions, m.Transactions...)
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

	return transactions
}
