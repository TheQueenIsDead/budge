package application

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/clock"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getAccount renders the account page for id, with query as its query
// string.
func getAccount(t *testing.T, app *Application, id, query string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/portfolio/accounts/"+id+query, nil)
	rec := httptest.NewRecorder()
	c := app.http.NewContext(req, rec)
	c.SetPathValues(echo.PathValues{{Name: "id", Value: id}})
	require.NoError(t, app.Account(c))
	require.Equal(t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

// TestAccountPage opens the account page for a loan and an everyday account,
// over a year and over all time.
func TestAccountPage(t *testing.T) {
	app := newTestApp(t)
	now := clock.Now()
	thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	loan := account("loan", "Home Loan", "Kiwibank", "LOAN", -100_000)
	loan.Refreshed.Balance = now
	everyday := account("everyday", "Everyday", "Kiwibank", "CHECKING", 1_500)
	everyday.Refreshed.Balance = now

	// Thirteen whole months before this one, each paying 2,000 off the loan
	// and charging 500 of interest, and nothing yet this month.
	var transactions []models.Transaction
	for i := 13; i >= 1; i-- {
		start := thisMonth.AddDate(0, -i, 0)
		transactions = append(transactions,
			models.Transaction{Id: fmt.Sprintf("repaid_%d", i), Account: "loan", Amount: 2_000, Type: "TRANSFER", Date: start},
			models.Transaction{Id: fmt.Sprintf("interest_%d", i), Account: "loan", Amount: -500, Type: "LOAN", Date: start.AddDate(0, 0, 27)},
			models.Transaction{Id: fmt.Sprintf("spent_%d", i), Account: "everyday", Amount: -40, Type: "EFTPOS", Date: start.AddDate(0, 0, 9)},
		)
	}
	require.NoError(t, app.store.SaveSynced(t.Context(), []models.Account{loan, everyday}, transactions, nil))

	t.Run("a loan's year is the year it always was", func(t *testing.T) {
		page := getAccount(t, app, "loan", "")
		assert.Contains(t, page, "End of month balance across "+now.Format("2006"))
		assert.Contains(t, page, `aria-label="Previous year"`)
		assert.Contains(t, page, `<a href="?" class="active" aria-current="page">Year</a>`)
		assert.NotContains(t, page, `id="loan-overview"`)
	})

	t.Run("a loan over all time says when it will be paid off", func(t *testing.T) {
		page := getAccount(t, app, "loan", "?range=all")
		assert.Contains(t, page, `<a href="?range=all" class="active" aria-current="page">All time</a>`)
		assert.NotContains(t, page, `aria-label="Previous year"`, "a year to step through is the year view's")
		assert.Contains(t, page, `id="loan-overview"`)

		since := thisMonth.AddDate(0, -13, 0)
		assert.Contains(t, page, "Owing at the end of each month since "+since.Format("Jan 2006"))
		// Owed as a positive amount, though the bank signs it negative.
		assert.Contains(t, page, "$100,000.00")
		// Thirteen months of 1,500 are paid off; the first month's balance
		// is already past its own.
		assert.Contains(t, page, "$18,000.00")
		assert.Contains(t, page, "$1,500.00")
		assert.Contains(t, page, "average fall over 12 months")
		// 100,000 at 1,500 a month is 67 months on.
		assert.Contains(t, page, thisMonth.AddDate(0, 67, 0).Format("Jan 2006"))
		assert.Contains(t, page, "estimate, at the recent rate")
	})

	t.Run("an everyday account has all time too, without a payoff", func(t *testing.T) {
		page := getAccount(t, app, "everyday", "?range=all")
		assert.Contains(t, page, `class="active" aria-current="page">All time</a>`)
		assert.Contains(t, page, "End of month balance since")
		assert.NotContains(t, page, `id="loan-overview"`)
	})

	t.Run("an everyday account's year is unchanged", func(t *testing.T) {
		page := getAccount(t, app, "everyday", "?year="+now.Format("2006"))
		assert.Contains(t, page, "End of month balance across "+now.Format("2006"))
		assert.NotContains(t, page, `id="loan-overview"`)
	})
}

func TestBuildAccountBalanceAllTime(t *testing.T) {
	now := time.Date(2026, time.June, 15, 9, 0, 0, 0, time.UTC)
	var acct models.Account
	acct.Id = "acct"
	acct.Balance.Current = 1_000
	acct.Refreshed.Balance = now

	at := func(year int, m time.Month, day int, amount float64) models.Transaction {
		return models.Transaction{Account: "acct", Amount: amount, Date: time.Date(year, m, day, 0, 0, 0, 0, time.UTC)}
	}

	t.Run("a point every month, quiet ones too, up to this one", func(t *testing.T) {
		graph, stats, balances := BuildAccountBalanceAllTime(acct, []models.Transaction{
			at(2026, time.June, 2, 300),
			at(2025, time.November, 20, 200),
			at(2026, time.January, 31, -100),
		}, now)

		assert.Equal(t, []string{"2025-11", "2025-12", "2026-01", "2026-02", "2026-03", "2026-04", "2026-05", "2026-06"}, graph.Labels)
		assert.Equal(t, []float64{800, 800, 700, 700, 700, 700, 700, 1_000}, graph.Data)
		require.Len(t, balances, 8)
		assert.Equal(t, MonthBalance{Month: time.Date(2025, time.November, 1, 0, 0, 0, 0, time.UTC), Balance: 800}, balances[0])
		assert.Equal(t, 1_000.0, stats.HighestBalance)
		assert.Equal(t, 700.0, stats.LowestBalance)
	})

	t.Run("nothing to chart without transactions", func(t *testing.T) {
		graph, _, balances := BuildAccountBalanceAllTime(acct, nil, now)
		assert.Empty(t, graph.Data)
		assert.Empty(t, balances)
	})
}
