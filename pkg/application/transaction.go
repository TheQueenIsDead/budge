package application

import (
	"github.com/labstack/echo/v4"
	"net/http"
)

func (app *Application) ListTransactions(c echo.Context) error {
	transactions, err := app.store.Transactions.List()

	if err != nil {
		return c.JSON(http.StatusInternalServerError, err)
	}
	return c.Render(http.StatusOK, "transaction.list", transactions)
}
