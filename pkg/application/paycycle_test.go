package application

import (
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pays(t *testing.T, days ...string) {
	t.Helper()
	var dates []time.Time
	for _, d := range days {
		date, err := time.Parse(time.DateOnly, d)
		require.NoError(t, err)
		dates = append(dates, date.Add(11*time.Hour))
	}
	SetPayDates(dates)
	t.Cleanup(func() { payCycle.Store(nil) })
}

func payday(s string) time.Time {
	d, _ := time.Parse(time.DateOnly, s)
	return d.Add(15 * time.Hour)
}

func TestPayDates(t *testing.T) {
	var txs []models.Transaction
	for _, d := range []string{"2026-07-28", "2026-08-28", "2026-09-28"} {
		pay := spendTransaction("", "", 6195.25, payday(d))
		pay.Type = "CREDIT"
		pay.Description = "Linc-Technologies Li Linc-Technol Ipayroll"
		txs = append(txs, pay)
	}
	bonus := spendTransaction("", "", 2000, payday("2026-08-10"))
	bonus.Type = "CREDIT"
	bonus.Description = "One off"
	own := spendTransaction("Transfer", "Transfer", 1750, payday("2026-08-28"))
	own.Type = "TRANSFER"
	txs = append(txs, bonus, own)

	dates := payDates(txs)
	require.Len(t, dates, 3, "the payer seen every month is pay; a one off and a transfer are not")
	assert.Equal(t, 28, dates[0].Day())
	assert.Nil(t, payDates(txs[3:]), "nothing regular enough to call pay")
}

func TestSpendPeriodsRunPaydayToPayday(t *testing.T) {
	// Pay moved forward to the Friday when the 28th fell on a weekend.
	pays(t, "2026-07-28", "2026-08-28", "2026-09-25")

	assert.Equal(t, payday("2026-08-28").Truncate(24*time.Hour), spendPeriodStart(payday("2026-09-10"), CadenceMonthly),
		"the 10th of September is spending from August's pay")
	assert.Equal(t, payday("2026-09-25").Truncate(24*time.Hour), spendPeriodStart(payday("2026-09-25"), CadenceMonthly))
	assert.Equal(t, payday("2026-09-25").Truncate(24*time.Hour), spendPeriodStart(payday("2026-10-19"), CadenceMonthly),
		"after the last pay, the cycle runs on from it")
	assert.Equal(t, payday("2026-10-25").Truncate(24*time.Hour), spendPeriodStart(payday("2026-10-26"), CadenceMonthly),
		"and the next pay is taken to fall on the same day")
	assert.Equal(t, payday("2026-06-28").Truncate(24*time.Hour), spendPeriodStart(payday("2026-07-01"), CadenceMonthly),
		"before the first known pay, likewise")

	start := spendPeriodStart(payday("2026-10-19"), CadenceMonthly)
	assert.Equal(t, payday("2026-08-28").Truncate(24*time.Hour), spendPeriodAdd(start, CadenceMonthly, -1))
	assert.Equal(t, payday("2026-07-28").Truncate(24*time.Hour), spendPeriodAdd(start, CadenceMonthly, -2))
	assert.Equal(t, payday("2026-06-28").Truncate(24*time.Hour), spendPeriodAdd(start, CadenceMonthly, -3))
	assert.Equal(t, payday("2026-10-25").Truncate(24*time.Hour), spendPeriodAdd(start, CadenceMonthly, 1))

	assert.Equal(t, "25 Sep", spendPeriodLabel(start, CadenceMonthly))
	weekly := spendPeriodStart(payday("2026-10-21"), CadenceWeekly)
	assert.Equal(t, time.Monday, weekly.Weekday(), "weeks are untouched")
}

func TestSpendBucketsFollowPay(t *testing.T) {
	pays(t, "2026-07-28", "2026-08-28", "2026-09-28")
	group, _ := SpendGroupByKey("groceries")
	txs := []models.Transaction{
		spendTransaction("Supermarkets and grocery stores", "PAK'nSAVE", -100, payday("2026-09-27")),
		spendTransaction("Supermarkets and grocery stores", "PAK'nSAVE", -40, payday("2026-09-29")),
	}
	buckets := BuildSpendBuckets(txs, group, CadenceMonthly, 2, payday("2026-10-05"))
	require.Len(t, buckets, 2)
	assert.Equal(t, "28 Aug", buckets[0].Label)
	assert.Equal(t, 100.0, buckets[0].Total, "the day before pay belongs to the cycle before")
	assert.Equal(t, 40.0, buckets[1].Total)
}

func TestSpendPeriodsWithoutPay(t *testing.T) {
	payCycle.Store(nil)
	assert.Equal(t, 1, spendPeriodStart(payday("2026-09-10"), CadenceMonthly).Day(), "calendar months without pay to go by")
}
