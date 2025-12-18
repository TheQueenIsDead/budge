package application

import (
	"maps"
	"math"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

type AccountTimeseriesData struct {
	Labels []string
	Data   []float64
}

type AccountStatistics struct {
	TotalInflow    float64
	TotalOutflow   float64
	NetChange      float64
	AverageBalance float64
	HighestBalance float64
	LowestBalance  float64
}

type AccountsPageProps struct {
	Account       models.Account
	GraphData     AccountTimeseriesData
	Statistics    AccountStatistics
	IsCurrentYear bool
	PrevYear      int
	NextYear      int
	Date          time.Time
}

func (app *Application) Accounts(c echo.Context) error {
	accounts, _ := app.store.ReadAccounts()

	var props []AccountsPageProps
	for _, account := range accounts {
		props = append(props, AccountsPageProps{
			Account: account,
		})
	}
	return c.Render(http.StatusOK, "accounts", props)
}

func (app *Application) Account(c echo.Context) error {

	accountId := c.Param("id")
	account, err := app.store.GetAccount([]byte(accountId))
	if err != nil {
		c.Logger().Debug(accountId)
		c.Logger().Error(err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch accounts")
	}

	year := c.QueryParam("year")
	var viewDate time.Time
	if year != "" {
		y, err := strconv.Atoi(year)
		if err != nil {
			viewDate = time.Now()
		} else {
			viewDate = time.Date(y, time.January, 1, 0, 0, 0, 0, time.UTC)
		}
	} else {
		viewDate = time.Now()
	}

	graphData, statistics, err := app.accountBalance(c, account, viewDate)
	if err != nil {
		c.Logger().Error(err)
		// Continue, maybe graph is not essential
	}

	props := AccountsPageProps{
		Account:       account,
		GraphData:     graphData,
		Statistics:    statistics,
		Date:          viewDate,
		IsCurrentYear: viewDate.Year() == time.Now().Year(),
		PrevYear:      viewDate.AddDate(-1, 0, 0).Year(),
		NextYear:      viewDate.AddDate(1, 0, 0).Year(),
	}

	return c.Render(http.StatusOK, "account", props)
}

func (app *Application) accountBalance(c echo.Context, account models.Account, viewDate time.Time) (AccountTimeseriesData, AccountStatistics, error) {

	// Retrieve all transactions for an account
	transactions, err := app.store.ReadTransactionsByAccount(account.Id)
	if err != nil {
		c.Logger().Error(err)
		return AccountTimeseriesData{}, AccountStatistics{}, err
	}

	// Filter transactions for the selected year
	year := viewDate.Year()
	startDate := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
	endDate := startDate.AddDate(1, 0, 0)

	// Calculate the balance at the end of the selected year by rolling back future transactions
	balanceAtEndDate := account.Balance.Current
	for _, t := range transactions {
		if t.Date.After(endDate) && (t.Date.Before(account.Refreshed.Balance) || t.Date.Equal(account.Refreshed.Balance)) {
			balanceAtEndDate -= t.Amount
		}
	}

	var recentTransactions []models.Transaction
	for _, t := range transactions {
		if (t.Date.After(startDate) || t.Date.Equal(startDate)) && t.Date.Before(endDate) {
			recentTransactions = append(recentTransactions, t)
		}
	}

	// Calculate statistics on the filtered transactions
	stats := AccountStatistics{
		LowestBalance: math.MaxFloat64,
	}
	for _, t := range recentTransactions {
		if t.Amount > 0 {
			stats.TotalInflow += t.Amount
		} else {
			stats.TotalOutflow += t.Amount
		}
	}
	stats.NetChange = stats.TotalInflow + stats.TotalOutflow

	// Calculate the balance delta per month
	deltas := make(map[string]float64)
	for _, t := range recentTransactions {
		deltas[t.Date.Format("2006-01")] += t.Amount
	}

	// Iterate all months between the first and last transaction, creating a backwards running balance
	balances := WalkAccount(balanceAtEndDate, deltas)

	var data []float64
	var labels []string
	keys := slices.Collect(maps.Keys(balances))
	slices.Sort(keys)
	var balanceSum float64
	for _, k := range keys {
		balance := balances[k]
		data = append(data, balance)
		labels = append(labels, k)

		// calculate balance stats
		balanceSum += balance
		if balance > stats.HighestBalance {
			stats.HighestBalance = balance
		}
		if balance < stats.LowestBalance {
			stats.LowestBalance = balance
		}
	}
	if len(data) > 0 {
		stats.AverageBalance = balanceSum / float64(len(data))
	} else {
		stats.LowestBalance = 0 // Avoid showing MaxFloat64
	}

	graphData := AccountTimeseriesData{
		Labels: labels,
		Data:   data,
	}

	return graphData, stats, nil
}

// WalkAccount takes a balance and list of changes in balance for a series of periods and calculates the balance at the preceding periods.
// It is assumed that the delta map is keyed with a format that sorts chronologically (e.g. "2006-01"), and that the balance given is for the most
// recent period in the deltas map.
func WalkAccount(balance float64, deltas map[string]float64) map[string]float64 {

	balances := make(map[string]float64)

	// Retrieve periods to iterate and ensure that they are sorted.
	periods := slices.Collect(maps.Keys(deltas))
	slices.Sort(periods)

	// Iterate through each period from most recent into the past.
	for i := len(periods) - 1; i >= 0; i-- {
		today := periods[i]
		if i+1 < len(periods) {
			// Derive today's balance by setting it to tomorrow's balance - tomorrow's delta (Inverted)
			tomorrow := periods[i+1]
			balances[today] = balances[tomorrow] + (deltas[tomorrow] * -1)
		} else {
			// Most recent period is assumed to have the starting balance
			balances[today] = balance
		}
	}

	return balances
}
