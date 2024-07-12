package pkg

import (
	"bufio"
	"encoding/csv"
	"io"
	"os"
	"time"
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

	BankParsingStrategy = map[Bank]func(io.Reader) ([]CsvImportRow, error){
		Kiwibank: parseKiwibankCSV,
	}
)

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

func ParseCSV(filepath string) ([]CsvImportRow, error) {
	file, _ := os.Open(filepath)
	defer file.Close()

	bank, err := classifyCSV(file)
	if err != nil {
		return nil, err
	}

	parseFunc, ok := BankParsingStrategy[bank]
	if !ok {
		return nil, NoBankParsingStrategyError
	}

	return parseFunc(file)
}

// parseKiwibankCSV reads a CSV file of ransactions expoerted from a Kiwibank account via the "Full CSV" export.
// It parses and validates the fields, and returns an array of Transaction.
// The record fields are as such:
// [0]  Account number
// [1]  Date
// [2]  Memo/Description
// [3]  Source Code (payment type)
// [4]  TP ref
// [5]  TP part
// [6]  TP code
// [7]  OP ref
// [8]  OP part
// [9]  OP code
// [10] OP name
// [11] OP Bank Account Number
// [12] Amount (credit)
// [13] Amount (debit)
// [14] Amount
// [15] Balance
func parseKiwibankCSV(in io.Reader) ([]CsvImportRow, error) {

	r := csv.NewReader(in)
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}

	var transactions []CsvImportRow
	for i, record := range records {

		// Skip header
		if i == 0 {
			continue
		}

		t, err := time.Parse("02-01-2006", record[1])
		if err != nil {
			return nil, err
		}

		exp := CsvImportRow{
			AccountNumber:             record[0],
			Date:                      t,
			Description:               record[2],
			Source:                    record[3],
			Code:                      record[4],
			TPref:                     record[5],
			TPpart:                    record[6],
			TPcode:                    record[7],
			OPref:                     record[8],
			OPpart:                    record[9],
			OPcode:                    record[10],
			OPname:                    record[11],
			OPBankAccountNumberAmount: record[12],
			Amount:                    record[13],
			AmountBalance:             record[14],
		}
		transactions = append(transactions, exp)
	}

	return transactions, nil
}
