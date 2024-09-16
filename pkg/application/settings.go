package application

import (
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"net/http"
)

func (app *Application) Settings(c echo.Context) error {

	config := app.integrations.Config()
	accounts, err := app.integrations.AkahuAccounts()
	if err != nil {
		return err
	}
	return c.Render(http.StatusOK, "settings", map[string]interface{}{
		"config":   config,
		"accounts": accounts,
	})
}

func (app *Application) SyncAkahu(c echo.Context) error {
	err := app.integrations.SyncAkahu(c)
	if err != nil {
		return err
	}
	return nil
}

func (app *Application) PutAkahuSettings(c echo.Context) error {

	appToken := c.FormValue("akahuAppToken")
	userToken := c.FormValue("akahuUserToken")

	settings := models.IntegrationAkahuSettings{
		AppToken:  appToken,
		UserToken: userToken,
	}

	if err := settings.Validate(); err != nil {
		return err
	}

	err := app.integrations.PutAkahuSettings(settings)
	if err != nil {
		return err
	}

	return nil
}
