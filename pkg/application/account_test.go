package application

import (
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestWalkAccount(t *testing.T) {
	tests := []struct {
		name         string
		balance      float64
		first        time.Time
		last         time.Time
		transactions map[string][]float64
		expected     map[string]float64
	}{

		{"simple",
			99,
			time.Now().AddDate(0, 0, -3),
			time.Now(),
			map[string][]float64{
				time.Now().Format(time.DateOnly):                   {33},
				time.Now().AddDate(0, 0, -1).Format(time.DateOnly): {33},
				time.Now().AddDate(0, 0, -2).Format(time.DateOnly): {33},
			},
			map[string]float64{
				time.Now().Format(time.DateOnly):                   99,
				time.Now().AddDate(0, 0, -1).Format(time.DateOnly): 66,
				time.Now().AddDate(0, 0, -2).Format(time.DateOnly): 33,
				time.Now().AddDate(0, 0, -3).Format(time.DateOnly): 33,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			balances := WalkAccount(test.balance, test.first, test.last, test.transactions)
			assert.Equal(t, test.expected, balances)
		})
	}
}
