package application

import (
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"net/http"
	"slices"
	"time"
)

func (app *Application) Transactions(c echo.Context) error {

	account := c.QueryParam("account")
	search := c.QueryParam("search")

	start := time.Now().AddDate(0, 0, -30)
	end := time.Now()

	var transactions []models.Transaction
	var err error
	if account != "" && search == "" {
		// All transactions for an account
		transactions, err = app.store.ReadTransactionsByAccount(account)
	} else if account == "" && search != "" {
		// Search all transactions across accounts
		transactions, err = app.store.SearchTransactions(search, "", start, end)
	} else if account != "" && search != "" {
		transactions, err = app.store.SearchTransactions(search, account, start, end)
	} else {
		// No parameters provided, load all transactions
		transactions, err = app.store.ReadTransactionsByDate(start, end)
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
		"search":       search,
	})
}
