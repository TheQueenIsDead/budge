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

	// TODO: Change this to just calculate the delta for the day for easier looping down the line
	// Bucket the transaction values into groups by day
	transactionsByDay := make(map[string][]float64)
	for _, t := range transactions {
		key := t.Date.Format(time.DateOnly)
		transactionsByDay[key] = append(transactionsByDay[key], t.Amount)
	}

	balances := make(map[string]float64)
	first, last := FindTransactionRange(transactions)
	if first.Date.IsZero() && last.Date.IsZero() {
		return c.NoContent(http.StatusInternalServerError)
	}

	// Iterate all days between the first and last transaction, creating a backwards running balance by decrementing
	// spend (or adding income) per day.
	balances = WalkAccount(account.Balance.Current, first.Date, last.Date, transactionsByDay)

	var data []float64
	var labels []string
	var background []string
	keys := slices.Collect(maps.Keys(balances))
	slices.Sort(keys)
	for _, k := range keys {
		data = append(data, balances[k])
		labels = append(labels, k)
		//if buckets[k] > 0 {
		background = append(background, "rgb(26, 188, 156)")
		//} else {
		//	background = append(background, "rgb(255, 205, 52)")
		//}
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

func WalkAccount(currentBalance float64, first, last time.Time, transactionsByDay map[string][]float64) map[string]float64 {

	balances := make(map[string]float64)

	balances[last.Format(time.DateOnly)] = currentBalance // Init the last / most recent balance to the currently know balance
	for d := last.AddDate(0, 0, -1); !d.Before(first); d = d.AddDate(0, 0, -1) {
		//for d := last; !d.Before(first); d = d.AddDate(0, 0, -1) {
		dayAfterBalance := balances[d.AddDate(0, 0, 1).Format(time.DateOnly)]
		balance := dayAfterBalance
		for _, amount := range transactionsByDay[d.Format(time.DateOnly)] {
			balance += amount * -1
		}
		balances[d.Format(time.DateOnly)] = balance
	}
	return balances
}
