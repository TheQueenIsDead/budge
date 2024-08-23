package database

import (
	"encoding/csv"
	"github.com/TheQueenIsDead/budge/pkg/bank"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"github.com/scylladb/go-set/strset"
	"os"
	"strings"
)

var (
	BankCsvHeaders = map[string]models.Bank{
		"Account number,Date,Memo/Description,Source Code (payment type),TP ref,TP part,TP code,OP ref,OP part,OP code,OP name,OP Bank Account Number,Amount (credit),Amount (debit),Amount,Balance": models.Kiwibank,
	}

	BankParsingStrategy = map[models.Bank]func(echo.Context, *csv.Reader) (*models.Account, []models.Merchant, []models.Transaction, error){
		models.Kiwibank: parseKiwibankCSV,
	}
)

func classifyCSV(header []string) (models.Bank, error) {

	joinedHeader := strings.Join(header, ",")
	bank, ok := BankCsvHeaders[joinedHeader]
	if !ok {
		return -1, BankHeaderNotFoundClassifierError
	}

	return bank, nil
}

func ParseCSV(ctx echo.Context, filepath string) (*models.Account, []models.Merchant, []models.Transaction, error) {
	file, _ := os.Open(filepath)
	defer file.Close()

	r := csv.NewReader(file)
	header, err := r.Read()

	bank, err := classifyCSV(header)
	if err != nil {
		ctx.Logger().Error(err)
		return nil, nil, nil, err
	}

	parseFunc, ok := BankParsingStrategy[bank]
	if !ok {
		ctx.Logger().Error(NoBankParsingStrategyError)
		return nil, nil, nil, NoBankParsingStrategyError
	}

	return parseFunc(ctx, r)
}

func parseKiwibankCSV(ctx echo.Context, r *csv.Reader) (*models.Account, []models.Merchant, []models.Transaction, error) {

	var account = models.Account{
		Bank: models.Kiwibank,
	}
	var merchants []models.Merchant
	merchantSet := strset.New()
	var transactions []models.Transaction

	rows, err := r.ReadAll()
	if err != nil {
		ctx.Logger().Error(err)
		return nil, nil, nil, err
	}
	for _, row := range rows {
		kiwibank := bank.KiwibankExportRow{
			AccountNumber:         row[0],
			Date:                  row[1],
			MemoDescription:       row[2],
			SourceCodePaymentType: row[3],
			TPref:                 row[4],
			TPpart:                row[5],
			TPcode:                row[6],
			OPref:                 row[7],
			OPpart:                row[8],
			OPcode:                row[9],
			OPname:                row[10],
			OPBankAccountNumber:   row[11],
			AmountCredit:          row[12],
			AmountDebit:           row[13],
			Amount:                row[14],
			Balance:               row[15],
		}

		// TODO: reimplement
		//tx, err := kiwibank.toTransaction()
		//if err != nil {
		//	return nil, nil, nil, err
		//}

		// Add the transaction to the return list
		//transactions = append(transactions, tx)

		// Build up an array of unique merchants
		if !merchantSet.Has(kiwibank.MerchantName()) {
			merchantSet.Add(kiwibank.MerchantName())
			merchants = append(merchants, models.Merchant{
				Description: kiwibank.MerchantName(),
				Name:        "",
				Category:    "",
				Account:     kiwibank.OPBankAccountNumber,
			})
		}

		// Adjust the account number now that we're iterating fields and able to determine it
		account.Number = kiwibank.AccountNumber
	}

	// Tidy up the final attributes on the account
	account.Transactions = transactions

	return &account, merchants, transactions, nil
}
