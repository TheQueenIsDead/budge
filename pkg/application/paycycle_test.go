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
		dates = append(dates, date.Add(1*time.Hour)) // the same day in UTC and in New Zealand
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

func TestFortnightlyPayMakesMonths(t *testing.T) {
	// Paid every second Wednesday: 1 Jul has three pays (1, 15, 29), the
	// others two.
	pays(t, "2026-07-01", "2026-07-15", "2026-07-29", "2026-08-12", "2026-08-26", "2026-09-09", "2026-09-23", "2026-10-07")

	starts := []string{}
	at := spendPeriodStart(payday("2026-10-19"), CadenceMonthly)
	for i := 0; i < 4; i++ {
		starts = append([]string{at.Format(time.DateOnly)}, starts...)
		at = spendPeriodAdd(at, CadenceMonthly, -1)
	}
	assert.Equal(t, []string{"2026-07-01", "2026-08-12", "2026-09-09", "2026-10-07"}, starts,
		"each month starts at its first pay, not every pay")
	assert.Equal(t, "2026-08-12", spendPeriodStart(payday("2026-09-08"), CadenceMonthly).Format(time.DateOnly),
		"a day before September's first pay belongs to August's month")
}

func TestWeeklyPayMakesMonths(t *testing.T) {
	var days []string
	for d := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC); d.Before(time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)); d = d.AddDate(0, 0, 7) {
		days = append(days, d.Format(time.DateOnly))
	}
	pays(t, days...)
	assert.Equal(t, "2026-10-01", spendPeriodStart(payday("2026-10-19"), CadenceMonthly).Format(time.DateOnly))
	assert.Equal(t, "2026-09-03", spendPeriodStart(payday("2026-09-30"), CadenceMonthly).Format(time.DateOnly))
}

func TestMonthlyPayUnchanged(t *testing.T) {
	pays(t, "2026-07-28", "2026-08-28", "2026-09-25")
	assert.Equal(t, "2026-09-25", spendPeriodStart(payday("2026-10-19"), CadenceMonthly).Format(time.DateOnly))
}
