package application

import (
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
)

func TestFilterRecentTransactionsCountsWholeDays(t *testing.T) {
	// Late in the day, so a window counted to the minute would leave out the
	// morning of its first day.
	now := time.Date(2026, time.November, 4, 18, 0, 0, 0, time.UTC)
	at := func(month time.Month, day, hour int) models.Transaction {
		return models.Transaction{Date: time.Date(2026, month, day, hour, 0, 0, 0, time.UTC)}
	}
	firstMorning := at(time.October, 6, 9)
	dayBefore := at(time.October, 5, 23)
	pastFirst := at(time.September, 6, 1)
	tooOld := at(time.September, 5, 23)

	past, recent := FilterRecentTransactions([]models.Transaction{firstMorning, dayBefore, pastFirst, tooOld}, now)
	assert.Equal(t, []models.Transaction{firstMorning}, recent, "the whole of the first of the 30 days, today among them")
	assert.Equal(t, []models.Transaction{dayBefore, pastFirst}, past, "and the 30 whole days before")
}

func TestBuildCardsSavingsDeltaFromALoss(t *testing.T) {
	var account models.Account
	account.Balance.Current = 1000
	amounts := func(amounts ...float64) []models.Transaction {
		var txs []models.Transaction
		for _, amount := range amounts {
			txs = append(txs, models.Transaction{Amount: amount})
		}
		return txs
	}
	// $500 short last time, $500 saved this time.
	_, _, _, savings := BuildCards([]models.Account{account}, amounts(1000, -1500), amounts(1500, -1000))
	assert.Equal(t, 500.0, savings.Total)
	assert.Equal(t, -500.0, savings.PreviousTotal)
	assert.InDelta(t, 2.0, savings.Delta, 0.0001, "up by twice the loss, not down")
}
