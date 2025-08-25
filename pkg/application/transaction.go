package application

import (
	"net/http"
	"slices"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

func (app *Application) Transactions(c echo.Context) error {

	account := c.QueryParam("account")
	search := c.QueryParam("search")

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
		"account":      account,
	})
}
