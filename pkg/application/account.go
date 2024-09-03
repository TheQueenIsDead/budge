package application

import (
	"github.com/labstack/echo/v4"
	"net/http"
)

func (app *Application) ListAccounts(c echo.Context) error {
	accounts, _ := app.store.Accounts.List()
	return c.Render(http.StatusOK, "account.list", accounts)
}
