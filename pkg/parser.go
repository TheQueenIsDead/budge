package pkg

import (
	"encoding/csv"
	"os"
	"time"
)

type Parser interface {
	ParseCSV(path string) []KiwibankExportRow
}

type KiwibankParser struct {
}

func (parser KiwibankParser) ParseCSV(path string) ([]KiwibankExportRow, error) {

	file, _ := os.Open(path)
	r := csv.NewReader(file)
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}

	var transactions []KiwibankExportRow
	for i, record := range records {

		// Skip header
		if i == 0 {
			continue
		}

		t, err := time.Parse("02-01-2006", record[1])
		if err != nil {
			return nil, err
		}

		exp := KiwibankExportRow{
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
