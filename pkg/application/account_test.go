package application

import (
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestWalkAccount(t *testing.T) {
	tests := []struct {
		name     string
		balance  float64
		deltas   map[string]float64
		expected map[string]float64
	}{

		{"simple",
			99,
			map[string]float64{
				"2023-03": 33,
				"2023-02": 33,
				"2023-01": 33,
				"2022-12": 0,
			},
			map[string]float64{
				"2023-03": 99,
				"2023-02": 66,
				"2023-01": 33,
				"2022-12": 0,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			balances := WalkAccount(test.balance, test.deltas)
			assert.Equal(t, test.expected, balances)
		})
	}
}
