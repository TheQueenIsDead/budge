package pkg

import (
	"fmt"
	"time"
)


type TransactionType int

const (
	TransactionTypeDebit TransactionType = iota
	TransactionTypeCredit
)

type Account struct {
	Number       string
	Transactions []Transaction
	Bank         Bank
}

type Transaction struct {
	AccountID uint
	Account   Account

	Date      time.Time
	Merchant  string
	Value     uint32
	Precision uint8
	Type      TransactionType
}

func (t *Transaction) String() string {
	return fmt.Sprintf("$%.2f", float64(t.Value)/float64(t.Precision))
}

type Merchant struct {

	// Description is the raw description of the merchant as parsed directly from a CSV
	Description string
	// Name is the display / friendly name for the merchant.
	// For example, if "Pak N Save Wainoni Wainoni ;" is the description, then this will be "Pak N Save"
	Name     string
	Category string
	Account  string
}
