package pkg

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestClassifyCSVErrorsOnEmpty(t *testing.T) {
	buf := bytes.NewBufferString(``)

	_, err := classifyCSV(buf)
	assert.ErrorIs(t, err, EmptyFileClassifierError)
}

func TestClassifyCSVErrorsOnIrrelevant(t *testing.T) {
	buf := bytes.NewBufferString(`This,Is,Not,A,Valid,CSV
Neither,Is,This,Good,Data`)

	_, err := classifyCSV(buf)
	assert.ErrorIs(t, err, BankHeaderNotFoundClassifierError)
}

func TestClassifyCSVKiwibank(t *testing.T) {
	buf := bytes.NewBufferString(`Account number,Date,Memo/Description,Source Code (payment type),TP ref,TP part,TP code,OP ref,OP part,OP code,OP name,OP Bank Account Number,Amount (credit),Amount (debit),Amount,Balance
ExampleAccount, EXAMPLE VENDOR;,,,,,,,,,,,10.00,-10.00,00.00
`)

	bank, err := classifyCSV(buf)
	assert.Nil(t, err)
	assert.Equal(t, Kiwibank, bank)
}

func TestClassifyCSVKiwibankHeaderOnly(t *testing.T) {
	buf := bytes.NewBufferString(`Account number,Date,Memo/Description,Source Code (payment type),TP ref,TP part,TP code,OP ref,OP part,OP code,OP name,OP Bank Account Number,Amount (credit),Amount (debit),Amount,Balance`)

	bank, err := classifyCSV(buf)
	assert.Nil(t, err)
	assert.Equal(t, Kiwibank, bank)
}
