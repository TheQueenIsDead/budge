package models

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestTransaction_Float(t *testing.T) {
	testCases := []struct {
		Value     uint32
		Precision uint8
		Expected  float64
	}{
		{0, 100, 0.00},
		{1, 100, 0.01},
		{10, 100, 0.10},
		{100, 100, 1.00},
		{1000, 100, 10.00},
		{10000, 100, 100.00},
		{100000, 100, 1000.00},
		{42069, 100, 420.69},
		{123456789, 100, 1234567.89},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%f", tc.Expected), func(t *testing.T) {
			tx := Transaction{
				Value:     tc.Value,
				Precision: tc.Precision,
			}
			assert.Equal(t, tc.Expected, tx.Float())
		})
	}
}

func TestTransaction_Add(t *testing.T) {
	testCases := []struct {
		A        Transaction
		B        Transaction
		Expected float64
	}{
		{Transaction{Value: 0, Precision: 100}, Transaction{Value: 0, Precision: 100}, 0.00},
		{Transaction{Value: 10000, Precision: 100}, Transaction{Value: 0, Precision: 100}, 100.00},
		{Transaction{Value: 0, Precision: 100}, Transaction{Value: 10000, Precision: 100}, 100.00},
		{Transaction{Value: 1234, Precision: 100}, Transaction{Value: 5678, Precision: 100}, 69.12},
		{Transaction{Value: 1000000000, Precision: 100}, Transaction{Value: 1000000000, Precision: 100}, 20000000.00},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%f", tc.Expected), func(t *testing.T) {
			assert.Equal(t, tc.Expected, tc.A.Add(&tc.B))
		})
	}
}
