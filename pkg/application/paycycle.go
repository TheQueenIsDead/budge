package application

import (
	"slices"
	"sync/atomic"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
)

// Spending is counted by pay cycle rather than calendar month: the pay that
// lands on the 28th is next month's money, so a calendar month shows a pay
// with the wrong month's spending against it. The pay dates are found from
// the transactions at startup and after each sync. Without them, months are
// calendar months. The timeline keeps calendar months: memories are dated,
// not budgeted.

// payDates finds when the owner is paid: the large deposit from one payer
// that lands month after month. Its dates, oldest first, mark out pay
// cycles, which is how a monthly budget is lived: the pay on the 28th is next
// month's money, not the tail of this one. Dates rather than a day of the
// month, because pay moves forward when the day falls on a weekend. Nil when
// nothing arrives regularly enough to call pay.
func payDates(txs []models.Transaction) []time.Time {
	type payer struct {
		dates  []time.Time
		months map[time.Time]bool
		total  float64
	}
	payers := map[string]*payer{}
	for _, tx := range txs {
		if tx.Amount < 1000 || tx.Type == "TRANSFER" || tx.Category.Name == "Transfer" {
			continue
		}
		key := lettersOnly(tx.Description)
		if len(key) > 16 {
			key = key[:16]
		}
		p := payers[key]
		if p == nil {
			p = &payer{months: map[time.Time]bool{}}
			payers[key] = p
		}
		p.dates = append(p.dates, tx.Date)
		p.months[time.Date(tx.Date.Year(), tx.Date.Month(), 1, 0, 0, 0, 0, time.UTC)] = true
		p.total += tx.Amount
	}
	var best *payer
	for _, p := range payers {
		if len(p.months) < 3 {
			continue
		}
		if best == nil || len(p.months) > len(best.months) || (len(p.months) == len(best.months) && p.total > best.total) {
			best = p
		}
	}
	if best == nil {
		return nil
	}
	slices.SortFunc(best.dates, func(a, b time.Time) int { return a.Compare(b) })
	return best.dates
}

// payCycle holds the pay dates, at midnight UTC on the local day they
// landed. Local, because pay lands early in the morning: a pay on the 28th
// in New Zealand is still the 27th in UTC.
var payCycle atomic.Pointer[[]time.Time]

// SetPayDates replaces the pay dates months are counted between.
func SetPayDates(dates []time.Time) {
	days := make([]time.Time, 0, len(dates))
	for _, d := range dates {
		d = d.In(time.Local)
		day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
		if len(days) == 0 || !days[len(days)-1].Equal(day) {
			days = append(days, day)
		}
	}
	if len(days) < 2 {
		payCycle.Store(nil)
		return
	}
	payCycle.Store(&days)
}

func payCycleKnown() bool {
	return payCycle.Load() != nil
}

// localDay is a pay date as midnight in loc, so it compares and keys alike
// with the periods built from a local now.
func localDay(d time.Time, loc *time.Location) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
}

// spendPeriodStart is periodStart for spending: a month starts on the
// payday that funded it. Before the first known pay, and after the last,
// pay is taken to fall on the same day each month.
func spendPeriodStart(t time.Time, cadence Cadence) time.Time {
	dates := payCycle.Load()
	if cadence != CadenceMonthly || dates == nil {
		return periodStart(t, cadence)
	}
	days := *dates
	loc := t.Location()
	first := localDay(days[0], loc)
	if t.Before(first) {
		start := first
		for t.Before(start) {
			start = start.AddDate(0, -1, 0)
		}
		return start
	}
	i, found := slices.BinarySearchFunc(days, t, func(day, target time.Time) int {
		return localDay(day, loc).Compare(target)
	})
	if found {
		return localDay(days[i], loc)
	}
	start := localDay(days[i-1], loc)
	if i == len(days) {
		// Past the last pay: count on from it a month at a time.
		for !t.Before(start.AddDate(0, 1, 0)) {
			start = start.AddDate(0, 1, 0)
		}
	}
	return start
}

// spendPeriodAdd steps n periods from the start of one, through the known
// paydays and a month at a time beyond them.
func spendPeriodAdd(start time.Time, cadence Cadence, n int) time.Time {
	dates := payCycle.Load()
	if cadence != CadenceMonthly || dates == nil {
		return periodAdd(start, cadence, n)
	}
	days := *dates
	loc := start.Location()
	i := slices.IndexFunc(days, func(d time.Time) bool { return localDay(d, loc).Equal(start) })
	if i < 0 {
		// An estimated payday before the first or after the last known one.
		next := start.AddDate(0, n, 0)
		if n > 0 || next.Before(localDay(days[0], loc)) {
			return next
		}
		return spendPeriodStart(next, cadence)
	}
	switch j := i + n; {
	case j < 0:
		return localDay(days[0], loc).AddDate(0, j, 0)
	case j >= len(days):
		return localDay(days[len(days)-1], loc).AddDate(0, j-len(days)+1, 0)
	default:
		return localDay(days[j], loc)
	}
}

// spendPeriodLabel names a period: a pay cycle by the payday that started
// it, a calendar month by its name.
func spendPeriodLabel(start time.Time, cadence Cadence) string {
	if cadence == CadenceMonthly && payCycleKnown() {
		return start.Format("2 Jan")
	}
	return periodLabel(start, cadence)
}

// refreshPayCycle finds the pay dates from the last year and a bit of
// transactions.
func (app *Application) refreshPayCycle() error {
	txs, err := app.store.ReadTransactionsByDate(time.Now().AddDate(0, -14, 0), time.Now())
	if err != nil {
		return err
	}
	SetPayDates(payDates(txs))
	return nil
}
