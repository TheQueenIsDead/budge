package application

import (
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

// transactionPageSize is how many transactions a single render carries. The
// full history runs to thousands of rows, which no phone should be asked to
// lay out in one go, so the list pages instead.
const transactionPageSize = 100

// TransactionRow is a transaction prepared for display. It carries the name the
// row should be listed under and the spend group it belongs to, so a row can
// wear the same accent colour that category uses everywhere else in the app.
type TransactionRow struct {
	models.Transaction

	Title  string
	Accent string
	Icon   string
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

	// Money coming in is not classified as spend, so give it its own mark
	// rather than leaving it to fall through to the neutral default.
	if tx.Amount > 0 {
		row.Icon = "bi-arrow-down-left"
		row.Accent = "sage"
		return row
	}
	if key, ok := ClassifySpend(tx); ok {
		if group, found := SpendGroupByKey(key); found {
			row.Accent = group.Accent
			row.Icon = group.Icon
		}
	}
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

func (app *Application) Transactions(c echo.Context) error {

	account := c.QueryParam("account")
	search := c.QueryParam("search")

	start := time.Now()
	var transactions []models.Transaction
	var err error
	if account == "" && search == "" {
		// Default, all transactions
		transactions, err = app.store.ReadTransactions()
	} else {
		// If were here, either an account or search is specified,
		// so we need to search the transactions.
		transactions, err = app.store.SearchTransactions(search, account)
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	took := time.Since(start)

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

	accounts, err := app.store.ReadAccounts()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Built from every match, not the page, so the chart describes the search
	// rather than the hundred rows under it. The category split only earns its
	// place once a search has narrowed the results to something coherent.
	series := BuildTransactionSeries(transactions, transactionSeriesMonths, time.Now())
	if search == "" {
		series = series.Combined()
	}

	return c.Render(http.StatusOK, "transactions", map[string]interface{}{
		"accounts":  accounts,
		"series":    series,
		"days":      GroupTransactionsByDay(page, time.Now().Location()),
		"search":    search,
		"account":   account,
		"took":      took,
		"in":        in,
		"out":       out,
		"total":     total,
		"showing":   len(page),
		"hasMore":   total > len(page),
		"nextLimit": limit + transactionPageSize,
	})
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

// unclassifiedSpendLabel names spend that matches no tracked category.
const unclassifiedSpendLabel = "Other"

// BuildTransactionSeries totals spend by month and category for a result set.
//
// Transfers are excluded. They are money moving between the owner's own
// accounts, so counting them would swamp every real category and describe
// nothing. Income is excluded too: this answers "what did this cost", and a
// salary credit in the same bar would net away the thing being looked at.
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

	// Bands are keyed by group so the ordering stays the registry's, which is
	// the same order the pills use on the insights page.
	groups := SpendGroups()
	totals := make(map[string][]float64, len(groups)+1)
	for _, group := range groups {
		totals[group.Key] = make([]float64, months)
	}
	totals[unclassifiedSpendLabel] = make([]float64, months)

	series := TransactionSeries{Labels: labels}

	for _, tx := range transactions {
		if tx.Type == "TRANSFER" || tx.Amount >= 0 {
			continue
		}
		i, ok := index[periodStart(tx.Date.In(location), CadenceMonthly)]
		if !ok {
			continue
		}

		key := unclassifiedSpendLabel
		if matched, ok := ClassifySpend(tx); ok {
			key = matched
		}
		totals[key][i] += -tx.Amount
		series.Total += -tx.Amount
	}

	for _, group := range groups {
		if band, ok := nonEmptyBand(totals[group.Key]); ok {
			series.Groups = append(series.Groups, TransactionSeriesGroup{
				Label:  group.Label,
				Accent: group.Accent,
				Data:   band,
			})
		}
	}
	// Unclassified spend sits last, so the named categories read first.
	if band, ok := nonEmptyBand(totals[unclassifiedSpendLabel]); ok {
		series.Groups = append(series.Groups, TransactionSeriesGroup{
			Label:  unclassifiedSpendLabel,
			Accent: "muted",
			Data:   band,
		})
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
