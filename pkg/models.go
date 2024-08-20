package pkg

import (
	"time"
)

type Account struct {
	Number       string
	Transactions []Transaction
	Bank         Bank
}

type Transaction struct {
	AccountID uint
	Account   Account

	Date     time.Time
	Merchant string
	Value    string
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

// CsvImportRow is a struct based on a Kiwibank CSV export folder.
// It includes all the data from the imported CSV file, and later gets persisted as a Transaction.
// As new banks are added, their own CSV exports will need to conform to this import struct.
type CsvImportRow struct {
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
	Bank                      Bank
}
