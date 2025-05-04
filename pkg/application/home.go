package application

import (
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"maps"
	"net/http"
	"slices"
	"sort"
	"time"
)

func topMerchants(transactions []models.Transaction, n int) []models.MerchantTotal {

	merchants := make(map[string]float64)
	for _, tx := range transactions {
		merchants[tx.Description] += tx.Float()
	}

	var top []models.MerchantTotal
	for merchant, total := range merchants {
		top = append(top, models.MerchantTotal{
			Merchant: merchant,
			Total:    total,
		})
	}

	sort.Slice(top, func(i, j int) bool {
		return top[i].Total > top[j].Total
	})

	if len(top) >= n {
		return top[:n]
	}
	return top[:]
}

func (app *Application) Home(c echo.Context) error {

	var accountCount, transactionCount, merchantCount int
	var err error

	accountCount, err = app.store.CountAccount()
	if err != nil {
		return err
	}
	transactionCount, err = app.store.CountTransactions()
	if err != nil {
		return err
	}
	merchantCount, err = app.store.CountMerchant()
	if err != nil {
		return err
	}

	var transactions []models.Transaction
	transactions, err = app.store.ReadTransactions()
	if err != nil {
		c.Logger().Error(err)
		return err
	}

	top := topMerchants(transactions, 5)

	return c.Render(http.StatusOK, "home", map[string]interface{}{
		"accountCount":     accountCount,
		"transactionCount": transactionCount,
		"merchantCount":    merchantCount,
		"topMerchants":     top,
	})
}

func (app *Application) Report(c echo.Context) error {

	/* Common Code */
	period := c.QueryParam("period")
	var periodDays = 7
	switch period {
	case "week":
		periodDays = 7
	case "month":
		periodDays = 30
	case "quarter":
		periodDays = 31 * 4
	}
	queryStart := time.Now().AddDate(0, 0, -1*periodDays)

	transactions, err := app.store.ReadTransactionsByDate(queryStart, time.Now())
	if err != nil {
		c.Logger().Error(err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	/*Transactions By Category*/
	slices.SortFunc(transactions, func(a, b models.Transaction) int {
		return b.Date.Compare(a.Date)
	})

	/*TODO: Tidy up the accordian drop down, could do a relative date (With hover), as well as justify content in a table?
	Experiment a bit with not using a <ul> and perhaps having table rows to utilise more div space */
	type TransactionsByCategory struct {
		Id           string
		Category     string
		Total        float64
		Transactions []models.Transaction
	}

	// Categories holds all transactions based on category
	categories := map[string]*TransactionsByCategory{}
	// Buckets sums the total spend over the reporting period
	buckets := map[string]float64{}

	for _, t := range transactions {

		// Track spend by date by summing transactions based on a daily bucket
		key := t.Date.Format(time.DateOnly)
		buckets[key] += t.Amount

		// Proceed to categorize transactions for the frontend

		// Exit early if the Tx is zero value, or not spending
		if t.Amount >= 0 || t.Type == "TRANSFER" {
			continue
		}

		category := "Uncategorised"
		if t.Category.Name != "" {
			category = t.Category.Name
		}
		if _, ok := categories[category]; ok {
			categories[category].Transactions = append(categories[category].Transactions, t)
			categories[category].Total += t.Amount
		} else {
			categories[category] = &TransactionsByCategory{
				Id:           t.Category.Id,
				Category:     category,
				Total:        t.Amount,
				Transactions: []models.Transaction{t},
			}
		}
	}

	// Build the timeseries chart data
	var data []float64
	var labels []string
	var background []string
	keys := slices.Collect(maps.Keys(buckets))
	slices.Sort(keys)
	for _, k := range keys {
		data = append(data, buckets[k])
		labels = append(labels, k)
		if buckets[k] > 0 {
			background = append(background, "rgb(26, 188, 156)")
		} else {
			background = append(background, "rgb(255, 205, 52)")
		}
	}

	return c.Render(http.StatusOK, "report", map[string]interface{}{
		"chart_data": TimeSeriesData{
			ChartId:    "timeseries_chart",
			Title:      "Spend Over Time",
			Labels:     labels,
			Data:       data,
			Border:     background,
			Background: background,
		},
		"categories": categories,
	})
}

func (app *Application) _4XX(c echo.Context) error {
	return c.Render(http.StatusOK, "4XX", nil)
}
