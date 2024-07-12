package pkg

import (
	"fmt"
	"gorm.io/gorm"
	"time"
)

type BudgetFrequency string

type TransactionType int

const (
	TransactionTypeDebit TransactionType = iota
	TransactionTypeCredit
)

const (
	Weekly  BudgetFrequency = "w"
	Monthly BudgetFrequency = "m"
	Yearly  BudgetFrequency = "y"
)

type Account struct {
	gorm.Model

	Number       string
	Transactions []Transaction
	Bank         Bank
}

type Transaction struct {
	gorm.Model
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
	gorm.Model

	// Description is the raw description of the merchant as parsed directly from a CSV
	Description string
	// Name is the display / friendly name for the merchant.
	// For example, if "Pak N Save Wainoni Wainoni ;" is the description, then this will be "Pak N Save"
	Name     string
	Category string
	Account  string
}

type BudgetItem struct {
	gorm.Model

	Name      string          `goorm:"name"`
	Cost      float64         `goorm:"cost"`
	Frequency BudgetFrequency `goorm:"frequency"`
	Account   string          `goorm:"account"`
}
