package application

import (
	"github.com/labstack/echo/v4"
	"net/http"
)

func (app *Application) Settings(c echo.Context) error {

	config := app.integrations.Config()
	accounts := app.integrations.AkahuAccounts()
	return c.Render(http.StatusOK, "settings", map[string]interface{}{
		"config":   config,
		"accounts": accounts,
	})
}

func (app *Application) SyncAkahu(c echo.Context) error {
	err := app.integrations.SyncAkahu()
	if err != nil {
		return err
	}
	return nil
}
