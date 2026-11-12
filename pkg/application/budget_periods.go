package application

import (
	"math"
	"net/http"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v5"
)

// The budget's period charts show each kind of target against its own
// period: the weekly targets against the last week, the monthly ones against
// the last month, and so on. Measuring everything weekly made a quarterly
// rates bill look like a blown week; here it lands in the period it is
// budgeted for.
//
// Each period is the one ending today rather than a calendar one. A calendar
// fortnight that began yesterday is empty, however the last one went; the
// last fourteen days always hold a fortnight's spending, so the chart says how
// things are going on any day it is looked at.

// budgetPeriods are the frequencies a target can be set per, in the order
// their charts are shown.
var budgetPeriods = []string{"weekly", "fortnightly", "monthly", "quarterly", "yearly"}

// PeriodChart is a period's spending built up against its targets.
type PeriodChart struct {
	Frequency string
	Title     string // "Weekly", "Monthly"...
	Range     string // the dates it covers
	Span      string // "the last 14 days"...

	// Labels name each bar: a day, a week, or a month for the year. Spent is
	// the running total at the end of each, and Pace how much of the target
	// an even spend would have used by then.
	Labels []string
	Spent  []float64
	Pace   []float64

	Target     float64
	Total      float64
	Categories []string

	// Link opens the transactions behind the total, and Links those behind
	// each bar. A bar is a running total, so its transactions run from the
	// start of the period to the end of the bar.
	Link  string
	Links []string
}

// Remaining is what is left of the target, never below nothing.
func (p PeriodChart) Remaining() float64 { return math.Max(0, p.Target-p.Total) }

// Overspent is how far past the target the period has gone.
func (p PeriodChart) Overspent() float64 { return math.Max(0, p.Total-p.Target) }

// IsOver reports whether the period spent more than its target.
func (p PeriodChart) IsOver() bool { return p.Total > p.Target+overToleranceWeekly }

// rollingBounds is the period of a frequency ending today, in now's location,
// with the steps its chart is broken into and how each is labelled. The
// period runs to the end of today, so today's spending is in it.
func rollingBounds(frequency string, now time.Time) (start, end time.Time, step func(time.Time) time.Time, label string, span string) {
	y, m, d := now.Date()
	end = time.Date(y, m, d, 0, 0, 0, 0, now.Location()).AddDate(0, 0, 1)
	day := func(t time.Time) time.Time { return t.AddDate(0, 0, 1) }
	week := func(t time.Time) time.Time { return t.AddDate(0, 0, 7) }
	// The first of the next month, so each bar after the first is a whole
	// month. Adding a month to a start late in one overflows instead: 30
	// January and a month is 2 March, and February had no bar.
	month := func(t time.Time) time.Time {
		return time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
	}

	today := end.AddDate(0, 0, -1)
	switch frequency {
	case "fortnightly":
		return end.AddDate(0, 0, -14), end, day, "Mon 2", "the last 14 days"
	case "monthly":
		return monthsBack(today, 1), end, day, "2 Jan", "the last month"
	case "quarterly":
		// By week: ninety-odd daily bars are too thin to read.
		return monthsBack(today, 3), end, week, "2 Jan", "the last 3 months"
	case "yearly":
		// Labelled with the year: unless today ends a month, the first and
		// last bars are the same month a year apart.
		return monthsBack(today, 12), end, month, "Jan 06", "the last 12 months"
	}
	return end.AddDate(0, 0, -7), end, day, "Mon 2", "the last 7 days"
}

// monthsBack is where a window of the given months ending today starts: the
// day after today's date that many months ago, or after the last day of that
// month when it is too short to have the date. A bill paid on the same day
// each month then lands in the window once, never twice and never missed.
// Counting back with AddDate instead overflows: a month before 31 March is
// 3 March, which leaves out a bill paid on the 1st.
func monthsBack(today time.Time, months int) time.Time {
	y, m, d := today.Date()
	first := time.Date(y, m-time.Month(months), 1, 0, 0, 0, 0, today.Location())
	if last := first.AddDate(0, 1, -1).Day(); d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, 0, 0, 0, 0, today.Location()).AddDate(0, 0, 1)
}

// periodTitle names a chart by the frequency of the targets in it. The dates
// it covers are given beside it.
func periodTitle(frequency string) string {
	switch frequency {
	case "fortnightly":
		return "Fortnightly"
	case "monthly":
		return "Monthly"
	case "quarterly":
		return "Quarterly"
	case "yearly":
		return "Yearly"
	}
	return "Weekly"
}

// BuildPeriodCharts builds a chart for each frequency some target is set
// per, from the spending in the categories with those targets.
func BuildPeriodCharts(transactions []models.Transaction, targets map[string]models.BudgetItem, now time.Time) []PeriodChart {
	// The targets in force: a category's own, or each of its subcategories'
	// when it is split, which are matched on their own spending.
	type line struct {
		category models.BudgetCategory
		item     models.BudgetItem
	}
	var lines []line
	for _, category := range models.TopBudgetCategories() {
		if !budgetedSplit(category, targets) {
			lines = append(lines, line{category, targets[category.Key]})
			continue
		}
		for _, sub := range models.BudgetSubcategories(category.Key) {
			lines = append(lines, line{sub, targets[sub.Key]})
		}
		general := generalCategory(category)
		lines = append(lines, line{general, targets[general.Key]})
	}

	// Each spending transaction is filed once, here, rather than once for
	// every period charted. A transaction filed under a category itself is
	// that category's general bucket when it is split.
	type spent struct {
		date   time.Time
		amount float64
		top    string
		filed  string
	}
	var spending []spent
	for _, tx := range transactions {
		category, ok := spendCategory(tx)
		if !ok {
			continue
		}
		filed := tx.BudgetCategory()
		if filed == category.Key {
			filed = generalKey(filed)
		}
		spending = append(spending, spent{date: tx.Date.In(now.Location()), amount: -tx.Amount, top: category.Key, filed: filed})
	}

	var charts []PeriodChart
	for _, frequency := range budgetPeriods {
		inPeriod := map[string]bool{}
		var target float64
		var names, keys []string
		for _, l := range lines {
			if l.item.Amount <= 0 || frequencyOrWeekly(l.item.Frequency) != frequency {
				continue
			}
			inPeriod[l.category.Key] = true
			target += l.item.Amount
			names = append(names, l.category.Label)
			keys = append(keys, l.category.Key)
		}
		if len(inPeriod) == 0 {
			continue
		}

		start, end, step, format, span := rollingBounds(frequency, now)
		chart := PeriodChart{
			Frequency:  frequency,
			Title:      periodTitle(frequency),
			Range:      TimelineEntry{Start: start, End: end.AddDate(0, 0, -1)}.Span(),
			Span:       span,
			Target:     target,
			Categories: names,
			Link:       transactionsLink(keys, start, end),
		}

		var bounds []time.Time
		for at := start; at.Before(end); at = step(at) {
			bounds = append(bounds, at)
			chart.Labels = append(chart.Labels, at.Format(format))
		}

		totals := make([]float64, len(bounds))
		for _, s := range spending {
			if !inPeriod[s.top] && !inPeriod[s.filed] || s.date.Before(start) || !s.date.Before(end) {
				continue
			}
			for i := len(bounds) - 1; i >= 0; i-- {
				if !s.date.Before(bounds[i]) {
					totals[i] += s.amount
					break
				}
			}
		}

		length := end.Sub(start).Hours()
		var running float64
		for i := range bounds {
			next := end
			if i+1 < len(bounds) {
				next = bounds[i+1]
			}
			chart.Pace = append(chart.Pace, math.Round(target*next.Sub(start).Hours()/length*100)/100)
			chart.Links = append(chart.Links, transactionsLink(keys, start, next))
			running += totals[i]
			chart.Spent = append(chart.Spent, roundCents(running))
		}
		chart.Total = roundCents(running)
		charts = append(charts, chart)
	}
	return charts
}

// frequencyOrWeekly reads a target's frequency, an unset one being weekly.
func frequencyOrWeekly(frequency string) string {
	if frequency == "" {
		return "weekly"
	}
	return frequency
}

// BudgetPeriods answers with the period charts, redrawn after a target
// changes.
func (app *Application) BudgetPeriods(c *echo.Context) error {
	ctx := c.Request().Context()
	now := time.Now()
	txs, err := app.store.ReadTransactionsByDate(ctx, now.AddDate(-1, 0, -1), now)
	if err != nil {
		return err
	}
	items, err := app.store.ReadBudgetItems(ctx)
	if err != nil {
		return err
	}
	return c.Render(http.StatusOK, "budget.periods", BuildPeriodCharts(txs, budgetTargets(items), now))
}
