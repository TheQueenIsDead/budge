package application

import (
	"github.com/labstack/echo/v4"
	"net/http"
)

func (app *Application) ListTransactions(c echo.Context) error {
	transactions, _ := app.store.Transactions.List()
	return c.Render(http.StatusOK, "transaction.list", transactions)
}
