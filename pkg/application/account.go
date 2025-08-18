package application

import (
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"maps"
	"net/http"
	"slices"
	"time"
)

type AccountTimeseriesData struct {
	Labels []string
	Data   []float64
}

type AccountsPageProps struct {
	Account   models.Account
	GraphData AccountTimeseriesData
}

func (app *Application) Accounts(c echo.Context) error {
	accounts, _ := app.store.ReadAccounts()

	var props []AccountsPageProps
	for _, account := range accounts {
		gd, err := app.accountBalance(c, account)
		if err != nil {
			c.Logger().Error(err)
			continue
		}
		props = append(props, AccountsPageProps{
			Account:   account,
			GraphData: gd,
		})
	}
	return c.Render(http.StatusOK, "accounts", props)
}

func (app *Application) accountBalance(c echo.Context, account models.Account) (AccountTimeseriesData, error) {

	// Retrieve all transactions for an account
	transactions, err := app.store.ReadTransactionsByAccount(account.Id)
	if err != nil {
		c.Logger().Error(err)
		return AccountTimeseriesData{}, err
	}

	// Calculate the balance delta per day
	deltas := make(map[string]float64)
	for _, t := range transactions {
		deltas[t.Date.Format(time.DateOnly)] += t.Amount
	}

	// Iterate all days between the first and last transaction, creating a backwards running balance by decrementing
	// spend (or adding income) per day.
	balances := WalkAccount(account.Balance.Current, deltas)

	var data []float64
	var labels []string
	keys := slices.Collect(maps.Keys(balances))
	slices.Sort(keys)
	for _, k := range keys {
		data = append(data, balances[k])
		labels = append(labels, k)
	}

	return AccountTimeseriesData{
		Labels: labels,
		Data:   data,
	}, nil
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
