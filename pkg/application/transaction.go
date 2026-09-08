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

	return c.Render(http.StatusOK, "transactions", map[string]interface{}{
		"accounts":  accounts,
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
