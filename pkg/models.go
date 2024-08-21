package pkg

import (
	"bytes"
	"crypto/md5"
	"encoding/gob"
	"fmt"
	"time"
)

type TransactionType int

const (
	TransactionTypeDebit TransactionType = iota
	TransactionTypeCredit
)

type Account struct {
	Bank         Bank
	Number       string
	Transactions []Transaction
}

type Transaction struct {
	Date      time.Time
	Merchant  string
	Precision uint8
	Type      TransactionType
	Value     uint32
}

func (t *Transaction) String() string {
	return fmt.Sprintf("$%.2f", float64(t.Value)/float64(t.Precision))
}

type Merchant struct {
	// Description is the raw description of the merchant as parsed directly from a CSV
	Description string
	// Name is the display / friendly name for the merchant.
	// For example, if "Pak N Save Wainoni Wainoni ;" is the description, then this will be "Pak N Save"
	Name string
	// TODO: Add the ability to set categories for a merchant
	Category string
	// If the merchant was not a POS payment, and was a bank transfer, then we will have a receiving bank account number.
	Account string
}

func HashModel(m any) [16]byte {
	var b bytes.Buffer
	gob.NewEncoder(&b).Encode(m)
	return md5.Sum(b.Bytes())
	//return hex.EncodeToString(md[:])
}
