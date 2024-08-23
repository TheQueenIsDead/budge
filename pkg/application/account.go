package application

import (
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"net/http"
)

func (app *Application) ListAccounts(c echo.Context) error {
	// TODO: Remove insert of dummy accounts
	_, err := app.store.Accounts.Put(models.Account{
		Number:       "6969",
		Transactions: nil,
		Bank:         models.Bank(models.Kiwibank),
	})
	if err != nil {
		return err
	}
	accounts, _ := app.store.Accounts.List()
	return c.Render(http.StatusOK, "account.list", accounts)
}
