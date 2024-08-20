package pkg

import (
	"bufio"
	"encoding/csv"
	"github.com/labstack/echo/v4"
	"io"
	"os"
)

type Bank int

const (
	Kiwibank Bank = iota
)

func (b Bank) String() string {
	return [...]string{"Kiwibank"}[b]
}

var (
	BankHeaders = map[string]Bank{
		"Account number,Date,Memo/Description,Source Code (payment type),TP ref,TP part,TP code,OP ref,OP part,OP code,OP name,OP Bank Account Number,Amount (credit),Amount (debit),Amount,Balance": Kiwibank,
	}

	BankParsingStrategy = map[Bank]func(echo.Context, io.Reader) ([]Transaction, error){
		Kiwibank: parseKiwibankCSV,
	}
)

func classifyCSV(in io.Reader) (Bank, error) {

	s := bufio.NewScanner(in)
	s.Scan()
	header := s.Text()

	if header == "" {
		return -1, EmptyFileClassifierError
	}

	bank, ok := BankHeaders[header]
	if !ok {
		return -1, BankHeaderNotFoundClassifierError
	}

	return bank, nil
}

func ParseCSV(ctx echo.Context, filepath string) ([]Transaction, error) {
	file, _ := os.Open(filepath)
	defer file.Close()

	bank, err := classifyCSV(file)
	if err != nil {
		ctx.Logger().Error(err)
		return nil, err
	}

	parseFunc, ok := BankParsingStrategy[bank]
	if !ok {
		ctx.Logger().Error(NoBankParsingStrategyError)
		return nil, NoBankParsingStrategyError
	}

	return parseFunc(ctx, file)
}

func parseKiwibankCSV(ctx echo.Context, in io.Reader) (transactions []Transaction, err error) {

	r := csv.NewReader(in)

	// Skip Header
	//_, err = r.Read()
	//if err != nil {
	//	ctx.Logger().Error(err)
	//	return nil, err
	//}

	rows, err := r.ReadAll()
	if err != nil {
		ctx.Logger().Error(err)
		return nil, err
	}

	for _, row := range rows {
		kiwibank := KiwibankExportRow{
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
		tx, err := kiwibank.toTransaction()
		if err != nil {
			return nil, err
		}
		transactions = append(transactions, tx)
	}

	return transactions, nil
}
