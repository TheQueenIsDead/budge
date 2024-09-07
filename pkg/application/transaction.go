package application

import (
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"net/http"
)

func (app *Application) ListTransactions(c echo.Context) error {

	account := c.QueryParam("account")
	var transactions []models.Transaction
	var err error
	if account == "" {
		transactions, err = app.store.Transactions.List()
	} else {
		transactions, err = app.store.Transactions.ListByAccount(account)
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	accounts, err := app.store.Accounts.List()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.Render(http.StatusOK, "transactions", map[string]interface{}{
		"accounts":     accounts,
		"transactions": transactions,
	})
}
