package application

import (
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSeedHasRecurringPayments finds the recurring payments in the browser
// tests' data, as of their stopped clock, so the page and its pictures have
// one of each kind to show.
func TestSeedHasRecurringPayments(t *testing.T) {
	app := newTestApp(t)
	frozen := time.Date(2026, time.June, 15, 9, 0, 0, 0, time.UTC)
	require.NoError(t, test.Seed(t.Context(), app.store, frozen))
	txs, err := app.store.ReadTransactionsByDate(t.Context(), frozen.AddDate(0, -recurringHistoryMonths, 0), frozen)
	require.NoError(t, err)

	found := map[string]RecurringPayment{}
	for _, payment := range FindRecurring(spendingOnly(txs), frozen) {
		found[payment.Name] = payment
	}
	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}
	assert.ElementsMatch(t, []string{"Neon", "AA Insurance", "Mercury", "Spark", "CityFitness", "Lightbox"}, names,
		"and not the supermarket, the fuel or the cafe")

	neon := found["Neon"]
	assert.Equal(t, "monthly", neon.Cadence.Key)
	assert.True(t, neon.PriceChanged, "a subscription whose price just went up")
	assert.Equal(t, 19.99, neon.Usual)
	assert.True(t, neon.ComingUp)

	insurance := found["AA Insurance"]
	assert.Equal(t, "yearly", insurance.Cadence.Key)
	assert.True(t, insurance.DueSoon, "yearly insurance due within the week")
	assert.Equal(t, 4, insurance.DaysAhead)
	assert.False(t, insurance.New)

	assert.False(t, found["Mercury"].Fixed, "a power bill that varies")
	assert.False(t, found["Mercury"].New)
	assert.True(t, found["Spark"].Fixed)
	assert.False(t, found["Spark"].New)
	assert.True(t, found["CityFitness"].New, "a gym newly recurring")
	assert.Equal(t, "fortnightly", found["CityFitness"].Cadence.Key)
	assert.True(t, found["Lightbox"].Stopped, "a subscription cancelled months ago")
}
