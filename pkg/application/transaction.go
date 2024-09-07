package application

import (
	"github.com/labstack/echo/v4"
	"net/http"
)

func (app *Application) ListTransactions(c echo.Context) error {
	transactions, err := app.store.Transactions.List()
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
