package application

import (
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func Date(s string) time.Time {
	date, _ := time.Parse(time.DateOnly, s)
	return date
}

func TestFindTransactionRange(t *testing.T) {
	tests := []struct {
		name         string
		transactions []models.Transaction
		first        models.Transaction
		last         models.Transaction
	}{
		{"empty",
			[]models.Transaction{},
			models.Transaction{},
			models.Transaction{},
		},
		{"same",
			[]models.Transaction{
				{Date: time.Now()},
				{Date: time.Now()},
			},
			models.Transaction{Date: time.Now()},
			models.Transaction{Date: time.Now()},
		},
		{"three",
			[]models.Transaction{
				{Date: Date("2000-01-01")},
				{Date: Date("2000-01-02")},
				{Date: Date("2000-01-03")},
			},
			models.Transaction{Date: Date("2000-01-01")},
			models.Transaction{Date: Date("2000-01-03")},
		},
		{"three_reverse",
			[]models.Transaction{
				{Date: Date("2000-01-03")},
				{Date: Date("2000-01-02")},
				{Date: Date("2000-01-01")},
			},
			models.Transaction{Date: Date("2000-01-01")},
			models.Transaction{Date: Date("2000-01-03")},
		},
		{"cross_year",
			[]models.Transaction{
				{Date: Date("1000-01-01")},
				{Date: Date("2000-01-01")},
				{Date: Date("3000-01-01")},
			},
			models.Transaction{Date: Date("1000-01-01")},
			models.Transaction{Date: Date("3000-01-01")},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			first, last := FindTransactionRange(test.transactions)
			assert.Equal(t, test.first.Date, first.Date)
			assert.Equal(t, test.last.Date, last.Date)
		})
	}
}
