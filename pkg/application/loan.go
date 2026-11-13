package application

import (
	"math"
	"strings"
	"time"
)

// Akahu reports a mortgage as a LOAN. MORTGAGE is not one of its types, but
// it is what another provider would call the same thing, so it is read as a
// loan too rather than charted as an account that is simply very overdrawn.
var loanAccountTypes = []string{"LOAN", "MORTGAGE"}

// IsLoanAccount reports whether an account type is borrowing being paid
// down, which the account page gives a payoff projection.
func IsLoanAccount(accountType string) bool {
	for _, kind := range loanAccountTypes {
		if strings.EqualFold(accountType, kind) {
			return true
		}
	}
	return false
}

const (
	// loanRateMonths is how far back the repayment rate is averaged: long
	// enough to smooth a month with a missed or doubled repayment, short
	// enough to follow a change in what is being paid.
	loanRateMonths = 12

	// loanMinMonths is the fewest whole months of change the rate is worked
	// out from. Fewer than that and one odd month is the whole estimate.
	loanMinMonths = 3

	// loanHorizonMonths is as far ahead as a payoff is projected. Beyond
	// forty years the rate says more about the last few months than about
	// when the loan will be paid off, and the history would be squeezed to
	// nothing against the projection.
	loanHorizonMonths = 40 * 12
)

// MonthBalance is an account's balance at the end of a month, or as it
// stands for the month in progress. Month is the first of the month, UTC.
type MonthBalance struct {
	Month   time.Time
	Balance float64
}

// LoanOverview is how a loan is being paid down, worked out from its month
// end balances. Amounts owing are positive, however the provider signs them.
type LoanOverview struct {
	Owing float64 // What is owed now.
	Since time.Time
	// PaidOff is how much less is owed now than at Since. It is negative
	// when the loan has grown.
	PaidOff float64
	// Rate is the average fall in what is owed each month over the last
	// RateMonths whole months: repayments less interest. Zero RateMonths
	// means there was not enough history to work it out.
	Rate       float64
	RateMonths int

	// Payoff is the month the loan is estimated to be paid off at Rate,
	// when HasPayoff. Projection is what is owed each month after the last
	// balance until then, ending at zero.
	HasPayoff  bool
	Payoff     time.Time
	Projection []MonthBalance

	// History is what was owed at the end of each month, as the balances
	// given, to chart alongside the projection.
	History []MonthBalance

	// Settled is a loan that owes nothing.
	Settled bool
	// Note says why there is no payoff date, when there is none.
	Note string
}

// loanOverview works out how a loan is being paid down from its balance at
// the end of each month, oldest first and one a month with none missing,
// the last being the month now falls in. That month is not over, so it is
// left out of the rate: it may hold the month's repayment and not yet its
// interest. The projection starts from it all the same, as it is what is
// owed today.
func loanOverview(balances []MonthBalance, now time.Time) LoanOverview {
	if len(balances) == 0 {
		return LoanOverview{Note: "no history to go on"}
	}

	// A loan is signed negative by Akahu, but a provider that signs debt
	// positive gives the same loan, so the sign is read from the balances.
	sign := 1.0
	for _, b := range balances {
		if b.Balance < 0 {
			sign = -1
			break
		}
	}
	owing := func(i int) float64 { return sign * balances[i].Balance }

	last := len(balances) - 1
	overview := LoanOverview{
		Owing:   roundCents(owing(last)),
		Since:   balances[0].Month,
		PaidOff: roundCents(owing(0) - owing(last)),
	}
	for i, b := range balances {
		overview.History = append(overview.History, MonthBalance{Month: b.Month, Balance: roundCents(owing(i))})
	}

	if overview.Owing <= 0 {
		overview.Settled = true
		overview.Note = "nothing left to pay"
		return overview
	}

	// The rate is measured to the end of the last whole month.
	complete := last
	if inMonth(balances[last].Month, now) {
		complete--
	}
	months := min(loanRateMonths, complete)
	if months < loanMinMonths {
		overview.Note = "needs a few months of history to estimate"
		return overview
	}
	overview.RateMonths = months
	overview.Rate = roundCents((owing(complete-months) - owing(complete)) / float64(months))
	if overview.Rate <= 0 {
		overview.Note = "not falling, so there's no payoff to estimate"
		return overview
	}

	remaining := int(math.Ceil(overview.Owing / overview.Rate))
	if remaining > loanHorizonMonths {
		overview.Note = "more than 40 years away at this rate"
		return overview
	}

	start := balances[last].Month
	for i := 1; i <= remaining; i++ {
		owed := math.Max(0, roundCents(overview.Owing-overview.Rate*float64(i)))
		overview.Projection = append(overview.Projection, MonthBalance{Month: start.AddDate(0, i, 0), Balance: owed})
	}
	overview.HasPayoff = true
	overview.Payoff = start.AddDate(0, remaining, 0)
	return overview
}

// inMonth reports whether t falls in the month starting at month.
func inMonth(month, t time.Time) bool {
	t = t.UTC()
	return month.Year() == t.Year() && month.Month() == t.Month()
}
