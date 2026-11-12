package application

import (
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRollingBounds(t *testing.T) {
	// Sunday 1 November 2026, mid afternoon.
	now := time.Date(2026, time.November, 1, 15, 0, 0, 0, time.UTC)
	tests := []struct {
		frequency string
		start     time.Time
		bars      int
		span      string
	}{
		{"weekly", time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), 7, "the last 7 days"},
		{"fortnightly", time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC), 14, "the last 14 days"},
		{"monthly", time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), 31, "the last month"},
		{"quarterly", time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), 14, "the last 3 months"},
		{"yearly", time.Date(2025, 11, 2, 0, 0, 0, 0, time.UTC), 13, "the last 12 months"}, // the rest of November, then by month to today
	}
	for _, tt := range tests {
		t.Run(tt.frequency, func(t *testing.T) {
			start, end, step, _, span := rollingBounds(tt.frequency, now)
			assert.Equal(t, tt.start, start)
			assert.Equal(t, time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC), end, "it runs to the end of today")
			assert.Equal(t, tt.span, span)
			bars := 0
			for at := start; at.Before(end); at = step(at) {
				bars++
			}
			assert.Equal(t, tt.bars, bars)
		})
	}
}

// TestYearlyBarsMissNoMonth steps a year that starts late in a month. Adding
// a month to 30 January overflows to 2 March, which left February out.
func TestYearlyBarsMissNoMonth(t *testing.T) {
	now := time.Date(2027, time.January, 29, 15, 0, 0, 0, time.UTC)
	start, end, step, format, _ := rollingBounds("yearly", now)
	require.Equal(t, time.Date(2026, time.January, 30, 0, 0, 0, 0, time.UTC), start)

	var labels []string
	var bounds []time.Time
	for at := start; at.Before(end); at = step(at) {
		bounds = append(bounds, at)
		labels = append(labels, at.Format(format))
	}
	assert.Equal(t, []string{
		"Jan 26", "Feb 26", "Mar 26", "Apr 26", "May 26", "Jun 26", "Jul 26",
		"Aug 26", "Sep 26", "Oct 26", "Nov 26", "Dec 26", "Jan 27",
	}, labels, "the first bar is the end of a month, the last the start of one, and every month between is whole")
	assert.Equal(t, time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC), bounds[1])
}

func TestBuildPeriodCharts(t *testing.T) {
	// The first day of a calendar fortnight, the morning after a repayment.
	now := time.Date(2026, time.November, 2, 9, 0, 0, 0, time.UTC)
	targets := map[string]models.BudgetItem{
		"groceries":  {BudgetCategory: "groceries", Amount: 140, Frequency: "weekly"},
		"eating-out": {BudgetCategory: "eating-out", Amount: 70, Frequency: ""},
		"home":       {BudgetCategory: "home", Amount: 1284, Frequency: "fortnightly"},
		"shopping":   {BudgetCategory: "shopping", Amount: 0, Frequency: "yearly"},
	}
	at := func(month time.Month, day int) time.Time { return time.Date(2026, month, day, 12, 0, 0, 0, time.UTC) }
	transactions := []models.Transaction{
		spendTx("thu", "EFTPOS", "Food", "Supermarkets and grocery stores", "", "", -50, at(time.October, 29)),
		spendTx("sat", "EFTPOS", "Lifestyle", "Cafes and restaurants", "", "", -20, at(time.October, 31)),
		spendTx("today", "EFTPOS", "Food", "Supermarkets and grocery stores", "", "", -30, now.Add(-time.Hour)),
		spendTx("lastweek", "EFTPOS", "Food", "Supermarkets and grocery stores", "", "", -99, at(time.October, 26)),
		spendTx("repay", models.TypeCountedTransfer, "Housing", "Mortgage", "", "AP#1 TO D J KING", -1284, at(time.November, 1)),
		spendTx("fuel", "EFTPOS", "Transport", "Fuel stations", "", "", -80, at(time.October, 30)),
		spendTx("refund", "EFTPOS", "Food", "Supermarkets and grocery stores", "", "", 10, at(time.October, 30)),
	}

	charts := BuildPeriodCharts(transactions, targets, now)

	var frequencies []string
	for _, chart := range charts {
		frequencies = append(frequencies, chart.Frequency)
	}
	require.Equal(t, []string{"weekly", "fortnightly"}, frequencies, "only periods some target is set per, in order")

	week := charts[0]
	assert.Equal(t, "Weekly", week.Title)
	assert.Equal(t, "27 Oct – 2 Nov", week.Range, "the seven days ending today")
	assert.Equal(t, 210.0, week.Target, "weekly targets, and those with no frequency, add up")
	assert.Equal(t, []string{"Groceries", "Eating & drinking out"}, week.Categories)
	assert.Equal(t, 100.0, week.Total, "the last seven days in those categories only")
	require.Len(t, week.Spent, 7)
	assert.Equal(t, 0.0, week.Spent[1])
	assert.Equal(t, 50.0, week.Spent[2], "each bar is the running total")
	assert.Equal(t, 70.0, week.Spent[4])
	assert.Equal(t, 100.0, week.Spent[6], "today is in it")
	assert.Equal(t, 30.0, week.Pace[0], "an even pace uses a seventh of the target a day")
	assert.Equal(t, 210.0, week.Pace[6])
	assert.Equal(t, 110.0, week.Remaining())
	assert.False(t, week.IsOver())
	assert.Equal(t, "/transactions?from=2026-10-27&group=groceries&group=eating-out&to=2026-11-02", week.Link,
		"the total opens the transactions behind it")
	require.Len(t, week.Links, 7)
	assert.Equal(t, "/transactions?from=2026-10-27&group=groceries&group=eating-out&to=2026-10-29", week.Links[2],
		"a bar is a running total, so it opens everything from the start of the period to the end of the bar")

	fortnight := charts[1]
	assert.Equal(t, "Fortnightly", fortnight.Title)
	assert.Equal(t, 1284.0, fortnight.Total, "yesterday's repayment is in the last fourteen days, though a calendar fortnight began today")
	assert.Len(t, fortnight.Spent, 14)

	t.Run("a year is shown by month", func(t *testing.T) {
		yearly := map[string]models.BudgetItem{"big-purchases": {BudgetCategory: "big-purchases", Amount: 6000, Frequency: "yearly"}}
		bike := spendTx("bike", "PAYMENT", "Transport", "Car, truck, and motorcycle dealers", "", "", -5490, at(time.October, 15))
		old := spendTx("old", "PAYMENT", "Transport", "Car, truck, and motorcycle dealers", "", "", -999, at(time.October, 15).AddDate(-1, 0, 0))
		charts := BuildPeriodCharts([]models.Transaction{bike, old}, yearly, now)
		require.Len(t, charts, 1)
		assert.Equal(t, "Yearly", charts[0].Title)
		assert.Equal(t, "Nov 25", charts[0].Labels[0], "from this month last year")
		require.Len(t, charts[0].Spent, 13)
		assert.Equal(t, "Nov 26", charts[0].Labels[12], "to this month, told apart by its year")
		assert.Equal(t, 5490.0, charts[0].Total, "a purchase over a year ago has dropped out")
	})

	t.Run("a split category opens its general row on its own", func(t *testing.T) {
		split := budgetTargets([]models.BudgetItem{
			{BudgetCategory: "home", Split: true, GeneralAmount: 300, GeneralFrequency: "quarterly"},
			{BudgetCategory: "rates", Amount: 900, Frequency: "quarterly"},
		})
		charts := BuildPeriodCharts(nil, split, now)
		require.Len(t, charts, 1)
		assert.Equal(t, "/transactions?from=2026-08-03&group=rates&group=home%3Ageneral&to=2026-11-02", charts[0].Link)
	})

	t.Run("the page shows each and a hint when there are none", func(t *testing.T) {
		html := renderTemplate(t, "budget.periods", charts)
		assert.Contains(t, html, `id="period-weekly"`)
		assert.Contains(t, html, `id="period-fortnightly"`)
		assert.Contains(t, html, "$110 to spare")
		assert.Contains(t, html, "in the last 7 days")
		assert.Contains(t, html, `href="/transactions?from=2026-10-27&amp;group=groceries&amp;group=eating-out&amp;to=2026-11-02"`,
			"the total is a link")
		assert.Contains(t, renderTemplate(t, "budget.periods", []PeriodChart(nil)), "Set a target below")
	})
}

func TestMonthsBackLandsEachBillOnce(t *testing.T) {
	at := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	tests := []struct {
		name   string
		today  time.Time
		months int
		start  time.Time
	}{
		{"an ordinary day", at(2026, time.March, 15), 1, at(2026, time.February, 16)},
		{"the end of a month after a short one", at(2026, time.March, 30), 1, at(2026, time.March, 1)},
		{"the last day of a month after a short one", at(2026, time.March, 31), 1, at(2026, time.March, 1)},
		{"a quarter ending on the 31st", at(2026, time.May, 31), 3, at(2026, time.March, 1)},
		{"a year ending on a leap day", at(2028, time.February, 29), 12, at(2027, time.March, 1)},
		{"across a new year", at(2026, time.January, 10), 1, at(2025, time.December, 11)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.start, monthsBack(tt.today, tt.months))
		})
	}

	t.Run("a bill on the 1st is in the window at the end of March", func(t *testing.T) {
		power := spendTx("power", "DIRECT DEBIT", "Utilities", "Electricity services", "Mercury", "", -180, at(2026, time.March, 1).Add(9*time.Hour))
		charts := BuildPeriodCharts([]models.Transaction{power},
			map[string]models.BudgetItem{"bills": {BudgetCategory: "bills", Amount: 200, Frequency: "monthly"}},
			at(2026, time.March, 30).Add(15*time.Hour))
		require.Len(t, charts, 1)
		assert.Equal(t, 180.0, charts[0].Total, "counting back with AddDate left this out")
	})
}
