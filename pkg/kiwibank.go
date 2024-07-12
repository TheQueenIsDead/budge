package pkg

import (
	"strconv"
	"strings"
	"time"
)

type KiwibankExportRow struct {
	AccountNumber         string
	Date                  string
	MemoDescription       string
	SourceCodePaymentType string
	TPref                 string
	TPpart                string
	TPcode                string
	OPref                 string
	OPpart                string
	OPcode                string
	OPname                string
	OPBankAccountNumber   string
	AmountCredit          string
	AmountDebit           string
	Amount                string
	Balance               string
}

func (k *KiwibankExportRow) toTransaction() (tx Transaction, err error) {

	tx = Transaction{
		//Model:     gorm.Model{},
		//AccountID: 0,
		Account: Account{
			Number: k.AccountNumber,
			Bank:   Kiwibank,
		},
		//Date:      time.Time{},
		Merchant:  k.MemoDescription,
		Value:     0,
		Precision: 100,
		//Type:      false,
	}

	// Parse time
	tx.Date, err = time.Parse("02-01-2006", k.Date)
	if err != nil {
		return
	}

	// Parse value
	if len(k.Amount) > 0 && k.Amount[0] != '-' {
		tx.Type = TransactionTypeDebit
	} else {
		tx.Type = TransactionTypeCredit
		k.Amount = strings.ReplaceAll(k.Amount, "-", "")
	}

	value := strings.ReplaceAll(k.Amount, ".", "")
	intValue, err := strconv.ParseUint(value, 10, 32)
	tx.Value = uint32(intValue)

	return tx, nil
}
