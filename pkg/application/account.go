package application

import (
	"fmt"
	"github.com/labstack/echo/v4"
	"maps"
	"net/http"
	"slices"
	"time"
)

func (app *Application) ListAccounts(c echo.Context) error {
	accounts, _ := app.store.ReadAccounts()
	return c.Render(http.StatusOK, "accounts", accounts)
}

func (app *Application) AccountBalanceGraph(c echo.Context) error {

	id := c.QueryParam("id")
	if id == "" {
		return c.NoContent(http.StatusBadRequest)
	}

	account, _ := app.store.GetAccount([]byte(id))

	// Retrieve all transactions for an account
	transactions, err := app.store.ReadTransactionsByAccount(account.Id)
	if err != nil {
		c.Logger().Error(err)
		return err
	}

	// Calculate the balance delta per day
	deltas := make(map[string]float64)
	for _, t := range transactions {
		deltas[t.Date.Format(time.DateOnly)] += t.Amount
	}

	balances := make(map[string]float64)

	// Iterate all days between the first and last transaction, creating a backwards running balance by decrementing
	// spend (or adding income) per day.
	balances = WalkAccount(account.Balance.Current, deltas)

	var data []float64
	var labels []string
	var background []string
	keys := slices.Collect(maps.Keys(balances))
	slices.Sort(keys)
	for _, k := range keys {
		data = append(data, balances[k])
		labels = append(labels, k)
		background = append(background, "rgb(26, 188, 156)")
	}

	return c.Render(200, "chart.timeseries", TimeseriesData{
		ChartId:    fmt.Sprintf("account_balance_chart_%s", id),
		Title:      "Balance Over Time",
		Labels:     labels,
		Data:       data,
		Border:     background,
		Background: background,
	})
}

// WalkAccount takes a balance and list of changes in balance for a series of days and calculates the balance at the preceding days.
// It is assumed that the delta map is keyed with the time.DateOnly format, and that the balance given is for the most
// recent day in the deltas map.
func WalkAccount(balance float64, deltas map[string]float64) map[string]float64 {

	balances := make(map[string]float64)

	// Retrieve days to iterate and ensure that they are sorted.
	days := slices.Collect(maps.Keys(deltas))
	slices.Sort(days)

	// Iterate through each day from most recent into the past.
	for i := len(days) - 1; i >= 0; i-- {
		today := days[i]
		if i+1 < len(days) {
			// Derive today's balance by setting it to tomorrow's balance - tomorrow's delta (Inverted)
			tomorrow := days[i+1]
			balances[today] = balances[tomorrow] + (deltas[tomorrow] * -1)
		} else {
			// Most recent day is assumed to have the starting balance
			balances[today] = balance
		}
	}

	return balances
}
