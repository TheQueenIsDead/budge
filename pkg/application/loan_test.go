package application

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// months gives a run of month end balances, one a month from the first of
// from, in the order given.
func months(from time.Time, balances ...float64) []MonthBalance {
	start := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
	out := make([]MonthBalance, len(balances))
	for i, b := range balances {
		out[i] = MonthBalance{Month: start.AddDate(0, i, 0), Balance: b}
	}
	return out
}

// falling is n month end balances of a loan, signed as Akahu signs it,
// starting at owed and owing by less each month.
func falling(owed, by float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = -(owed - by*float64(i))
	}
	return out
}

func month(year int, m time.Month) time.Time {
	return time.Date(year, m, 1, 0, 0, 0, 0, time.UTC)
}

func TestLoanOverview(t *testing.T) {
	jan25 := month(2025, time.January)

	tests := []struct {
		name     string
		balances []MonthBalance
		now      time.Time
		check    func(t *testing.T, o LoanOverview)
	}{
		{
			name: "a steady payoff is projected to zero",
			// Jan 2025 to Jun 2026, falling 1,000 a month, with now in June.
			balances: months(jan25, falling(30_000, 1_000, 18)...),
			now:      time.Date(2026, time.June, 15, 9, 0, 0, 0, time.UTC),
			check: func(t *testing.T, o LoanOverview) {
				assert.Equal(t, 13_000.0, o.Owing)
				assert.Equal(t, 17_000.0, o.PaidOff)
				assert.Equal(t, jan25, o.Since)
				assert.Equal(t, 1_000.0, o.Rate)
				assert.Equal(t, 12, o.RateMonths)
				assert.True(t, o.HasPayoff)
				// 13 more months from June 2026.
				assert.Equal(t, month(2027, time.July), o.Payoff)
				assert.Len(t, o.Projection, 13)
				assert.Equal(t, MonthBalance{Month: month(2026, time.July), Balance: 12_000}, o.Projection[0])
				assert.Equal(t, MonthBalance{Month: month(2027, time.July), Balance: 0}, o.Projection[12])
				assert.Empty(t, o.Note)
				require.Len(t, o.History, 18)
				assert.Equal(t, MonthBalance{Month: jan25, Balance: 30_000}, o.History[0], "owing reads positive")
			},
		},
		{
			name: "the month in progress is left out of the rate",
			// The last month has had its repayment and not its interest, so
			// it fell by far more than a whole month does.
			balances: months(jan25, -10_000, -9_500, -9_000, -8_500, -6_000),
			now:      time.Date(2025, time.May, 2, 0, 0, 0, 0, time.UTC),
			check: func(t *testing.T, o LoanOverview) {
				assert.Equal(t, 500.0, o.Rate)
				assert.Equal(t, 3, o.RateMonths)
				// But it is what is owed today, and the projection starts there.
				assert.Equal(t, 6_000.0, o.Owing)
				assert.Equal(t, month(2026, time.May), o.Payoff)
			},
		},
		{
			name:     "the rate is averaged over the last twelve months only",
			balances: months(jan25, append(falling(100_000, 5_000, 6), falling(75_000, 1_000, 14)...)...),
			now:      time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC),
			check: func(t *testing.T, o LoanOverview) {
				assert.Equal(t, 1_000.0, o.Rate)
				assert.Equal(t, 12, o.RateMonths)
			},
		},
		{
			name:     "a growing balance is not projected",
			balances: months(jan25, -10_000, -10_100, -10_200, -10_300, -10_400, -10_500),
			now:      time.Date(2025, time.June, 10, 0, 0, 0, 0, time.UTC),
			check: func(t *testing.T, o LoanOverview) {
				assert.Equal(t, -100.0, o.Rate)
				assert.Equal(t, -500.0, o.PaidOff)
				assert.False(t, o.HasPayoff)
				assert.Empty(t, o.Projection)
				assert.Contains(t, o.Note, "not falling")
			},
		},
		{
			name:     "a flat balance is not projected",
			balances: months(jan25, -10_000, -10_000, -10_000, -10_000, -10_000),
			now:      time.Date(2025, time.May, 10, 0, 0, 0, 0, time.UTC),
			check: func(t *testing.T, o LoanOverview) {
				assert.Equal(t, 0.0, o.Rate)
				assert.False(t, o.HasPayoff)
				assert.Contains(t, o.Note, "not falling")
			},
		},
		{
			name: "too little history is not projected",
			// Three balances, but the last is this month: two whole months
			// of change.
			balances: months(jan25, -10_000, -9_000, -8_000),
			now:      time.Date(2025, time.March, 20, 0, 0, 0, 0, time.UTC),
			check: func(t *testing.T, o LoanOverview) {
				assert.Equal(t, 8_000.0, o.Owing)
				assert.Equal(t, 2_000.0, o.PaidOff)
				assert.Zero(t, o.RateMonths)
				assert.False(t, o.HasPayoff)
				assert.Contains(t, o.Note, "few months")
			},
		},
		{
			name: "three whole months of change are enough",
			// The same balances two months on: January to April are whole.
			balances: months(jan25, -10_000, -9_000, -8_000, -7_000, -6_500),
			now:      time.Date(2025, time.May, 1, 0, 0, 0, 0, time.UTC),
			check: func(t *testing.T, o LoanOverview) {
				assert.Equal(t, 3, o.RateMonths)
				assert.Equal(t, 1_000.0, o.Rate)
				assert.True(t, o.HasPayoff)
			},
		},
		{
			name: "a payoff beyond forty years is not drawn",
			// 400,000 at 800 a month is 500 months away.
			balances: months(jan25, falling(404_000, 800, 6)...),
			now:      time.Date(2025, time.June, 15, 0, 0, 0, 0, time.UTC),
			check: func(t *testing.T, o LoanOverview) {
				assert.Equal(t, 800.0, o.Rate)
				assert.False(t, o.HasPayoff)
				assert.Empty(t, o.Projection)
				assert.Contains(t, o.Note, "40 years")
			},
		},
		{
			name: "a payoff just inside forty years is drawn",
			// 480 months at 1,000 is exactly forty years.
			balances: months(jan25, falling(485_000, 1_000, 6)...),
			now:      time.Date(2025, time.June, 15, 0, 0, 0, 0, time.UTC),
			check: func(t *testing.T, o LoanOverview) {
				assert.True(t, o.HasPayoff)
				assert.Equal(t, month(2065, time.June), o.Payoff)
				assert.Len(t, o.Projection, 480)
			},
		},
		{
			name: "a payoff crosses the year and lands on the right month",
			// Now is the last moment of the 31st, and the history is all
			// whole months. Adding months to the 31st would skip February.
			balances: months(month(2025, time.August), -6_000, -5_000, -4_000, -3_000),
			now:      time.Date(2025, time.December, 31, 23, 59, 0, 0, time.UTC),
			check: func(t *testing.T, o LoanOverview) {
				// The last balance, November's, is whole, so the rate is
				// three months, August to November.
				assert.Equal(t, 3, o.RateMonths)
				assert.Equal(t, 1_000.0, o.Rate)
				// 3,000 at 1,000 is three months on from November.
				assert.Equal(t, month(2026, time.February), o.Payoff)
				assert.Equal(t, []time.Time{month(2025, time.December), month(2026, time.January), month(2026, time.February)},
					[]time.Time{o.Projection[0].Month, o.Projection[1].Month, o.Projection[2].Month})
			},
		},
		{
			name:     "a month in progress on its first day is still in progress",
			balances: months(month(2025, time.October), -4_000, -3_000, -2_000, -1_000, -900),
			now:      time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC),
			check: func(t *testing.T, o LoanOverview) {
				assert.Equal(t, 3, o.RateMonths)
				assert.Equal(t, 1_000.0, o.Rate)
				assert.Equal(t, month(2026, time.March), o.Payoff)
			},
		},
		{
			name:     "a loan signed positive reads the same",
			balances: months(jan25, 10_000, 9_000, 8_000, 7_000, 6_500),
			now:      time.Date(2025, time.May, 5, 0, 0, 0, 0, time.UTC),
			check: func(t *testing.T, o LoanOverview) {
				assert.Equal(t, 6_500.0, o.Owing)
				assert.Equal(t, 3_500.0, o.PaidOff)
				assert.Equal(t, 1_000.0, o.Rate)
				assert.True(t, o.HasPayoff)
			},
		},
		{
			name:     "a paid off loan has nothing to project",
			balances: months(jan25, -2_000, -1_000, 0, 0),
			now:      time.Date(2025, time.April, 5, 0, 0, 0, 0, time.UTC),
			check: func(t *testing.T, o LoanOverview) {
				assert.True(t, o.Settled)
				assert.False(t, o.HasPayoff)
				assert.Equal(t, 2_000.0, o.PaidOff)
			},
		},
		{
			name: "no history",
			now:  time.Date(2025, time.April, 5, 0, 0, 0, 0, time.UTC),
			check: func(t *testing.T, o LoanOverview) {
				assert.False(t, o.HasPayoff)
				assert.NotEmpty(t, o.Note)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.check(t, loanOverview(test.balances, test.now))
		})
	}
}

func TestIsLoanAccount(t *testing.T) {
	assert.True(t, IsLoanAccount("LOAN"))
	assert.True(t, IsLoanAccount("loan"))
	assert.True(t, IsLoanAccount("MORTGAGE"))
	assert.False(t, IsLoanAccount("CREDITCARD"))
	assert.False(t, IsLoanAccount(""))
}
