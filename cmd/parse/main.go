package main

import (
	"encoding/csv"
	"fmt"
	"github.com/TheQueenIsDead/budge/pkg/bank"
	"os"
	"time"
)

func main() {

	file, _ := os.Open("./export/38-9022-0224639-00_18Jun.CSV")
	r := csv.NewReader(file)
	records, err := r.ReadAll()
	if err != nil {
		panic(err)
	}

	for i, record := range records {

		// Skip header
		if i == 0 {
			continue
		}

		t, err := time.Parse("02-01-2006", record[1])
		if err != nil {
			panic(err)
		}

		exp := bank.KiwibankExportRow{
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
		fmt.Println(exp)
	}
}
