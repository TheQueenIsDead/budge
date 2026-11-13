package application

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/clock"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v5"
)

// transactionPageSize is how many transactions a single render carries. The
// full history runs to thousands of rows, which no phone should be asked to
// lay out in one go, so the list pages instead.
const transactionPageSize = 100

// TransactionRow is a transaction prepared for display. It carries the name the
// row should be listed under and the colour and icon of the category it is
// filed under, the same ones that category wears everywhere else in the app.
type TransactionRow struct {
	models.Transaction

	Title  string
	Accent string
	Icon   string

	// MemoryID and MemoryTitle name the memory the transaction belongs to,
	// when it belongs to one.
	MemoryID    string
	MemoryTitle string
}

// TransactionDay groups a day's transactions under a single heading, which is
// what keeps a long list readable on a narrow screen.
type TransactionDay struct {
	Date time.Time

	// Net is the day's movement across the listed transactions, so a day can be
	// read as a whole rather than by summing the rows by eye.
	Net  float64
	Rows []TransactionRow
}

// BuildTransactionRow decorates a transaction for display.
func BuildTransactionRow(tx models.Transaction) TransactionRow {
	row := TransactionRow{
		Transaction: tx,
		Title:       tx.Merchant.Name,
		Icon:        "bi-receipt",
	}
	if row.Title == "" {
		row.Title = tx.Description
	}
	if row.Title == "" {
		row.Title = "Transaction"
	}

	// Money coming in is not filed under a category, so give it its own mark
	// rather than leaving it to fall through to the neutral default.
	if tx.Amount > 0 {
		row.Icon = "bi-arrow-down-left"
		row.Accent = "sage"
		return row
	}
	if tx.Type == "TRANSFER" {
		return row
	}
	category := models.TopBudgetCategory(tx.BudgetCategory())
	row.Accent = category.Accent
	row.Icon = category.Icon
	return row
}

// GroupTransactionsByDay folds a date-descending list of transactions into days,
// reading each date in the given location. Akahu stores dates in UTC, so a
// late evening purchase lands on tomorrow unless it is read locally first.
// The input is assumed to be sorted, which lets this stay a single pass and
// keeps the days in the same order as the transactions.
func GroupTransactionsByDay(transactions []models.Transaction, location *time.Location) []TransactionDay {
	if location == nil {
		location = time.Local
	}

	var days []TransactionDay

	for _, tx := range transactions {
		local := tx.Date.In(location)
		year, month, day := local.Date()
		date := time.Date(year, month, day, 0, 0, 0, 0, location)

		if len(days) == 0 || !days[len(days)-1].Date.Equal(date) {
			days = append(days, TransactionDay{Date: date})
		}

		current := &days[len(days)-1]
		current.Net += tx.Amount
		current.Rows = append(current.Rows, BuildTransactionRow(tx))
	}

	return days
}

func (app *Application) Transactions(c *echo.Context) error {
	ctx := c.Request().Context()

	account := c.QueryParam("account")
	search := c.QueryParam("search")

	start := time.Now()
	var transactions []models.Transaction
	var err error
	if account == "" && search == "" {
		// Default, all transactions
		transactions, err = app.store.ReadTransactions(ctx)
	} else {
		// If were here, either an account or search is specified,
		// so we need to search the transactions.
		transactions, err = app.store.SearchTransactions(ctx, search, account)
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	took := time.Since(start)

	// A date range narrows whatever the search matched. It is how a memory on
	// the timeline opens everything bought around it.
	from, to, dated := transactionDateRange(c.QueryParam("from"), c.QueryParam("to"), clock.Now().Location())
	if dated {
		transactions = slices.DeleteFunc(transactions, func(t models.Transaction) bool {
			return t.Date.Before(from) || !t.Date.Before(to)
		})
	}

	// Groups narrow to the spending filed under the categories named. It is
	// how a chart opens the transactions behind a bar, so what is left adds
	// up to the bar.
	groups := transactionGroups(c.QueryParams()["group"])
	if len(groups) > 0 {
		transactions = slices.DeleteFunc(transactions, func(t models.Transaction) bool {
			return !slices.ContainsFunc(groups, func(g TransactionGroup) bool { return g.Matches(t) })
		})
	}

	// Uncategorised spending is counted across everything recent, whatever the
	// search, so the warning about it is the same wherever the page is opened.
	since := uncategorisedSince(clock.Now())
	recent, err := app.store.ReadTransactionsByDate(ctx, since, clock.Now())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	var uncategorisedCount int
	var uncategorisedTotal float64
	for _, t := range recent {
		if isUncategorisedSpend(t, since) {
			uncategorisedCount++
			uncategorisedTotal -= t.Amount
		}
	}
	// The filter shows every uncategorised payment, however old, so all of
	// them can be found and filed; the warning above counts recent ones only.
	uncategorisedOnly := c.QueryParam("uncategorised") != ""
	if uncategorisedOnly {
		transactions = slices.DeleteFunc(transactions, func(t models.Transaction) bool {
			return !isUncategorisedSpend(t, time.Time{})
		})
	}

	// Totals cover every match, not just the page on screen, so narrowing the
	// search tells you what the whole result set is worth.
	var in, out float64
	for _, t := range transactions {
		if t.Amount > 0 {
			in += t.Amount
		} else {
			out += t.Amount
		}
	}

	slices.SortFunc(transactions, func(a, b models.Transaction) int {
		return b.Date.Compare(a.Date)
	})

	// A hand-edited or missing limit falls back to one page rather than
	// rendering the entire history.
	limit := transactionPageSize
	if raw := c.QueryParam("limit"); raw != "" {
		if parsed, convErr := strconv.Atoi(raw); convErr == nil && parsed > 0 {
			limit = parsed
		}
	}

	total := len(transactions)
	page := transactions
	if len(page) > limit {
		page = page[:limit]
	}

	accounts, err := app.store.ReadAccounts(ctx)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	held, memories, err := app.memoryIndex(ctx)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	days := GroupTransactionsByDay(page, clock.Now().Location())
	markMemories(days, held)

	// Built from every match, not the page, so the chart describes the search
	// rather than the hundred rows under it. The category split only earns its
	// place once a search has narrowed the results to something coherent.
	series := BuildTransactionSeries(transactions, transactionSeriesMonths, clock.Now())
	if search == "" && len(groups) < 2 {
		series = series.Combined()
	}

	return c.Render(http.StatusOK, "transactions", map[string]interface{}{
		"accounts":   accounts,
		"series":     series,
		"days":       days,
		"memories":   memories,
		"categories": bulkCategoryPicker(),
		"search":     search,
		"account":    account,
		"from":       c.QueryParam("from"),
		"to":         c.QueryParam("to"),
		"dated":      dated,
		"dateRange":  transactionDateLabel(c.QueryParam("from"), c.QueryParam("to"), from, to),
		"groups":     groups,
		"took":       took,
		"in":         in,
		"out":        out,
		"total":      total,
		"showing":    len(page),
		"hasMore":    total > len(page),
		"nextLimit":  limit + transactionPageSize,

		"uncategorisedCount": uncategorisedCount,
		"uncategorisedTotal": uncategorisedTotal,
		"uncategorisedOnly":  uncategorisedOnly,
		"uncategorisedWeeks": untargetedWindowWeeks,
	})
}

// uncategorisedSince is how far back the transactions page looks for
// uncategorised spending: the same 13 weeks the budget measures untargeted
// categories over, so the warning and the budget agree.
func uncategorisedSince(now time.Time) time.Time {
	return isoWeekStart(now).AddDate(0, 0, -7*untargetedWindowWeeks)
}

// isUncategorisedSpend reports whether a transaction is recent spending that
// neither Akahu, a rule nor the owner has filed: what the budget can only
// lump together under Uncategorised.
func isUncategorisedSpend(tx models.Transaction, since time.Time) bool {
	return tx.IsDebit() && tx.BudgetCategory() == models.CategoryUncategorised && !tx.Date.Before(since)
}

// TransactionGroup is a category the transactions page is narrowed to: the
// category with its subcategories, or only what is filed under the category
// itself when it is a split category's general row.
type TransactionGroup struct {
	Key   string
	Label string

	category string
	general  bool
}

// Matches reports whether a transaction is spending filed under the group,
// the same spending the budget and insights count for it, or for Income,
// money earned.
func (g TransactionGroup) Matches(tx models.Transaction) bool {
	if g.category == models.CategoryIncome {
		return tx.IsIncome()
	}
	if !tx.IsDebit() {
		return false
	}
	key := tx.BudgetCategory()
	if g.general {
		return key == g.category
	}
	return key == g.category || models.TopBudgetCategory(key).Key == g.category
}

// transactionGroups reads the groups named in a query, leaving out any that
// name no category, so a stale link still opens rather than showing nothing.
func transactionGroups(keys []string) []TransactionGroup {
	var groups []TransactionGroup
	for _, key := range keys {
		top, general := strings.CutSuffix(key, generalSuffix)
		category, ok := models.BudgetCategoryByKey(top)
		if !ok || general && category.IsSub() {
			continue
		}
		group := TransactionGroup{Key: category.Key, Label: category.Label, category: category.Key, general: general}
		if general {
			group.Key, group.Label = generalCategory(category).Key, generalCategory(category).Label
		}
		if !slices.ContainsFunc(groups, func(g TransactionGroup) bool { return g.Key == group.Key }) {
			groups = append(groups, group)
		}
	}
	return groups
}

// transactionsLink opens the spending filed under the given categories over
// a run of local days, end exclusive, as a drill-down from a chart.
func transactionsLink(groups []string, start, end time.Time) string {
	query := url.Values{
		"group": groups,
		"from":  {start.Format(time.DateOnly)},
		"to":    {end.AddDate(0, 0, -1).Format(time.DateOnly)},
	}
	return "/transactions?" + query.Encode()
}

// transactionDateRange reads an inclusive range of local dates into the times
// that bound it: midnight at the start of the first day, and midnight after
// the last. Either end may be left open. A date that does not parse is
// ignored rather than emptying the list.
func transactionDateRange(fromRaw, toRaw string, location *time.Location) (from, to time.Time, ok bool) {
	from = time.Time{}
	to = time.Date(9999, time.January, 1, 0, 0, 0, 0, location)
	if parsed, err := time.ParseInLocation(time.DateOnly, fromRaw, location); err == nil {
		from, ok = parsed, true
	}
	if parsed, err := time.ParseInLocation(time.DateOnly, toRaw, location); err == nil {
		to, ok = parsed.AddDate(0, 0, 1), true
	}
	return from, to, ok
}

// transactionDateLabel words a date range for the chip that clears it.
func transactionDateLabel(fromRaw, toRaw string, from, to time.Time) string {
	last := to.AddDate(0, 0, -1)
	_, fromErr := time.Parse(time.DateOnly, fromRaw)
	_, toErr := time.Parse(time.DateOnly, toRaw)
	switch {
	case fromErr == nil && toErr == nil:
		label := TimelineEntry{Start: from, End: last}.Span()
		if from.Year() == last.Year() {
			label += " " + last.Format("2006")
		}
		return label
	case fromErr == nil:
		return "From " + from.Format("2 Jan 2006")
	default:
		return "Until " + last.Format("2 Jan 2006")
	}
}

// transactionSeriesMonths is how much history the chart above the results
// covers. A year of months fits a phone without the labels colliding.
const transactionSeriesMonths = 12

// TransactionSeriesGroup is one coloured band of the stacked chart. Accent is a
// token name rather than a colour so the chart and the rows beneath it cannot
// drift apart.
type TransactionSeriesGroup struct {
	Label  string
	Accent string
	Data   []float64
}

// TransactionSeries is monthly spend across the whole result set, split by the
// same categories that colour the rows. It covers every match rather than the
// page on screen, so narrowing the search reshapes the chart even when the
// visible rows do not change.
type TransactionSeries struct {
	Labels  []string
	Groups  []TransactionSeriesGroup
	HasData bool

	// Split is whether the bars are broken down by category. Without a search
	// narrowing the results the breakdown is mostly the catch-all band, which
	// reads as noise, so the whole set collapses to a single total instead.
	Split bool

	// Total is the spend the chart accounts for, which is less than the result
	// set's outgoings whenever transfers were filtered out of it.
	Total float64
}

// BuildTransactionSeries totals spend by month and category for a result set.
//
// Transfers are excluded. They are money moving between the owner's own
// accounts, so counting them would swamp every real category and describe
// nothing. Income is excluded too: this answers "what did this cost", and a
// salary credit in the same bar would net away the thing being looked at. So
// are savings and loan interest, which leave without being spent.
func BuildTransactionSeries(transactions []models.Transaction, months int, now time.Time) TransactionSeries {
	if months <= 0 {
		return TransactionSeries{}
	}

	location := now.Location()
	current := periodStart(now.In(location), CadenceMonthly)

	labels := make([]string, months)
	index := make(map[time.Time]int, months)
	for i := 0; i < months; i++ {
		start := current.AddDate(0, -(months - 1 - i), 0)
		labels[i] = start.Format("Jan 06")
		index[start] = i
	}

	// Bands are keyed by category so the ordering stays the registry's, the
	// same order the budget lists them in.
	categories := models.TopBudgetCategories()
	totals := make(map[string][]float64, len(categories))
	for _, category := range categories {
		totals[category.Key] = make([]float64, months)
	}

	series := TransactionSeries{Labels: labels}

	for _, tx := range transactions {
		category, ok := spendCategory(tx)
		if !ok {
			continue
		}
		i, ok := index[periodStart(tx.Date.In(location), CadenceMonthly)]
		if !ok {
			continue
		}
		totals[category.Key][i] += -tx.Amount
		series.Total += -tx.Amount
	}

	for _, category := range categories {
		if band, ok := nonEmptyBand(totals[category.Key]); ok {
			series.Groups = append(series.Groups, TransactionSeriesGroup{
				Label:  category.Label,
				Accent: category.Accent,
				Data:   band,
			})
		}
	}

	series.HasData = len(series.Groups) > 0
	series.Split = series.HasData
	return series
}

// Combined folds every band into one total per month. It is what an unfiltered
// list wants: across all spending the category split is dominated by whatever
// carries no category, and a chart that is seven parts grey says less than a
// plain bar does.
func (s TransactionSeries) Combined() TransactionSeries {
	if !s.HasData {
		return s
	}

	totals := make([]float64, len(s.Labels))
	for _, group := range s.Groups {
		for i, value := range group.Data {
			totals[i] += value
		}
	}

	return TransactionSeries{
		Labels:  s.Labels,
		HasData: true,
		Split:   false,
		Total:   s.Total,
		Groups: []TransactionSeriesGroup{{
			Label:  "Spend",
			Accent: "slate",
			Data:   totals,
		}},
	}
}

// nonEmptyBand reports whether a band has any spend in it. A category that
// never appears in the results should not take up a legend entry.
func nonEmptyBand(data []float64) ([]float64, bool) {
	for _, value := range data {
		if value > 0 {
			return data, true
		}
	}
	return nil, false
}
