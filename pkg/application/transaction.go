package application

import (
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"net/http"
	"slices"
	"time"
)

func (app *Application) ListTransactions(c echo.Context) error {

	account := c.QueryParam("account")
	var transactions []models.Transaction
	var err error
	if account == "" {
		transactions, err = app.store.ReadTransactions()
	} else {
		transactions, err = app.store.ReadTransactionsByAccount(account)
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	slices.SortFunc(transactions, func(a, b models.Transaction) int {
		return b.Date.Compare(a.Date)
	})

	accounts, err := app.store.ReadAccounts()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.Render(http.StatusOK, "transactions", map[string]interface{}{
		"accounts":     accounts,
		"transactions": transactions,
	})
}

func (app *Application) ListTransactionsByCategory(c echo.Context) error {

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
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	slices.SortFunc(transactions, func(a, b models.Transaction) int {
		return b.Date.Compare(a.Date)
	})

	/*TODO: Filter out apparent zero values / double ups.*/
	/*TODO: Give uncategorized spending a name (Link to the transactions?)*/
	/*TODO: Tidy up the accordian drop down, could do a relative date (With hover), as well as justify content in a table?
	Experiment a bit with not using a <ul> and perhaps having table rows to utilise more div space */
	type TransactionsByCategory struct {
		Id           string
		Category     string
		Total        float64
		Transactions []models.Transaction
	}
	categories := map[string]*TransactionsByCategory{}
	for _, t := range transactions {

		// Exit early if the Tx is zero value
		if t.Amount == 0 {
			continue
		}

		category := t.Category.Name
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

	return c.Render(http.StatusOK, "transactions.by_category", map[string]interface{}{
		"categories": categories,
	})
}

func FindTransactionRange(transactions []models.Transaction) (models.Transaction, models.Transaction) {

	if len(transactions) == 0 {
		return models.Transaction{}, models.Transaction{}
	}

	first := models.Transaction{Date: time.Now()}
	last := models.Transaction{Date: time.Unix(0, 0)}

	for _, t := range transactions {
		if t.Date.Before(first.Date) {
			first = t
		}
		if t.Date.After(last.Date) {
			last = t
		}
	}

	return first, last
}
