package application

import (
	"fmt"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"maps"
	"net/http"
	"slices"
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

	transactions, err := app.store.ReadTransactionsByAccount(account.Id)
	if err != nil {
		c.Logger().Error(err)
		return err
	}

	slices.SortFunc(transactions, func(a, b models.Transaction) int {
		return b.Date.Compare(a.Date)
	})

	balances := make(map[string]float64)
	first, last := FindTransactionRange(transactions)
	for d := first.Date; d.After(last.Date) == false; d = d.AddDate(0, 0, 1) {
		//balances[d.Format("2006-01-02")] = balances[d.Format("2006-01-02")]
		balances[d.Format("2006-01-02")] = account.Balance.Current
	}

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
