package integrations

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestSanitiseRemovesNoise(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{"Autopay", "Ap#19841451 To F M Surname", "F M Surname"},
		{"ATM", "Atm 20 Marshland", "20 Marshland"},
		{"Bill Payment", "Bill Payment Bicycle First Middle Last", "Bicycle First Middle Last"},
		{"Transfer", "Transfer From D J King - 02", "D J King 02"},
		{"Direct Debit", "Direct Debit - ACME Internet Limited", "ACME Internet Limited"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := sanitise(tc.text)
			assert.Equal(t, tc.expected, actual)
		})
	}
}
