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

type BudgetItem struct {
	gorm.Model

	Name      string          `goorm:"name"`
	Cost      float64         `goorm:"cost"`
	Frequency BudgetFrequency `goorm:"frequency"`
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
