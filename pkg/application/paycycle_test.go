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

// ameliePays is a real fortnightly pattern: the payroll renamed itself in
// March, and several pays fall under a thousand dollars.
func ameliePays() []models.Transaction {
	raw := []struct {
		day, payer string
		amount     float64
	}{
		{"2026-10-07", "R J Hill Laborat", 1526}, {"2026-09-23", "R J Hill Laborat", 1621.54}, {"2026-09-09", "R J Hill Laborat", 1526},
		{"2026-08-26", "R J Hill Laborat", 1526}, {"2026-08-12", "R J Hill Laborat", 1303.09}, {"2026-07-29", "R J Hill Laborat", 978.17},
		{"2026-07-15", "R J Hill Laborat", 1006.07}, {"2026-07-01", "R J Hill Laborat", 1011.03}, {"2026-06-17", "R J Hill Laborat", 1005.88},
		{"2026-06-03", "R J Hill Laborat", 1094.01}, {"2026-05-20", "R J Hill Laborat", 959.55}, {"2026-05-06", "R J Hill Laborat", 1116.53},
		{"2026-04-22", "R J Hill Laborat", 1254.21}, {"2026-04-08", "R J Hill Laborat", 1189.72}, {"2026-03-25", "R J Hill Laborat", 1110.46},
		{"2026-03-11", "Rj Hill Lab Ltd", 1081.27}, {"2026-02-25", "Rj Hill Lab Ltd", 948.32}, {"2026-02-11", "Rj Hill Lab Ltd", 1138.89},
		{"2026-01-28", "Rj Hill Lab Ltd", 1340.64}, {"2026-01-14", "Rj Hill Lab Ltd", 1008.58}, {"2025-12-31", "Rj Hill Lab Ltd", 1053.81},
		{"2025-12-17", "Rj Hill Lab Ltd", 1155.70}, {"2025-12-03", "Rj Hill Lab Ltd", 919.82}, {"2025-11-19", "Rj Hill Lab Ltd", 914.40},
		{"2025-11-05", "Rj Hill Lab Ltd", 1049.43}, {"2025-10-22", "Rj Hill Lab Ltd", 914.38},
	}
	var txs []models.Transaction
	for _, r := range raw {
		pay := spendTransaction("", "", r.amount, payday(r.day).Add(-14*time.Hour)) // 1am UTC: the same day here and in New Zealand
		pay.Type, pay.Description = "CREDIT", r.payer
		txs = append(txs, pay)
	}
	return txs
}

func TestPayDatesThroughARenameAndSmallPays(t *testing.T) {
	dates := payDates(ameliePays())
	assert.Len(t, dates, 26, "every pay, under a thousand or under the old name")
}

func TestSpendBucketsStepOntoKnownPaydays(t *testing.T) {
	inNZ(t)
	SetPayDates(payDates(ameliePays()[:15])) // known only from 25 Mar
	t.Cleanup(func() { payCycle.Store(nil) })

	group, _ := SpendGroupByKey("groceries")
	shop := spendTransaction("Supermarkets and grocery stores", "PAK'nSAVE", -214.06, payday("2026-10-12"))
	buckets := BuildSpendBuckets([]models.Transaction{shop}, group, CadenceMonthly, 12, payday("2026-10-20"))

	labels := make([]string, len(buckets))
	for i, b := range buckets {
		labels[i] = b.Label
	}
	assert.Equal(t, []string{"25 Nov", "25 Dec", "25 Jan", "25 Feb", "25 Mar", "8 Apr", "6 May", "3 Jun", "1 Jul", "12 Aug", "9 Sep", "7 Oct"}, labels,
		"estimated months before the first known pay, then the real first pay of each month")
	assert.Equal(t, 214.06, buckets[11].Total, "spending lands in the month it was paid from")
}
