package pkg

import (
	"gorm.io/gorm"
	"time"
)

type BudgetFrequency string

const (
	Weekly  BudgetFrequency = "w"
	Monthly BudgetFrequency = "m"
	Yearly  BudgetFrequency = "y"
)

type Merchant struct {
	gorm.Model

	// Description is the raw description of the merchant as parsed directly from a CSV
	Description string
	// Name is the display / friendly name for the merchant.
	// For example, if "Pak N Save Wainoni Wainoni ;" is the description, then this will be "Pak N Save"
	Name     string
	Category string
}

type BudgetItem struct {
	gorm.Model

	Name      string          `goorm:"name"`
	Cost      float64         `goorm:"cost"`
	Frequency BudgetFrequency `goorm:"frequency"`
	Account   string          `goorm:"account"`
}

type KiwibankExportRow struct {
	AccountNumber             string
	Date                      time.Time
	Description               string
	Source                    string
	Code                      string // (payment type)
	TPref                     string
	TPpart                    string
	TPcode                    string
	OPref                     string
	OPpart                    string
	OPcode                    string
	OPname                    string
	OPBankAccountNumberAmount string // (credit)
	Amount                    string // (debit)
	AmountBalance             string
}
