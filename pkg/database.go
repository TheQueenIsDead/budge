package pkg

import (
	"encoding/json"
	bolt "go.etcd.io/bbolt"
)

var (
	AccountBucket     = []byte("accounts")
	MerchantBucket    = []byte("merchants")
	TransactionBucket = []byte("transactions")
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

func PutMerchant(db *bolt.DB, merchant *Merchant) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(AccountBucket)
		buf, err := json.Marshal(merchant)
		if err != nil {
			return err
		}
		err = b.Put([]byte(merchant.Name), buf)
		if err != nil {
			return err
		}
		return nil
	})
}

func ListMerchants(db *bolt.DB) []Merchant {

	var merchants []Merchant

	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(MerchantBucket)
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
		b := tx.Bucket(AccountBucket)
		err := b.ForEach(func(k, v []byte) error {
			var a Account
			err := json.Unmarshal(v, &a)
			if err != nil {
				return err
			}
			transactions = append(transactions, a.Transactions...)
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

func Import(db *bolt.DB, account *Account, merchants []Merchant, transactions []Transaction) error {
	tx, err := db.Begin(true)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Import Account
	b := tx.Bucket(AccountBucket)

	buf, err := json.Marshal(account)
	if err != nil {
		return err
	}
	err = b.Put([]byte(account.Number), buf)
	if err != nil {
		return err
	}

	// Import Merchants
	b = tx.Bucket(MerchantBucket)
	for _, m := range merchants {
		buf, err = json.Marshal(m)
		if err != nil {
			return err
		}
		err = b.Put([]byte(m.Description), buf)
		if err != nil {
			return err
		}
	}

	// Import Transactions
	b = tx.Bucket(TransactionBucket)
	for _, t := range transactions {
		buf, err = json.Marshal(t)
		if err != nil {
			return err
		}
		hash := HashModel(t)
		err = b.Put(hash[:], buf)
		if err != nil {
			return err
		}
	}

	// Commit the transaction and check for error.
	if err := tx.Commit(); err != nil {
		return err
	}

	return err
}

func ImportMerchants(db *bolt.DB, merchants []Merchant) error {
	tx, err := db.Begin(true)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	b := tx.Bucket(MerchantBucket)

	for _, m := range merchants {
		buf, err := json.Marshal(m)
		if err != nil {
			return err
		}
		key := HashModel(buf)
		err = b.Put(key[:], buf)
		if err != nil {
			return err
		}
	}

	// Commit the transaction and check for error.
	if err := tx.Commit(); err != nil {
		return err
	}

	return err
}
