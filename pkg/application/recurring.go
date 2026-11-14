package application

import (
	"cmp"
	"context"
	"math"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
)

// Recurring payments are the subscriptions and bills that come round on their
// own: streaming, power, insurance. They are found in the spending rather
// than set up by hand, by who was paid and how evenly the payments are
// spaced, so a new one turns up without anyone telling budge about it. The
// owner can say a payment only looks regular, or that it has been cancelled,
// and that is all that is kept about them.

const (
	// recurringHistoryMonths is how far back payments are read. A yearly
	// payment needs two before it is seen at all; two years and a little
	// more puts a third on show when it renews, so a renewal is not taken
	// for a payment seen for the first time.
	recurringHistoryMonths = 26

	// recurringRegularShare is the share of the gaps between payments that
	// have to fall within the cadence's band. A month skipped, or a payment
	// a few days late, leaves the rest regular.
	recurringRegularShare = 0.75

	// recurringMaxPerPeriod is the most payments a period may carry, on
	// average across the history, for a merchant to count as recurring. A
	// subscription is paid once a period; a shop visited weekly with the odd
	// trip between is bought from as needed.
	recurringMaxPerPeriod = 1.25

	// recurringRecentPayments is how many of the latest payments say what a
	// recurring payment costs now. Older ones are from before a price rise
	// or a winter, and say less about the next.
	recurringRecentPayments = 6

	// recurringSameShare and recurringSameDollars are how far apart two
	// payments can be and still be the same price: within 2%, or a dollar,
	// which absorbs the cents a card charged in another currency moves by.
	recurringSameShare   = 0.02
	recurringSameDollars = 1.00

	// recurringFixedSteps is how many times the price may change among the
	// recent payments for it to be a fixed price, such as a subscription. A
	// subscription's price rise is one step; a bill moves every time.
	recurringFixedSteps = 1

	// recurringChangeShare and recurringChangeDollars are how far a fixed
	// price has to move to be reported: more than 2% and more than 50c both,
	// so neither a rounding nor a cheap plan's few cents is news.
	recurringChangeShare   = 0.02
	recurringChangeDollars = 0.50

	// recurringStoppedPeriods is how many periods past its expected payment
	// a recurring payment goes unpaid before it is taken to have stopped.
	recurringStoppedPeriods = 2

	// recurringNewDays caps how long a recurring payment is new for, after
	// the payment that showed it to be recurring: its cadence's period, but
	// no more than about a month, so a yearly one is not new all year.
	recurringNewDays = 35

	// recurringComingUpDays is how far ahead the page lists what is due.
	recurringComingUpDays = 14

	// recurringDueSoonDays is how far ahead a large payment is warned of:
	// a week, time enough to move money to cover it.
	recurringDueSoonDays = 7

	// recurringLargeAmount is what a payment has to cost to be warned of
	// before it is due. A quarterly or yearly payment is easier to forget,
	// so recurringLargeRareAmount, less, is enough for those.
	recurringLargeAmount     = 200.0
	recurringLargeRareAmount = 100.0
)

// RecurringCadence is how often a recurring payment comes round. Each is
// found from the gaps between payments, in days, which have to fall within
// its band.
type RecurringCadence struct {
	Key   string
	Label string // "Monthly"
	Noun  string // "month", as in "a month"

	MinDays, MaxDays int

	// MinPayments is how many payments it takes to be sure of the cadence.
	// Two payments a year apart are as sure as a year gets.
	MinPayments int

	PerYear float64

	// Payments are this many months apart, or days where that is zero.
	months, days int
}

var recurringCadences = []RecurringCadence{
	{Key: "weekly", Label: "Weekly", Noun: "week", MinDays: 6, MaxDays: 8, MinPayments: 3, PerYear: 52, days: 7},
	{Key: "fortnightly", Label: "Fortnightly", Noun: "fortnight", MinDays: 13, MaxDays: 15, MinPayments: 3, PerYear: 26, days: 14},
	{Key: "monthly", Label: "Monthly", Noun: "month", MinDays: 27, MaxDays: 34, MinPayments: 3, PerYear: 12, months: 1},
	{Key: "quarterly", Label: "Quarterly", Noun: "quarter", MinDays: 84, MaxDays: 98, MinPayments: 3, PerYear: 4, months: 3},
	{Key: "yearly", Label: "Yearly", Noun: "year", MinDays: 350, MaxDays: 380, MinPayments: 2, PerYear: 1, months: 12},
}

// after is n periods on from day. A monthly payment on the 31st falls on the
// last day of a shorter month rather than spilling into the next.
func (c RecurringCadence) after(day time.Time, n int) time.Time {
	if c.months == 0 {
		return day.AddDate(0, 0, c.days*n)
	}
	year, month, d := day.Date()
	first := time.Date(year, month+time.Month(c.months*n), 1, 0, 0, 0, 0, day.Location())
	last := first.AddDate(0, 1, -1).Day()
	return first.AddDate(0, 0, min(d, last)-1)
}

// rare reports whether the cadence is long enough for a payment to be
// forgotten between times.
func (c RecurringCadence) rare() bool { return c.months >= 3 }

// newDays is how long after it is recognised a payment is new for.
func (c RecurringCadence) newDays() int { return min(c.MaxDays, recurringNewDays) }

// RecurringPayment is a payment found coming round on a cadence.
type RecurringPayment struct {
	Key  string // the merchant key it is found and marked by
	Name string

	Icon   string
	Accent string

	Cadence  RecurringCadence
	Payments int

	First time.Time
	Last  time.Time
	Next  time.Time // when the next payment is expected

	// Recognised is the payment that showed it to be recurring: the third,
	// or the second of a yearly one.
	Recognised time.Time

	Latest   float64 // the last payment
	Previous float64 // what it usually was before the last payment
	Usual    float64 // what it costs now

	// Fixed is a set price, such as a subscription, rather than a bill
	// that varies, such as power.
	Fixed bool

	// PriceChanged is a fixed price whose last payment moved from the one
	// before.
	PriceChanged bool

	New      bool
	Stopped  bool
	DueSoon  bool   // a large payment due within the week
	ComingUp bool   // due within the fortnight
	Mark     string // what the owner said of it, if anything
}

// Yearly is what the payment comes to over a year.
func (p RecurringPayment) Yearly() float64 { return p.Usual * p.Cadence.PerYear }

// Change is how far the price moved, as a share of what it was.
func (p RecurringPayment) Change() float64 {
	if p.Previous == 0 {
		return 0
	}
	return (p.Latest - p.Previous) / p.Previous
}

// Link opens the payments behind it.
func (p RecurringPayment) Link() string {
	return "/transactions?search=" + url.QueryEscape(p.Name)
}

// readRecurring finds the recurring payments in the spending up to now, and
// reads what the owner has said about them.
func (app *Application) readRecurring(ctx context.Context, now time.Time) ([]RecurringPayment, models.RecurringMarks, error) {
	transactions, err := app.store.ReadTransactionsByDate(ctx, now.AddDate(0, -recurringHistoryMonths, 0), now)
	if err != nil {
		return nil, models.RecurringMarks{}, err
	}
	marks, err := app.store.GetRecurringMarks(ctx)
	if err != nil {
		return nil, models.RecurringMarks{}, err
	}
	return FindRecurring(spendingOnly(transactions), now), marks, nil
}

// FindRecurring finds the recurring payments in the spending up to now, each
// with its cadence, what it costs, when it is next due and anything worth
// saying about it. Only money spent counts: transfers, card repayments, cash
// and interest move money without paying anyone for anything. Days are read
// in now's location.
func FindRecurring(transactions []models.Transaction, now time.Time) []RecurringPayment {
	location := now.Location()
	today := localDay(now, location)

	// The history starts at the oldest transaction of any kind, so a payment
	// first seen at the very start of it is not taken for a new one.
	var since time.Time
	byKey := map[string][]models.Transaction{}
	for _, tx := range transactions {
		if since.IsZero() || tx.Date.Before(since) {
			since = tx.Date
		}
		if !recurringSpend(tx) {
			continue
		}
		key := recurringKey(tx)
		byKey[key] = append(byKey[key], tx)
	}

	var found []RecurringPayment
	for key, txs := range byKey {
		slices.SortStableFunc(txs, func(a, b models.Transaction) int { return a.Date.Compare(b.Date) })
		payment, ok := detectRecurring(txs, location)
		if !ok {
			continue
		}
		payment.Key = key
		payment.Stopped = today.After(payment.Cadence.after(payment.Next, recurringStoppedPeriods))
		known := !localDay(since, location).After(payment.Cadence.after(payment.First, -1))
		payment.New = known && !payment.Stopped && daysBetween(payment.Recognised, today) <= payment.Cadence.newDays()
		ahead := daysBetween(today, payment.Next)
		payment.ComingUp = !payment.Stopped && ahead >= 0 && ahead <= recurringComingUpDays
		large := payment.Usual >= recurringLargeAmount || (payment.Cadence.rare() && payment.Usual >= recurringLargeRareAmount)
		payment.DueSoon = !payment.Stopped && ahead >= 0 && ahead <= recurringDueSoonDays && large
		found = append(found, payment)
	}
	slices.SortFunc(found, func(a, b RecurringPayment) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(a.Key, b.Key))
	})
	return found
}

// recurringSpend reports whether a transaction is a payment to someone that
// could come round again.
func recurringSpend(tx models.Transaction) bool {
	if _, ok := spendCategory(tx); !ok {
		return false
	}
	switch tx.Type {
	case "CREDIT CARD", "ATM", "INTEREST":
		return false
	}
	return true
}

// recurringKey names who a payment went to, as merchantKey does, but from the
// description cleaned of its card timestamps, so a merchant Akahu did not
// name is still the same merchant each time.
func recurringKey(tx models.Transaction) string {
	return strings.ToLower(strings.Join(strings.Fields(spendMerchant(tx)), " "))
}

// detectRecurring reads one merchant's payments, oldest first, for a
// cadence. It is not recurring unless most gaps between paying days fall in
// one cadence's band, there are enough of them, and the merchant is not paid
// more often than that cadence: a supermarket visited every few days has
// short gaps, and one visited weekly with trips between has too many visits.
func detectRecurring(txs []models.Transaction, location *time.Location) (RecurringPayment, bool) {
	// Two payments on one day, such as a top-up, are one day's payment.
	var days []time.Time
	var amounts []float64
	for _, tx := range txs {
		day := localDay(tx.Date, location)
		if n := len(days); n > 0 && days[n-1].Equal(day) {
			amounts[n-1] += -tx.Amount
			continue
		}
		days = append(days, day)
		amounts = append(amounts, -tx.Amount)
	}
	if len(days) < 2 {
		return RecurringPayment{}, false
	}

	gaps := make([]float64, len(days)-1)
	for i := 1; i < len(days); i++ {
		gaps[i-1] = float64(daysBetween(days[i-1], days[i]))
	}
	typical := medianOf(gaps)
	index := slices.IndexFunc(recurringCadences, func(c RecurringCadence) bool {
		return typical >= float64(c.MinDays) && typical <= float64(c.MaxDays)
	})
	if index < 0 {
		return RecurringPayment{}, false
	}
	cadence := recurringCadences[index]
	if len(days) < cadence.MinPayments {
		return RecurringPayment{}, false
	}
	regular := 0
	for _, gap := range gaps {
		if gap >= float64(cadence.MinDays) && gap <= float64(cadence.MaxDays) {
			regular++
		}
	}
	if float64(regular) < recurringRegularShare*float64(len(gaps)) {
		return RecurringPayment{}, false
	}
	span := float64(daysBetween(days[0], days[len(days)-1]))
	if float64(len(gaps)) > recurringMaxPerPeriod*span/(365.25/cadence.PerYear) {
		return RecurringPayment{}, false
	}

	recent := amounts[max(0, len(amounts)-recurringRecentPayments):]
	steps := 0
	for i := 1; i < len(recent); i++ {
		if !samePrice(recent[i-1], recent[i]) {
			steps++
		}
	}
	latest := recent[len(recent)-1]
	before := recent[:len(recent)-1]
	payment := RecurringPayment{
		Cadence:    cadence,
		Payments:   len(days),
		First:      days[0],
		Last:       days[len(days)-1],
		Next:       cadence.after(days[len(days)-1], 1),
		Recognised: days[cadence.MinPayments-1],
		Latest:     latest,
		Previous:   medianOf(before),
		Usual:      medianOf(recent),
		Fixed:      steps <= recurringFixedSteps,
	}
	if payment.Fixed {
		// A fixed price costs what it was last charged, whatever it cost
		// before it went up.
		payment.Usual = latest
		// The change is the last payment's own, not one that came earlier
		// and the last only repeated.
		payment.PriceChanged = priceChanged(payment.Previous, latest) && priceChanged(before[len(before)-1], latest)
	}

	last := txs[len(txs)-1]
	payment.Name = spendMerchant(last)
	category := models.TopBudgetCategory(last.BudgetCategory())
	payment.Accent, payment.Icon = category.Accent, category.Icon
	if sub, ok := models.BudgetCategoryByKey(last.BudgetCategory()); ok && sub.Icon != "" {
		payment.Icon = sub.Icon
	}
	if payment.Icon == "" {
		payment.Icon = "bi-arrow-repeat"
	}
	return payment, true
}

// samePrice reports whether two payments are the same price, give or take.
func samePrice(a, b float64) bool {
	diff := math.Abs(a - b)
	return diff <= recurringSameDollars || diff <= recurringSameShare*math.Max(a, b)
}

// priceChanged reports whether a fixed price moved from was to now by enough
// to say so.
func priceChanged(was, now float64) bool {
	diff := math.Abs(now - was)
	return diff > recurringChangeDollars && diff > recurringChangeShare*was
}

// medianOf is the middle value, or the mean of the middle two.
func medianOf(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// localDay is midnight starting the day t falls on, in location.
func localDay(t time.Time, location *time.Location) time.Time {
	year, month, day := t.In(location).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, location)
}

// daysBetween counts whole days from one local midnight to another. It
// rounds, as a day either side of a change of clocks is an hour long or
// short.
func daysBetween(from, to time.Time) int {
	return int(math.Round(to.Sub(from).Hours() / 24))
}

// RecurringView is the recurring payments as the Insights page shows them,
// with the owner's marks applied.
type RecurringView struct {
	// Groups are the active payments, by cadence, weekly first.
	Groups []RecurringGroup

	// ComingUp are the active payments due in the next fortnight, soonest
	// first.
	ComingUp []RecurringPayment

	// Stopped have gone quiet, or were marked cancelled.
	Stopped []RecurringPayment

	// Hidden were marked not recurring.
	Hidden []RecurringPayment

	Count  int     // active payments
	Yearly float64 // what the active payments come to over a year
}

// Monthly is what the active payments come to in an average month.
func (v RecurringView) Monthly() float64 { return v.Yearly / 12 }

// Empty reports whether nothing recurring was found at all.
func (v RecurringView) Empty() bool {
	return v.Count == 0 && len(v.Stopped) == 0 && len(v.Hidden) == 0
}

// RecurringGroup is the active payments of one cadence.
type RecurringGroup struct {
	Cadence  RecurringCadence
	Payments []RecurringPayment
	Yearly   float64
}

// BuildRecurringView sorts what was found into what the page shows, by the
// owner's marks. A payment marked not recurring is hidden, and one marked
// cancelled has stopped, whatever its payments say; neither counts towards
// the totals.
func BuildRecurringView(found []RecurringPayment, marks map[string]string) RecurringView {
	var view RecurringView
	groups := map[string]*RecurringGroup{}
	for _, payment := range applyRecurringMarks(found, marks) {
		switch {
		case payment.Mark == models.RecurringNotRecurring:
			view.Hidden = append(view.Hidden, payment)
		case payment.Stopped:
			view.Stopped = append(view.Stopped, payment)
		default:
			view.Count++
			view.Yearly += payment.Yearly()
			if payment.ComingUp {
				view.ComingUp = append(view.ComingUp, payment)
			}
			group, ok := groups[payment.Cadence.Key]
			if !ok {
				group = &RecurringGroup{Cadence: payment.Cadence}
				groups[payment.Cadence.Key] = group
			}
			group.Payments = append(group.Payments, payment)
			group.Yearly += payment.Yearly()
		}
	}
	for _, cadence := range recurringCadences {
		if group, ok := groups[cadence.Key]; ok {
			view.Groups = append(view.Groups, *group)
		}
	}
	slices.SortStableFunc(view.ComingUp, func(a, b RecurringPayment) int { return a.Next.Compare(b.Next) })
	return view
}

// applyRecurringMarks sets each payment's mark. One marked cancelled has
// stopped, and one marked either way is neither new nor due: it raises
// nothing.
func applyRecurringMarks(found []RecurringPayment, marks map[string]string) []RecurringPayment {
	marked := slices.Clone(found)
	for i := range marked {
		mark := marks[marked[i].Key]
		if !models.ValidRecurringMark(mark) {
			continue
		}
		marked[i].Mark = mark
		marked[i].New, marked[i].DueSoon, marked[i].ComingUp, marked[i].PriceChanged = false, false, false, false
		if mark == models.RecurringCancelled {
			marked[i].Stopped = true
		}
	}
	return marked
}
